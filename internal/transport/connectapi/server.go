package connectapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/theoden9014/agentmetry/gen/agentmetry/v1"
	"github.com/theoden9014/agentmetry/gen/agentmetry/v1/agentmetryv1connect"
	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/planusage"
	"github.com/theoden9014/agentmetry/internal/query"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Clock func() time.Time

type Reader interface {
	query.DashboardReader
	query.SessionListReader
	query.SessionSummaryReader
	query.SessionActivitiesReader
	query.SessionReworkReader
	query.TraceReader
}

type LiveReader interface {
	query.ProjectionChangeReader
	query.ActivitySyncReader
}

type Server struct {
	agentmetryv1connect.UnimplementedAgentmetryQueryServiceHandler
	reader       Reader
	changes      query.ProjectionChangeReader
	activitySync query.ActivitySyncReader
	now          Clock
	subscribers  chan struct{}
}

func New(reader Reader, live LiveReader, now Clock) (string, http.Handler) {
	server := &Server{reader: reader, now: now, subscribers: make(chan struct{}, 8)}
	if live != nil {
		server.changes = live
		server.activitySync = live
	}
	return agentmetryv1connect.NewAgentmetryQueryServiceHandler(server)
}

func (server *Server) WatchProjectionChanges(ctx context.Context, request *connect.Request[v1.WatchProjectionChangesRequest], stream *connect.ServerStream[v1.WatchProjectionChangesResponse]) error {
	if server.changes == nil {
		return connect.NewError(connect.CodeUnimplemented, errors.New("projection change feed is unavailable"))
	}
	select {
	case server.subscribers <- struct{}{}:
		defer func() { <-server.subscribers }()
	default:
		return connect.NewError(connect.CodeResourceExhausted, errors.New("live subscriber limit reached"))
	}

	position, err := server.changes.CurrentProjectionPosition(ctx)
	if err != nil {
		return connect.NewError(connect.CodeInternal, err)
	}
	if token := strings.TrimSpace(request.Msg.GetAfterCursor()); token != "" {
		decoded, decodeErr := query.DecodeProjectionCursor(token)
		err = decodeErr
		if err != nil {
			return sendProjectionResync(stream, position, err)
		}
		position = decoded
	} else if err := stream.Send(&v1.WatchProjectionChangesResponse{ThroughCursor: query.EncodeProjectionCursor(position), Targets: mapProjectionTargets([]query.ChangeTarget{query.OverviewTarget(), query.AllSessionsTarget(), query.AllTracesTarget()})}); err != nil {
		return err
	}

	for {
		current, currentErr := server.changes.CurrentProjectionPosition(ctx)
		if currentErr != nil {
			return connect.NewError(connect.CodeInternal, currentErr)
		}
		if validationErr := query.ValidateProjectionPosition(current, position); validationErr != nil {
			return sendProjectionResync(stream, current, validationErr)
		}
		if current.Sequence == position.Sequence {
			if waitErr := server.changes.WaitForProjectionChange(ctx, position); waitErr != nil {
				if errors.Is(waitErr, context.Canceled) || errors.Is(waitErr, context.DeadlineExceeded) {
					return nil
				}
				return connect.NewError(connect.CodeInternal, waitErr)
			}
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return nil
			case <-timer.C:
			}
		}
		window, readErr := server.changes.ReadProjectionChanges(ctx, position, 256, 1024)
		if errors.Is(readErr, query.ErrProjectionCursorExpired) || errors.Is(readErr, query.ErrProjectionGeneration) {
			latest, latestErr := server.changes.CurrentProjectionPosition(ctx)
			if latestErr != nil {
				return connect.NewError(connect.CodeInternal, latestErr)
			}
			return sendProjectionResync(stream, latest, readErr)
		}
		if readErr != nil {
			return connect.NewError(connect.CodeInternal, readErr)
		}
		if window.Through.Sequence == position.Sequence {
			continue
		}
		if err := stream.Send(&v1.WatchProjectionChangesResponse{ThroughCursor: query.EncodeProjectionCursor(window.Through), Targets: mapProjectionTargets(window.Targets)}); err != nil {
			return err
		}
		position = window.Through
	}
}

func (server *Server) SyncSessionActivities(ctx context.Context, request *connect.Request[v1.SyncSessionActivitiesRequest]) (*connect.Response[v1.SyncSessionActivitiesResponse], error) {
	if server.activitySync == nil || server.changes == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("activity sync is unavailable"))
	}
	identity, err := query.NewConversationIdentity(request.Msg.GetSourceId(), request.Msg.GetSessionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	filter, resync, err := server.activitySyncFilter(ctx, request.Msg.GetAfterCursor(), request.Msg.GetThroughCursor(), request.Msg.GetPage())
	if err != nil {
		return nil, err
	}
	if resync != nil {
		return connect.NewResponse(sessionSyncResponse(resync)), nil
	}
	page, syncErr := server.activitySync.SyncSessionActivities(ctx, query.SessionActivitySyncFilter{ActivitySyncFilter: filter, Identity: identity})
	result, err := server.mapActivitySyncResult(ctx, page, syncErr)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(sessionSyncResponse(result)), nil
}

func (server *Server) SyncTraceActivities(ctx context.Context, request *connect.Request[v1.SyncTraceActivitiesRequest]) (*connect.Response[v1.SyncTraceActivitiesResponse], error) {
	if server.activitySync == nil || server.changes == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("activity sync is unavailable"))
	}
	traceID, err := query.ParseTraceID(strings.TrimSpace(request.Msg.GetTraceId()))
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	filter, resync, err := server.activitySyncFilter(ctx, request.Msg.GetAfterCursor(), request.Msg.GetThroughCursor(), request.Msg.GetPage())
	if err != nil {
		return nil, err
	}
	if resync != nil {
		return connect.NewResponse(traceSyncResponse(resync)), nil
	}
	page, syncErr := server.activitySync.SyncTraceActivities(ctx, query.TraceActivitySyncFilter{ActivitySyncFilter: filter, TraceID: traceID})
	result, err := server.mapActivitySyncResult(ctx, page, syncErr)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(traceSyncResponse(result)), nil
}

type activitySyncResult struct {
	mutations      []*v1.ActivityMutation
	throughCursor  string
	page           *v1.PageInfo
	resyncRequired bool
	resyncReason   string
}

func (server *Server) activitySyncFilter(ctx context.Context, afterToken, throughToken string, pageRequest *v1.PageRequest) (query.ActivitySyncFilter, *activitySyncResult, error) {
	current, err := server.changes.CurrentProjectionPosition(ctx)
	if err != nil {
		return query.ActivitySyncFilter{}, nil, connect.NewError(connect.CodeInternal, err)
	}
	after, err := query.DecodeProjectionCursor(afterToken)
	if err != nil {
		return query.ActivitySyncFilter{}, activityResync(current, err), nil
	}
	through := current
	if throughToken != "" {
		through, err = query.DecodeProjectionCursor(throughToken)
		if err != nil {
			return query.ActivitySyncFilter{}, activityResync(current, err), nil
		}
	}
	size, err := boundedPageSize(pageRequest)
	if err != nil {
		return query.ActivitySyncFilter{}, nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	offset, err := parsePageToken(pageRequest.GetPageToken())
	if err != nil {
		return query.ActivitySyncFilter{}, nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	page, err := query.NewPage(offset, size)
	if err != nil {
		return query.ActivitySyncFilter{}, nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return query.ActivitySyncFilter{After: after, Through: through, Page: page}, nil, nil
}

func (server *Server) mapActivitySyncResult(ctx context.Context, page query.ActivitySyncPage, err error) (*activitySyncResult, error) {
	if errors.Is(err, query.ErrProjectionCursorExpired) || errors.Is(err, query.ErrProjectionGeneration) || errors.Is(err, query.ErrProjectionCursorInvalid) {
		current, currentErr := server.changes.CurrentProjectionPosition(ctx)
		if currentErr != nil {
			return nil, connect.NewError(connect.CodeInternal, currentErr)
		}
		return activityResync(current, err), nil
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	mutations := make([]*v1.ActivityMutation, 0, len(page.Mutations))
	for _, mutation := range page.Mutations {
		mapped := &v1.ActivityMutation{ActivityId: mutation.ActivityID}
		if mutation.Operation == query.ActivityMutationRemove {
			mapped.Operation = v1.ActivityMutationOperation_ACTIVITY_MUTATION_OPERATION_REMOVE
		} else {
			mapped.Operation = v1.ActivityMutationOperation_ACTIVITY_MUTATION_OPERATION_UPSERT
		}
		if mutation.Activity != nil {
			mapped.Activity = mapActivities([]query.Activity{*mutation.Activity})[0]
		}
		mutations = append(mutations, mapped)
	}
	return &activitySyncResult{mutations: mutations, throughCursor: query.EncodeProjectionCursor(page.Through), page: pageInfo(page.HasMore, page.NextOffset, false, 0, page.Offset)}, nil
}

func activityResync(position query.ProjectionPosition, reason error) *activitySyncResult {
	return &activitySyncResult{throughCursor: query.EncodeProjectionCursor(position), resyncRequired: true, resyncReason: reason.Error()}
}

func sessionSyncResponse(result *activitySyncResult) *v1.SyncSessionActivitiesResponse {
	if result == nil {
		return nil
	}
	return &v1.SyncSessionActivitiesResponse{Mutations: result.mutations, ThroughCursor: result.throughCursor, Page: result.page, ResyncRequired: result.resyncRequired, ResyncReason: result.resyncReason}
}

func traceSyncResponse(result *activitySyncResult) *v1.SyncTraceActivitiesResponse {
	if result == nil {
		return nil
	}
	return &v1.SyncTraceActivitiesResponse{Mutations: result.mutations, ThroughCursor: result.throughCursor, Page: result.page, ResyncRequired: result.resyncRequired, ResyncReason: result.resyncReason}
}

func sendProjectionResync(stream *connect.ServerStream[v1.WatchProjectionChangesResponse], position query.ProjectionPosition, reason error) error {
	return stream.Send(&v1.WatchProjectionChangesResponse{ThroughCursor: query.EncodeProjectionCursor(position), ResyncRequired: true, ResyncReason: reason.Error()})
}

func mapProjectionTargets(targets []query.ChangeTarget) []*v1.ProjectionChangeTarget {
	result := make([]*v1.ProjectionChangeTarget, 0, len(targets))
	for _, target := range targets {
		result = append(result, &v1.ProjectionChangeTarget{Kind: mapProjectionTargetKind(target.Kind), SourceId: target.SourceID, SessionId: target.SessionID, TraceId: target.TraceID})
	}
	return result
}

func mapProjectionTargetKind(kind query.ChangeTargetKind) v1.ProjectionTargetKind {
	switch kind {
	case query.ChangeTargetOverview:
		return v1.ProjectionTargetKind_PROJECTION_TARGET_KIND_OVERVIEW
	case query.ChangeTargetSource:
		return v1.ProjectionTargetKind_PROJECTION_TARGET_KIND_SOURCE
	case query.ChangeTargetSession:
		return v1.ProjectionTargetKind_PROJECTION_TARGET_KIND_SESSION
	case query.ChangeTargetTrace:
		return v1.ProjectionTargetKind_PROJECTION_TARGET_KIND_TRACE
	case query.ChangeTargetPlanUsage:
		return v1.ProjectionTargetKind_PROJECTION_TARGET_KIND_PLAN_USAGE
	case query.ChangeTargetAllSources:
		return v1.ProjectionTargetKind_PROJECTION_TARGET_KIND_ALL_SOURCES
	case query.ChangeTargetAllSessions:
		return v1.ProjectionTargetKind_PROJECTION_TARGET_KIND_ALL_SESSIONS
	case query.ChangeTargetAllTraces:
		return v1.ProjectionTargetKind_PROJECTION_TARGET_KIND_ALL_TRACES
	default:
		return v1.ProjectionTargetKind_PROJECTION_TARGET_KIND_UNSPECIFIED
	}
}

func (server *Server) GetDashboard(ctx context.Context, request *connect.Request[v1.GetDashboardRequest]) (*connect.Response[v1.GetDashboardResponse], error) {
	filter, err := dashboardFilter(server.now(), request.Msg.GetFilter())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	dashboard, err := server.reader.GetDashboard(ctx, filter)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.GetDashboardResponse{Dashboard: mapDashboard(dashboard)}), nil
}

func (server *Server) ListSessions(ctx context.Context, request *connect.Request[v1.ListSessionsRequest]) (*connect.Response[v1.ListSessionsResponse], error) {
	filter, err := dashboardFilter(server.now(), request.Msg.GetFilter())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	pageSize, err := boundedPageSize(request.Msg.GetPage())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	offset, err := parsePageToken(request.Msg.GetPage().GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	queryPage, err := query.NewPage(offset, pageSize)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	page, err := server.reader.ListSessions(ctx, query.SessionListFilter{
		Since: filter.Since, SourceID: filter.SourceID, Search: filter.Search,
		Page: queryPage,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.ListSessionsResponse{
		Sessions: mapSessions(page.Sessions),
		Page:     pageInfo(page.HasMore, page.NextOffset, offset > 0, max(0, offset-pageSize), offset),
	}), nil
}

func (server *Server) GetSession(ctx context.Context, request *connect.Request[v1.GetSessionRequest]) (*connect.Response[v1.GetSessionResponse], error) {
	identity, err := query.NewConversationIdentity(request.Msg.GetSourceId(), request.Msg.GetSessionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	session, err := server.reader.GetSessionSummary(ctx, identity)
	if errors.Is(err, query.ErrConversationNotFound) || errors.Is(err, query.ErrConversationTargetNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.GetSessionResponse{
		Session:  mapSession(session),
		TraceIds: append([]string(nil), session.TraceIDs...),
	}), nil
}

func (server *Server) GetSessionRework(ctx context.Context, request *connect.Request[v1.GetSessionReworkRequest]) (*connect.Response[v1.GetSessionReworkResponse], error) {
	identity, err := query.NewConversationIdentity(request.Msg.GetSourceId(), request.Msg.GetSessionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	analysis, err := server.reader.GetSessionRework(ctx, identity)
	if errors.Is(err, query.ErrConversationNotFound) || errors.Is(err, query.ErrConversationTargetNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(mapSessionRework(analysis)), nil
}

func (server *Server) ListSessionActivities(ctx context.Context, request *connect.Request[v1.ListSessionActivitiesRequest]) (*connect.Response[v1.ListSessionActivitiesResponse], error) {
	identity, err := query.NewConversationIdentity(request.Msg.GetSourceId(), request.Msg.GetSessionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	pageSize, err := boundedPageSize(request.Msg.GetPage())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	offset, err := parsePageToken(request.Msg.GetPage().GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	queryPage, err := query.NewPage(offset, pageSize)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	direction, err := timelineDirection(request.Msg.GetDirection())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	anchor, err := query.NewActivityAnchor(request.Msg.GetAnchor().GetTraceId(), request.Msg.GetAnchor().GetSpanId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	page, err := server.reader.ListSessionActivities(ctx, query.ActivityPageFilter{
		Identity: identity, Page: queryPage,
		AgentID: strings.TrimSpace(request.Msg.GetAgentId()), Direction: direction, Anchor: anchor,
	})
	if errors.Is(err, query.ErrConversationNotFound) || errors.Is(err, query.ErrConversationTargetNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.ListSessionActivitiesResponse{
		Activities: mapActivities(page.Activities),
		Page:       pageInfo(page.HasMore, page.Offset+len(page.Activities), page.HasEarlier, max(0, page.Offset-pageSize), page.Offset),
		Total:      page.Total,
	}), nil
}

func (server *Server) GetTrace(ctx context.Context, request *connect.Request[v1.GetTraceRequest]) (*connect.Response[v1.GetTraceResponse], error) {
	traceID, err := query.ParseTraceID(strings.TrimSpace(request.Msg.GetTraceId()))
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	pageSize, err := boundedPageSize(request.Msg.GetPage())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	offset, err := parsePageToken(request.Msg.GetPage().GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	queryPage, err := query.NewPage(offset, pageSize)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	trace, err := server.reader.GetTrace(ctx, query.TraceFilter{TraceID: traceID, Page: queryPage, Tail: request.Msg.GetLiveTail() && request.Msg.GetPage().GetPageToken() == ""})
	if errors.Is(err, query.ErrTraceNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.GetTraceResponse{
		TraceId: trace.TraceID, StartedAt: timestamp(trace.StartedAt), EndedAt: timestamp(trace.EndedAt),
		Status: string(trace.Status), RootSpanCount: trace.RootSpanCount, MissingParentCount: trace.MissingParentCount,
		Conversations: mapConversationRefs(trace.Conversations), Agents: mapTraceAgents(trace.Agents), Activities: mapActivities(trace.Activities),
		Page:            pageInfo(trace.HasMore, trace.ActivityOffset+len(trace.Activities), trace.ActivityOffset > 0, max(0, trace.ActivityOffset-pageSize), trace.ActivityOffset),
		TotalActivities: trace.ActivityCount,
	}), nil
}

func dashboardFilter(now time.Time, filter *v1.TimeFilter) (query.DashboardFilter, error) {
	value := "24h"
	if filter != nil {
		switch filter.GetRange() {
		case v1.TimeRange_TIME_RANGE_UNSPECIFIED:
		case v1.TimeRange_TIME_RANGE_ONE_HOUR:
			value = "1h"
		case v1.TimeRange_TIME_RANGE_ONE_DAY:
			value = "24h"
		case v1.TimeRange_TIME_RANGE_SEVEN_DAYS:
			value = "7d"
		default:
			return query.DashboardFilter{}, errors.New("unsupported time range")
		}
	}
	base, err := query.FilterForRange(now, value)
	if err != nil {
		return query.DashboardFilter{}, err
	}
	result := query.DashboardFilter{Since: base.Since}
	if filter != nil {
		result.SourceID = strings.TrimSpace(filter.GetSourceId())
		result.Search = strings.TrimSpace(filter.GetSearch())
	}
	if len(result.SourceID) > 100 || len(result.Search) > 200 {
		return query.DashboardFilter{}, errors.New("filter value is too long")
	}
	return result, nil
}

func boundedPageSize(page *v1.PageRequest) (int, error) {
	size := 100
	if page != nil && page.GetPageSize() != 0 {
		size = int(page.GetPageSize())
	}
	if size < 1 || size > 100 {
		return 0, errors.New("page_size must be between 1 and 100")
	}
	return size, nil
}

func timelineDirection(value v1.PageDirection) (query.TimelineDirection, error) {
	switch value {
	case v1.PageDirection_PAGE_DIRECTION_UNSPECIFIED, v1.PageDirection_PAGE_DIRECTION_OLDER:
		return query.TimelineOlder, nil
	case v1.PageDirection_PAGE_DIRECTION_NEWER:
		return query.TimelineNewer, nil
	default:
		return "", fmt.Errorf("%w: %s is not supported", query.ErrInvalidTimelineDirection, value)
	}
}

func parsePageToken(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || !strings.HasPrefix(string(decoded), "agentmetry:v1:") {
		return 0, errors.New("invalid page_token")
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(string(decoded), "agentmetry:v1:"))
	if err != nil || offset < 0 {
		return 0, errors.New("invalid page_token")
	}
	return offset, nil
}

func pageInfo(hasMore bool, nextOffset int, hasPrevious bool, previousOffset, startOffset int) *v1.PageInfo {
	info := &v1.PageInfo{HasMore: hasMore, StartOffset: int64(startOffset)}
	if hasMore {
		info.NextPageToken = encodePageToken(nextOffset)
	}
	if hasPrevious {
		info.PreviousPageToken = encodePageToken(previousOffset)
	}
	return info
}

func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("agentmetry:v1:%d", offset)))
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	if value.IsZero() {
		return nil
	}
	return timestamppb.New(value)
}

func mapDashboard(value query.Overview) *v1.Dashboard {
	result := &v1.Dashboard{
		Sources: mapSources(value.Sources), SignalCounts: &v1.SignalCounts{Traces: value.SignalCounts.Traces, Logs: value.SignalCounts.Logs, Metrics: value.SignalCounts.Metrics},
		RunCount: value.RunCount, AgentCount: value.AgentCount, Tokens: mapTokens(value.Tokens), PlanUsage: mapPlanUsage(value.PlanUsage),
	}
	result.RecentActivity = mapActivities(value.RecentActivity)
	return result
}

func mapSessions(values []query.Session) []*v1.SessionSummary {
	result := make([]*v1.SessionSummary, 0, len(values))
	for _, value := range values {
		result = append(result, mapSession(value))
	}
	return result
}

func mapSession(value query.Session) *v1.SessionSummary {
	result := &v1.SessionSummary{Id: value.ID, SourceId: value.SourceID, Sources: mapSources(value.Sources), StartedAt: timestamp(value.StartedAt), EndedAt: timestamp(value.EndedAt), ActivityCount: value.ActivityCount, AgentCount: value.AgentCount, Tokens: mapTokens(value.Tokens), Agents: mapAgents(value.Agents)}
	if value.CostUSD != nil {
		result.CostUsd = value.CostUSD
	}
	return result
}

func mapSessionRework(value query.SessionRework) *v1.GetSessionReworkResponse {
	report := value.Report
	return &v1.GetSessionReworkResponse{
		SourceId: value.SourceID, SessionId: value.RunID,
		Metrics: &v1.ReworkMetrics{
			ValidationFailures: report.ValidationFailures, FailFixRetryCycles: report.FailFixRetryCycles,
			ReworkDurationMs: report.ReworkDuration.Milliseconds(), ReworkTokens: mapTokens(report.ReworkTokens),
			TotalAgentEffortMs: report.TotalAgentEffort.Milliseconds(), ReworkAgentEffortRate: report.ReworkAgentEffortRate,
			ToolAttemptsWithOutcome: report.ToolAttemptsWithOutcome, ToolFailures: report.ToolFailures, ToolFailureRate: report.ToolFailureRate,
			ApiRetryWaste:    &v1.ApiRetryWaste{Attempts: report.APIRetryWaste.Attempts, DurationMs: report.APIRetryWaste.Duration.Milliseconds(), Tokens: mapTokens(report.APIRetryWaste.Tokens)},
			RepeatedCommands: report.RepeatedCommands, ReeditedFiles: report.ReeditedFiles,
		},
		Coverage: &v1.ReworkCoverage{
			ActivityCoverage: report.Coverage.ActivityCoverage, CanonicalEvents: report.Coverage.CanonicalEvents,
			ClassifiedEvents: report.Coverage.ClassifiedEvents, KnownOutcomes: report.Coverage.KnownOutcomes,
		},
		Capabilities: &v1.ReworkCapabilities{
			ChangeRevert:      mapAnalysisCapability(report.Capabilities.ChangeRevert),
			CrossAgentOverlap: mapAnalysisCapability(report.Capabilities.CrossAgentOverlap),
		},
	}
}

func mapAnalysisCapability(value query.AnalysisCapability) *v1.AnalysisCapability {
	return &v1.AnalysisCapability{State: value.State, Reason: value.Reason}
}

func mapActivities(values []query.Activity) []*v1.Activity {
	result := make([]*v1.Activity, 0, len(values))
	for _, value := range values {
		result = append(result, &v1.Activity{
			Id: value.ID, Source: value.Source, Signal: string(value.Signal), TraceId: value.TraceID, SpanId: value.SpanID, ParentSpanId: value.ParentSpanID,
			Name: value.Name, Kind: string(value.Kind), ToolName: value.ToolName, TargetAgentId: value.TargetAgentID, TargetAgentType: value.TargetAgentType,
			Content: value.Content, AgentId: value.AgentID, AgentDefinition: value.AgentDefinition, AgentType: value.AgentType, ParentAgentId: value.ParentAgentID,
			RunId: value.RunID, Model: value.Model, StartedAt: timestamp(value.StartedAt), EndedAt: timestamp(value.EndedAt), ObservedAt: timestamp(value.ObservedAt),
			Status: value.Status, Tokens: mapTokens(value.Tokens), CostUsd: value.CostUSD, ContributesToTotal: value.ContributesToTotal,
			PromptId: value.PromptID, UsageId: value.UsageID, RelatedTraceId: value.RelatedTraceID, RelatedSpanId: value.RelatedSpanID,
		})
	}
	return result
}

func mapAgents(values []query.AgentSession) []*v1.AgentSummary {
	result := make([]*v1.AgentSummary, 0, len(values))
	for _, value := range values {
		result = append(result, &v1.AgentSummary{AgentId: value.AgentID, AgentDefinition: value.AgentDefinition, AgentType: value.AgentType, ParentAgentId: value.ParentAgentID, Model: value.Model, ActivityCount: value.ActivityCount, Tokens: mapTokens(value.Tokens)})
	}
	return result
}

func mapSources(values []query.TelemetrySource) []*v1.TelemetrySource {
	result := make([]*v1.TelemetrySource, 0, len(values))
	for _, value := range values {
		result = append(result, &v1.TelemetrySource{Id: value.ID, Label: value.Label})
	}
	return result
}

func mapTokens(value canonical.TokenUsage) *v1.TokenUsage {
	return &v1.TokenUsage{Input: reported(value.Input, value.InputReported()), Output: reported(value.Output, value.OutputReported()), CacheRead: reported(value.CacheRead, value.CacheReadReported()), CacheWrite: reported(value.CacheWrite, value.CacheWriteReported()), Reasoning: reported(value.Reasoning, value.ReasoningReported()), Total: reported(value.Total(), value.TotalReported())}
}

func reported(value int64, present bool) *int64 {
	if !present {
		return nil
	}
	return &value
}

func mapPlanUsage(values []planusage.Snapshot) []*v1.PlanUsageSnapshot {
	result := make([]*v1.PlanUsageSnapshot, 0, len(values))
	for _, value := range values {
		item := &v1.PlanUsageSnapshot{Source: value.Source, AccountId: value.AccountID, Plan: value.Plan, WindowId: value.WindowID, WindowDurationMinutes: int32(value.WindowDurationMinutes), UsedPercent: value.UsedPercent, CapturedAt: timestamp(value.CapturedAt), Authority: value.Authority}
		if value.ResetsAt != nil {
			item.ResetsAt = timestamp(*value.ResetsAt)
		}
		result = append(result, item)
	}
	return result
}

func mapConversationRefs(values []query.ConversationRef) []*v1.ConversationRef {
	result := make([]*v1.ConversationRef, 0, len(values))
	for _, value := range values {
		result = append(result, &v1.ConversationRef{SourceId: value.SourceID, Id: value.ID})
	}
	return result
}

func mapTraceAgents(values []query.TraceAgent) []*v1.TraceAgent {
	result := make([]*v1.TraceAgent, 0, len(values))
	for _, value := range values {
		result = append(result, &v1.TraceAgent{SourceId: value.SourceID, ConversationId: value.ConversationID, AgentId: value.AgentID, AgentDefinition: value.AgentDefinition, AgentType: value.AgentType, ParentAgentId: value.ParentAgentID, Model: value.Model})
	}
	return result
}
