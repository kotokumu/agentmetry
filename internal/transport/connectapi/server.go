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
	query.TraceReader
}

type Server struct {
	agentmetryv1connect.UnimplementedAgentmetryQueryServiceHandler
	reader Reader
	now    Clock
}

func New(reader Reader, now Clock) (string, http.Handler) {
	server := &Server{reader: reader, now: now}
	return agentmetryv1connect.NewAgentmetryQueryServiceHandler(server)
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
	page, err := server.reader.ListSessions(ctx, query.SessionListFilter{
		Since: filter.Since, SourceID: filter.SourceID, Search: filter.Search,
		PageSize: pageSize, Offset: offset,
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
	sourceID := strings.TrimSpace(request.Msg.GetSourceId())
	sessionID := strings.TrimSpace(request.Msg.GetSessionId())
	if sourceID == "" || sessionID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("source_id and session_id are required"))
	}
	session, err := server.reader.GetSessionSummary(ctx, sourceID, sessionID)
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

func (server *Server) ListSessionActivities(ctx context.Context, request *connect.Request[v1.ListSessionActivitiesRequest]) (*connect.Response[v1.ListSessionActivitiesResponse], error) {
	sourceID := strings.TrimSpace(request.Msg.GetSourceId())
	sessionID := strings.TrimSpace(request.Msg.GetSessionId())
	if sourceID == "" || sessionID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("source_id and session_id are required"))
	}
	pageSize, err := boundedPageSize(request.Msg.GetPage())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	offset, err := parsePageToken(request.Msg.GetPage().GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	page, err := server.reader.ListSessionActivities(ctx, query.ActivityPageFilter{
		SourceID: sourceID, ConversationID: sessionID, PageSize: pageSize, Offset: offset,
		AgentID:   strings.TrimSpace(request.Msg.GetAgentId()),
		Direction: strings.ToLower(request.Msg.GetDirection().String()),
		TraceID:   request.Msg.GetAnchor().GetTraceId(), SpanID: request.Msg.GetAnchor().GetSpanId(),
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
	trace, err := server.reader.GetTrace(ctx, query.TraceFilter{TraceID: traceID, Offset: offset, Limit: pageSize})
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

func mapActivities(values []query.Activity) []*v1.Activity {
	result := make([]*v1.Activity, 0, len(values))
	for _, value := range values {
		result = append(result, &v1.Activity{
			Source: value.Source, Signal: string(value.Signal), TraceId: value.TraceID, SpanId: value.SpanID, ParentSpanId: value.ParentSpanID,
			Name: value.Name, Kind: string(value.Kind), ToolName: value.ToolName, TargetAgentId: value.TargetAgentID, TargetAgentType: value.TargetAgentType,
			Content: value.Content, AgentId: value.AgentID, AgentDefinition: value.AgentDefinition, AgentType: value.AgentType, ParentAgentId: value.ParentAgentID,
			RunId: value.RunID, Model: value.Model, StartedAt: timestamp(value.StartedAt), EndedAt: timestamp(value.EndedAt), ObservedAt: timestamp(value.ObservedAt),
			Status: value.Status, Tokens: mapTokens(value.Tokens), CostUsd: value.CostUSD, ContributesToTotal: value.ContributesToTotal,
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
