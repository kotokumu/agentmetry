package connectapi

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/theoden9014/agentmetry/gen/agentmetry/v1"
	"github.com/theoden9014/agentmetry/gen/agentmetry/v1/agentmetryv1connect"
	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
)

type readerStub struct {
	dashboard      query.Overview
	sessions       query.SessionPage
	activities     query.ActivityPage
	conversation   query.Session
	trace          query.Trace
	lastDashboard  query.DashboardFilter
	lastSessions   query.SessionListFilter
	lastActivities query.ActivityPageFilter
	lastTrace      query.TraceFilter
	rework         query.SessionRework
	reworkIdentity query.ConversationIdentity
}

func (reader *readerStub) GetSessionRework(_ context.Context, identity query.ConversationIdentity) (query.SessionRework, error) {
	reader.reworkIdentity = identity
	return reader.rework, nil
}

func (reader *readerStub) GetDashboard(_ context.Context, filter query.DashboardFilter) (query.Overview, error) {
	reader.lastDashboard = filter
	return reader.dashboard, nil
}

func (reader *readerStub) ListSessions(_ context.Context, filter query.SessionListFilter) (query.SessionPage, error) {
	reader.lastSessions = filter
	return reader.sessions, nil
}

func (reader *readerStub) GetSessionSummary(_ context.Context, _ query.ConversationIdentity) (query.Session, error) {
	return reader.conversation, nil
}

func (reader *readerStub) ListSessionActivities(_ context.Context, filter query.ActivityPageFilter) (query.ActivityPage, error) {
	reader.lastActivities = filter
	return reader.activities, nil
}

func (reader *readerStub) GetTrace(_ context.Context, filter query.TraceFilter) (query.Trace, error) {
	reader.lastTrace = filter
	return reader.trace, nil
}

func TestConnectServerMapsDashboardAndFilters(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	reader := &readerStub{dashboard: query.Overview{SignalCounts: query.SignalCounts{Traces: 3}}}
	_, handler := New(reader, func() time.Time { return now })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)

	response, err := client.GetDashboard(context.Background(), connect.NewRequest(&v1.GetDashboardRequest{
		Filter: &v1.TimeFilter{Range: v1.TimeRange_TIME_RANGE_ONE_HOUR, SourceId: "claude", Search: "review"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.GetDashboard().GetSignalCounts().GetTraces() != 3 {
		t.Fatalf("unexpected dashboard: %#v", response.Msg.GetDashboard())
	}
	if !reader.lastDashboard.Since.Equal(now.Add(-time.Hour)) || reader.lastDashboard.SourceID != "claude" || reader.lastDashboard.Search != "review" {
		t.Fatalf("unexpected dashboard filter: %#v", reader.lastDashboard)
	}
}

func TestConnectServerUsesOpaqueSessionPageToken(t *testing.T) {
	reader := &readerStub{sessions: query.SessionPage{
		Sessions:   []query.Session{{ID: "session-1", SourceID: "claude", ActivityCount: 101}},
		NextOffset: 200, HasMore: true,
	}}
	_, handler := New(reader, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)

	response, err := client.ListSessions(context.Background(), connect.NewRequest(&v1.ListSessionsRequest{
		Page: &v1.PageRequest{PageSize: 50, PageToken: encodePageToken(100)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if reader.lastSessions.Page.Size() != 50 || reader.lastSessions.Page.Offset() != 100 {
		t.Fatalf("unexpected page filter: %#v", reader.lastSessions)
	}
	if response.Msg.GetPage().GetNextPageToken() != encodePageToken(200) || response.Msg.GetPage().GetPreviousPageToken() != encodePageToken(50) || !response.Msg.GetPage().GetHasMore() {
		t.Fatalf("unexpected page info: %#v", response.Msg.GetPage())
	}
}

func TestConnectServerRejectsUnboundedActivityPages(t *testing.T) {
	_, handler := New(&readerStub{}, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)

	_, err := client.ListSessionActivities(context.Background(), connect.NewRequest(&v1.ListSessionActivitiesRequest{
		SourceId: "claude", SessionId: "session-1", Page: &v1.PageRequest{PageSize: 1000},
	}))
	if err == nil || connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("error = %v, want invalid argument", err)
	}
}

func TestTimelineDirectionRejectsUnsupportedProtoValues(t *testing.T) {
	for _, value := range []v1.PageDirection{v1.PageDirection_PAGE_DIRECTION_NEWER, v1.PageDirection(99)} {
		if _, err := timelineDirection(value); err == nil {
			t.Fatalf("timelineDirection(%v) accepted an unsupported value", value)
		}
	}
	if direction, err := timelineDirection(v1.PageDirection_PAGE_DIRECTION_UNSPECIFIED); err != nil || direction != query.TimelineOlder {
		t.Fatalf("default direction = %q, %v", direction, err)
	}
}

func TestConnectServerPassesAgentActivityFilter(t *testing.T) {
	reader := &readerStub{activities: query.ActivityPage{Total: 2}}
	_, handler := New(reader, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)

	_, err := client.ListSessionActivities(context.Background(), connect.NewRequest(&v1.ListSessionActivitiesRequest{
		SourceId: "claude", SessionId: "session-1", AgentId: "reviewer", Page: &v1.PageRequest{PageSize: 25},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if reader.lastActivities.AgentID != "reviewer" {
		t.Fatalf("agent filter = %q, want reviewer", reader.lastActivities.AgentID)
	}
}

func TestConnectServerMapsSessionReworkWithoutInventingOptionalValues(t *testing.T) {
	rate := 0.25
	reader := &readerStub{rework: query.SessionRework{SourceID: "codex", RunID: "run-1", Report: query.ReworkReport{
		ValidationFailures: 2, FailFixRetryCycles: 1, ReworkDuration: 3 * time.Second,
		ReworkTokens:            canonical.TokenUsage{Input: 10, Presence: canonical.TokenPresence{Output: true}},
		ToolAttemptsWithOutcome: 4, ToolFailures: 1, ToolFailureRate: &rate,
		APIRetryWaste:    query.APIRetryWaste{Attempts: 1, Duration: time.Second},
		RepeatedCommands: 3, ReeditedFiles: 2,
		Coverage: query.ReworkCoverage{ActivityCoverage: query.ActivityCoveragePartial, CanonicalEvents: 8, ClassifiedEvents: 7, KnownOutcomes: 4},
		Capabilities: query.ReworkCapabilities{
			ChangeRevert:      query.AnalysisCapability{State: query.CapabilityUnavailable, Reason: "needs diffs"},
			CrossAgentOverlap: query.AnalysisCapability{State: query.CapabilityUnavailable, Reason: "needs identities"},
		},
	}}}
	_, handler := New(reader, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)

	response, err := client.GetSessionRework(context.Background(), connect.NewRequest(&v1.GetSessionReworkRequest{SourceId: "codex", SessionId: "run-1"}))

	if err != nil {
		t.Fatal(err)
	}
	if reader.reworkIdentity.SourceID() != "codex" || reader.reworkIdentity.ConversationID() != "run-1" {
		t.Fatalf("unexpected identity: %#v", reader.reworkIdentity)
	}
	metrics := response.Msg.GetMetrics()
	if metrics.GetValidationFailures() != 2 || metrics.GetFailFixRetryCycles() != 1 || metrics.GetReworkDurationMs() != 3000 || metrics.GetToolFailureRate() != rate {
		t.Fatalf("unexpected metrics: %#v", metrics)
	}
	if metrics.GetReworkTokens().GetInput() != 10 || metrics.GetReworkTokens().Output == nil || metrics.GetApiRetryWaste().GetDurationMs() != 1000 {
		t.Fatalf("optional usage/waste mapping failed: %#v", metrics)
	}
	if response.Msg.GetCoverage().GetActivityCoverage() != query.ActivityCoveragePartial || response.Msg.GetCapabilities().GetChangeRevert().GetState() != query.CapabilityUnavailable {
		t.Fatalf("coverage/capabilities missing: %#v", response.Msg)
	}
}

func TestConnectServerBoundsTracePages(t *testing.T) {
	const traceID = "11111111111111111111111111111111"
	reader := &readerStub{trace: query.Trace{TraceID: traceID, ActivityOffset: 50, ActivityCount: 101, HasMore: true}}
	_, handler := New(reader, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)

	response, err := client.GetTrace(context.Background(), connect.NewRequest(&v1.GetTraceRequest{
		TraceId: traceID, Page: &v1.PageRequest{PageSize: 50, PageToken: encodePageToken(50)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if reader.lastTrace.Page.Offset() != 50 || reader.lastTrace.Page.Size() != 50 {
		t.Fatalf("unexpected trace filter: %#v", reader.lastTrace)
	}
	if response.Msg.GetTotalActivities() != 101 || !response.Msg.GetPage().GetHasMore() || response.Msg.GetPage().GetPreviousPageToken() != encodePageToken(0) {
		t.Fatalf("unexpected trace page: %#v", response.Msg.GetPage())
	}
}
