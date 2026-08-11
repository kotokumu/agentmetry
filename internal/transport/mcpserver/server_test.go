package mcpserver

import (
	"context"
	"testing"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
)

type readerStub struct {
	dashboardFilter query.DashboardFilter
	sessionFilter   query.SessionListFilter
	activityFilter  query.ActivityPageFilter
	activityPage    query.ActivityPage
	traceFilter     query.TraceFilter
	trace           query.Trace
}

func (reader *readerStub) GetDashboard(_ context.Context, filter query.DashboardFilter) (query.Overview, error) {
	reader.dashboardFilter = filter
	return query.Overview{SignalCounts: query.SignalCounts{Traces: 4}}, nil
}

func (reader *readerStub) ListSessions(_ context.Context, filter query.SessionListFilter) (query.SessionPage, error) {
	reader.sessionFilter = filter
	return query.SessionPage{}, nil
}

func (reader *readerStub) ListSessionActivities(_ context.Context, filter query.ActivityPageFilter) (query.ActivityPage, error) {
	reader.activityFilter = filter
	return reader.activityPage, nil
}

func (reader *readerStub) GetTrace(_ context.Context, filter query.TraceFilter) (query.Trace, error) {
	reader.traceFilter = filter
	return reader.trace, nil
}

func TestGetOverviewUsesSharedRangeAndQueryContract(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	reader := &readerStub{}
	service := &Service{reader: reader, now: func() time.Time { return now }}

	_, output, err := service.getOverview(context.Background(), nil, OverviewInput{Range: "7d", Source: "claude", Search: "review"})

	if err != nil {
		t.Fatal(err)
	}
	if !reader.dashboardFilter.Since.Equal(now.Add(-7 * 24 * time.Hour)) {
		t.Fatalf("unexpected filter: %#v", reader.dashboardFilter)
	}
	if reader.dashboardFilter.SourceID != "claude" || reader.dashboardFilter.Search != "review" {
		t.Fatalf("dashboard filters were not forwarded: %#v", reader.dashboardFilter)
	}
	if reader.sessionFilter.SourceID != "claude" || reader.sessionFilter.Search != "review" || reader.sessionFilter.PageSize != 100 {
		t.Fatalf("session filters were not forwarded: %#v", reader.sessionFilter)
	}
	if output.Overview.SignalCounts.Traces != 4 {
		t.Fatalf("unexpected output: %#v", output)
	}
}

func TestGetTraceReturnsCompleteTraceAndPreservesTypedUsage(t *testing.T) {
	const traceID = "11111111111111111111111111111111"
	reader := &readerStub{trace: query.Trace{
		TraceID: traceID,
		Activities: []query.Activity{{
			TraceID: traceID,
			Tokens:  canonical.TokenUsage{Input: 12, Presence: canonical.TokenPresence{Output: true}},
		}},
	}}
	service := &Service{reader: reader, now: time.Now}

	_, output, err := service.getTrace(context.Background(), nil, TraceInput{TraceID: traceID})

	if err != nil {
		t.Fatal(err)
	}
	if reader.traceFilter.TraceID != traceID || reader.traceFilter.Limit != 100 {
		t.Fatalf("unexpected trace filter: %#v", reader.traceFilter)
	}
	if output.Trace.TraceID != traceID || output.Trace.Activities[0].Tokens.Input == nil || *output.Trace.Activities[0].Tokens.Input != 12 {
		t.Fatalf("unexpected output: %#v", output)
	}
	if output.Trace.Activities[0].Tokens.Output == nil || *output.Trace.Activities[0].Tokens.Output != 0 {
		t.Fatalf("reported zero output was lost: %#v", output.Trace.Activities[0].Tokens)
	}
	if output.Trace.Conversations == nil || output.Trace.Agents == nil {
		t.Fatalf("trace collections must be non-nil arrays: %#v", output.Trace)
	}
}

func TestGetSessionActivitiesIsBoundedAndUsesOpaqueContinuation(t *testing.T) {
	reader := &readerStub{activityPage: query.ActivityPage{
		Offset: 5, Total: 9, HasEarlier: true, HasMore: true,
		Activities: []query.Activity{{Name: "tool call"}},
	}}
	service := &Service{reader: reader, now: time.Now}

	_, output, err := service.getSessionActivities(context.Background(), nil, SessionActivitiesInput{
		Source: "codex", SessionID: "run-1", PageSize: 4, PageToken: encodePageToken(5), Direction: "older",
	})

	if err != nil {
		t.Fatal(err)
	}
	if reader.activityFilter.SourceID != "codex" || reader.activityFilter.ConversationID != "run-1" || reader.activityFilter.PageSize != 4 || reader.activityFilter.Offset != 5 {
		t.Fatalf("unexpected activity filter: %#v", reader.activityFilter)
	}
	if output.Total != 9 || len(output.Activities) != 1 || output.NextPageToken != encodePageToken(6) || !output.HasEarlier || !output.HasMore {
		t.Fatalf("unexpected activity output: %#v", output)
	}
}

func TestGetTraceRejectsInvalidOTLPIdentity(t *testing.T) {
	service := &Service{reader: &readerStub{}, now: time.Now}

	_, _, err := service.getTrace(context.Background(), nil, TraceInput{TraceID: "not-a-trace-id"})

	if err == nil {
		t.Fatal("expected invalid trace ID error")
	}
}
