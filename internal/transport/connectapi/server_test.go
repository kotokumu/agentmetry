package connectapi

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/theoden9014/agentmetry/gen/agentmetry/v1"
	"github.com/theoden9014/agentmetry/gen/agentmetry/v1/agentmetryv1connect"
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
}

func (reader *readerStub) GetDashboard(_ context.Context, filter query.DashboardFilter) (query.Overview, error) {
	reader.lastDashboard = filter
	return reader.dashboard, nil
}

func (reader *readerStub) ListSessions(_ context.Context, filter query.SessionListFilter) (query.SessionPage, error) {
	reader.lastSessions = filter
	return reader.sessions, nil
}

func (reader *readerStub) GetSessionSummary(_ context.Context, _, _ string) (query.Session, error) {
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
	if reader.lastSessions.PageSize != 50 || reader.lastSessions.Offset != 100 {
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
	if reader.lastTrace.Offset != 50 || reader.lastTrace.Limit != 50 {
		t.Fatalf("unexpected trace filter: %#v", reader.lastTrace)
	}
	if response.Msg.GetTotalActivities() != 101 || !response.Msg.GetPage().GetHasMore() || response.Msg.GetPage().GetPreviousPageToken() != encodePageToken(0) {
		t.Fatalf("unexpected trace page: %#v", response.Msg.GetPage())
	}
}
