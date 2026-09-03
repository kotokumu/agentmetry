package connectapi

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
)

func (server *Server) GetTraceOverview(ctx context.Context, request *connect.Request[v1.GetTraceOverviewRequest]) (*connect.Response[v1.GetTraceOverviewResponse], error) {
	traceID, err := query.ParseTraceID(strings.TrimSpace(request.Msg.GetTraceId()))
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	reader, ok := server.reader.(query.TraceOverviewReader)
	if !ok {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("trace overview is unavailable"))
	}
	overview, err := reader.GetTraceOverview(ctx, traceID)
	if errors.Is(err, query.ErrTraceNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	activities := make([]*v1.TraceOverviewActivity, 0, len(overview.Activities))
	for _, activity := range overview.Activities {
		activities = append(activities, &v1.TraceOverviewActivity{Id: activity.ID, Source: activity.Source, Signal: string(activity.Signal), SpanId: activity.SpanID, ParentSpanId: activity.ParentSpanID,
			Name: activity.Name, Kind: string(activity.Kind), Status: activity.Status, StartedAt: timestamp(activity.StartedAt), EndedAt: timestamp(activity.EndedAt), MissingParent: activity.MissingParent})
	}
	return connect.NewResponse(&v1.GetTraceOverviewResponse{TraceId: overview.TraceID, StartedAt: timestamp(overview.StartedAt), EndedAt: timestamp(overview.EndedAt), TotalActivities: overview.TotalActivities,
		ReturnedActivities: overview.ReturnedActivities, Coverage: overview.Coverage, Activities: activities}), nil
}

func (server *Server) GetTraceWindow(ctx context.Context, request *connect.Request[v1.GetTraceWindowRequest]) (*connect.Response[v1.GetTraceWindowResponse], error) {
	traceID, err := query.ParseTraceID(strings.TrimSpace(request.Msg.GetTraceId()))
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	window := query.TraceWindow{}
	if input := request.Msg.GetWindow(); input != nil {
		if input.StartedAt != nil {
			if err := input.StartedAt.CheckValid(); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			value := input.StartedAt.AsTime()
			window.StartedAt = &value
		}
		if input.EndedAt != nil {
			if err := input.EndedAt.CheckValid(); err != nil {
				return nil, connect.NewError(connect.CodeInvalidArgument, err)
			}
			value := input.EndedAt.AsTime()
			window.EndedAt = &value
		}
		window.Kind, window.ErrorsOnly = canonical.ActivityKind(input.GetKind()), input.GetErrorsOnly()
	}
	if err := query.ValidateTraceWindow(window); err != nil {
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
	page, err := query.NewPage(offset, pageSize)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	reader, ok := server.reader.(query.TraceWindowReader)
	if !ok {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("trace window is unavailable"))
	}
	result, err := reader.GetTraceWindow(ctx, query.TraceWindowFilter{TraceID: traceID, Window: window, Page: page})
	if errors.Is(err, query.ErrTraceNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.GetTraceWindowResponse{Trace: mapTraceResponse(result.Trace, pageSize), MatchingActivities: result.MatchingActivities}), nil
}
