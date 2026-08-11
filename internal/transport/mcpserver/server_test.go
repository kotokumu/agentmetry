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
	summary         query.Session
	summaryErr      error
}

func testService(reader *readerStub, now Clock) *Service {
	return &Service{dashboardReader: reader, sessionReader: reader, summaryReader: reader, activityReader: reader, traceReader: reader, now: now}
}

func (reader *readerStub) GetDashboard(_ context.Context, filter query.DashboardFilter) (query.Overview, error) {
	reader.dashboardFilter = filter
	return query.Overview{SignalCounts: query.SignalCounts{Traces: 4}}, nil
}

func (reader *readerStub) ListSessions(_ context.Context, filter query.SessionListFilter) (query.SessionPage, error) {
	reader.sessionFilter = filter
	return query.SessionPage{}, nil
}

func (reader *readerStub) GetSessionSummary(_ context.Context, _, _ string) (query.Session, error) {
	return reader.summary, reader.summaryErr
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
	service := testService(reader, func() time.Time { return now })

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
	service := testService(reader, time.Now)

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
	service := testService(reader, time.Now)

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
	service := testService(&readerStub{}, time.Now)

	_, _, err := service.getTrace(context.Background(), nil, TraceInput{TraceID: "not-a-trace-id"})

	if err == nil {
		t.Fatal("expected invalid trace ID error")
	}
}

func TestGetAgentContextDoesNotClaimCallerIdentity(t *testing.T) {
	service := testService(&readerStub{}, time.Now)

	_, output, err := service.getAgentContext(context.Background(), nil, AgentContextInput{})

	if err != nil {
		t.Fatal(err)
	}
	if output.CallerIdentity.Available || output.RunIdentity.LatestIsImplicit {
		t.Fatalf("caller identity must be explicit: %#v", output)
	}
	if len(output.Workflow) == 0 || len(output.Tools) == 0 {
		t.Fatalf("context contract is not discoverable: %#v", output)
	}
}

func TestGetRunSummaryUsesExplicitIdentityAndReportsDerivedCompleteness(t *testing.T) {
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	reader := &readerStub{
		summary:      query.Session{ID: "run-1", SourceID: "codex", StartedAt: start, EndedAt: start.Add(10 * time.Second), ActivityCount: 1, Tokens: canonical.TokenUsage{Input: 10, Output: 5, Presence: canonical.TokenPresence{Input: true, Output: true}}},
		activityPage: query.ActivityPage{Activities: []query.Activity{{Source: "codex", RunID: "run-1", Name: "tool", StartedAt: start, EndedAt: start.Add(2 * time.Second)}}},
	}
	service := testService(reader, time.Now)

	_, output, err := service.getRunSummary(context.Background(), nil, RunContextInput{Source: "codex", RunID: "run-1"})

	if err != nil {
		t.Fatal(err)
	}
	if output.Run.SourceID != "codex" || output.Run.ID != "run-1" || output.Efficiency.WallDurationMs != 10000 || output.Efficiency.ActiveDurationMs != 2000 {
		t.Fatalf("unexpected run summary: %#v", output)
	}
	if output.Metadata.SourceCompleteness != "observed_projection_complete" || output.Metadata.SourceCoverage != "unknown" || output.Metadata.Confidence != "heuristic" {
		t.Fatalf("unexpected analysis metadata: %#v", output.Metadata)
	}
}

func TestGetRunTimelineHidesContentByDefault(t *testing.T) {
	reader := &readerStub{activityPage: query.ActivityPage{Activities: []query.Activity{{Name: "prompt", Content: "secret"}}}}
	service := testService(reader, time.Now)

	_, hidden, err := service.getRunTimeline(context.Background(), nil, RunTimelineInput{Source: "claude", RunID: "run-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, shown, err := service.getRunTimeline(context.Background(), nil, RunTimelineInput{Source: "claude", RunID: "run-1", IncludeContent: true})
	if err != nil {
		t.Fatal(err)
	}
	if hidden.Activities[0].Content != "" || shown.Activities[0].Content != "secret" {
		t.Fatalf("content opt-in contract failed: hidden=%#v shown=%#v", hidden.Activities[0], shown.Activities[0])
	}
}

func TestGetRunTimelineBindsContinuationToRunIdentity(t *testing.T) {
	service := testService(&readerStub{activityPage: query.ActivityPage{}}, time.Now)

	_, _, err := service.getRunTimeline(context.Background(), nil, RunTimelineInput{
		Source: "codex", RunID: "run-2", PageToken: encodeScopedPageToken("codex", "run-1", "older", 100),
	})

	if err == nil {
		t.Fatal("expected scoped cursor mismatch")
	}
}

func TestGetRunTimelineRejectsUnsupportedDirection(t *testing.T) {
	service := testService(&readerStub{}, time.Now)

	_, _, err := service.getRunTimeline(context.Background(), nil, RunTimelineInput{Source: "codex", RunID: "run-1", Direction: "newer"})

	if err == nil {
		t.Fatal("expected unsupported direction error")
	}
}
