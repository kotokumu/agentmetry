package connectapi

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/gen/agentmetry/v1/agentmetryv1connect"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
)

type liveReader struct {
	readerStub
	mu          sync.Mutex
	position    query.ProjectionPosition
	targets     []query.ChangeTarget
	wake        chan struct{}
	syncPage    query.ActivitySyncPage
	sessionSync query.SessionActivitySyncFilter
	traceSync   query.TraceActivitySyncFilter
	readErr     error
	syncErr     error
}

func newLiveReader() *liveReader {
	return &liveReader{position: query.ProjectionPosition{Generation: "test"}, wake: make(chan struct{})}
}
func (reader *liveReader) CurrentProjectionPosition(context.Context) (query.ProjectionPosition, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.position, nil
}
func (reader *liveReader) ReadProjectionChanges(_ context.Context, after query.ProjectionPosition, _, _ int) (query.ProjectionChangeWindow, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	if reader.readErr != nil {
		return query.ProjectionChangeWindow{}, reader.readErr
	}
	return query.ProjectionChangeWindow{From: after, Through: reader.position, Targets: append([]query.ChangeTarget(nil), reader.targets...)}, nil
}
func (reader *liveReader) WaitForProjectionChange(ctx context.Context, _ query.ProjectionPosition) error {
	reader.mu.Lock()
	wake := reader.wake
	reader.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wake:
		return nil
	}
}
func (reader *liveReader) SyncSessionActivities(_ context.Context, filter query.SessionActivitySyncFilter) (query.ActivitySyncPage, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.sessionSync = filter
	return reader.syncResult(), reader.syncErr
}
func (reader *liveReader) SyncTraceActivities(_ context.Context, filter query.TraceActivitySyncFilter) (query.ActivitySyncPage, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.traceSync = filter
	return reader.syncResult(), reader.syncErr
}
func (reader *liveReader) syncResult() query.ActivitySyncPage {
	result := reader.syncPage
	if result.Through.Generation == "" {
		result.Through = reader.position
	}
	return result
}
func (reader *liveReader) commit(target query.ChangeTarget) {
	reader.mu.Lock()
	reader.position.Sequence++
	reader.targets = []query.ChangeTarget{target}
	close(reader.wake)
	reader.wake = make(chan struct{})
	reader.mu.Unlock()
}

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
	_, handler := New(reader, nil, func() time.Time { return now })
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
	_, handler := New(reader, nil, time.Now)
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
	_, handler := New(&readerStub{}, nil, time.Now)
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
	if _, err := timelineDirection(v1.PageDirection(99)); err == nil {
		t.Fatal("timelineDirection accepted an unsupported value")
	}
	if direction, err := timelineDirection(v1.PageDirection_PAGE_DIRECTION_UNSPECIFIED); err != nil || direction != query.TimelineOlder {
		t.Fatalf("default direction = %q, %v", direction, err)
	}
	if direction, err := timelineDirection(v1.PageDirection_PAGE_DIRECTION_NEWER); err != nil || direction != query.TimelineNewer {
		t.Fatalf("newer direction = %q, %v", direction, err)
	}
}

func TestConnectServerPassesAgentActivityFilter(t *testing.T) {
	reader := &readerStub{activities: query.ActivityPage{Total: 2}}
	_, handler := New(reader, nil, time.Now)
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
	effortRate := 0.6
	reader := &readerStub{rework: query.SessionRework{SourceID: "codex", RunID: "run-1", Report: query.ReworkReport{
		ValidationFailures: 2, FailFixRetryCycles: 1, ReworkDuration: 3 * time.Second,
		TotalAgentEffort: 5 * time.Second, ReworkAgentEffortRate: &effortRate,
		ReworkTokens:            canonical.TokenUsage{Input: 10, Presence: canonical.TokenPresence{Output: true}},
		ToolAttemptsWithOutcome: 4, ToolFailures: 1, ToolFailureRate: &rate,
		APIRetryWaste:    query.APIRetryWaste{Attempts: 1, Duration: time.Second},
		RepeatedCommands: 3, ReeditedFiles: 2,
		ValidationAttemptsWithOutcome: 4, FirstPassEligibleValidations: 2, FirstPassSuccesses: 1, FirstPassSuccessRate: &effortRate,
		RecurringFailureLoops: 1, RepeatedFailureAttempts: 3, ResolvedFailureLoops: 1,
		FailureResolutionDuration: 6500 * time.Millisecond,
		FailureResolutionTokens:   canonical.TokenUsage{Input: 38, Presence: canonical.TokenPresence{Output: true}},
		FailureEpisodes:           []query.RecurringFailureEpisode{{AgentID: "agent-1", Operation: canonical.OperationTest, ValidationFingerprint: "sha256:validation", ErrorFingerprints: []string{"sha256:abc"}, FailureAttempts: 3, Resolved: true, ResolutionDuration: 6500 * time.Millisecond, TraceID: "trace-1", SpanID: "span-1"}},
		Coverage:                  query.ReworkCoverage{ActivityCoverage: query.ActivityCoveragePartial, CanonicalEvents: 8, ClassifiedEvents: 7, KnownOutcomes: 4, ValidationAttempts: 4, FingerprintedFailures: 2, IdentifiedValidationAttempts: 4, IDBackedValidationAttempts: 3, MergedValidationAttempts: 1, UncorrelatedValidationObservations: 1},
		Capabilities: query.ReworkCapabilities{
			ChangeRevert:      query.AnalysisCapability{State: query.CapabilityUnavailable, Reason: "needs diffs"},
			CrossAgentOverlap: query.AnalysisCapability{State: query.CapabilityUnavailable, Reason: "needs identities"},
		},
	}}}
	_, handler := New(reader, nil, time.Now)
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
	if metrics.GetValidationFailures() != 2 || metrics.GetFailFixRetryCycles() != 1 || metrics.GetReworkDurationMs() != 3000 || metrics.GetTotalAgentEffortMs() != 5000 || metrics.GetReworkAgentEffortRate() != effortRate || metrics.GetToolFailureRate() != rate {
		t.Fatalf("unexpected metrics: %#v", metrics)
	}
	if metrics.GetRecurringFailureLoops() != 1 || metrics.GetRepeatedFailureAttempts() != 3 || metrics.GetFailureResolutionDurationMs() != 6500 || metrics.GetFailureResolutionTokens().GetTotal() != 38 || metrics.GetFirstPassEligibleValidations() != 2 {
		t.Fatalf("unexpected recurring failure metrics: %#v", metrics)
	}
	if response.Msg.GetCoverage().GetValidationAttempts() != 4 || response.Msg.GetCoverage().GetFingerprintedFailures() != 2 || response.Msg.GetCoverage().GetIdentifiedValidationAttempts() != 4 || response.Msg.GetCoverage().GetIdBackedValidationAttempts() != 3 || response.Msg.GetCoverage().GetMergedValidationAttempts() != 1 || response.Msg.GetCoverage().GetUncorrelatedValidationObservations() != 1 {
		t.Fatalf("unexpected recurrence coverage: %#v", response.Msg.GetCoverage())
	}
	if len(response.Msg.GetFailureEpisodes()) != 1 || response.Msg.GetFailureEpisodes()[0].GetValidationFingerprint() != "sha256:validation" || response.Msg.GetFailureEpisodes()[0].GetFailureAttempts() != 3 || response.Msg.GetFailureEpisodes()[0].GetTraceId() != "trace-1" {
		t.Fatalf("unexpected failure episode details: %#v", response.Msg.GetFailureEpisodes())
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
	_, handler := New(reader, nil, time.Now)
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

func TestConnectServerStreamsCheckpointThenBoundedChangeWindow(t *testing.T) {
	reader := newLiveReader()
	_, handler := New(reader, reader, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.WatchProjectionChanges(ctx, connect.NewRequest(&v1.WatchProjectionChangesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Receive() {
		t.Fatal(stream.Err())
	}
	checkpoint := stream.Msg()
	if checkpoint.GetThroughCursor() == "" || len(checkpoint.GetTargets()) != 3 {
		t.Fatalf("bootstrap checkpoint = %#v", checkpoint)
	}
	reader.commit(query.SessionTarget("codex", "session-1"))
	if !stream.Receive() {
		t.Fatal(stream.Err())
	}
	window := stream.Msg()
	if len(window.GetTargets()) != 1 || window.GetTargets()[0].GetKind() != v1.ProjectionTargetKind_PROJECTION_TARGET_KIND_SESSION {
		t.Fatalf("window = %#v", window)
	}
}

func TestConnectServerReturnsResyncForMalformedWatchCursor(t *testing.T) {
	reader := newLiveReader()
	_, handler := New(reader, reader, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)
	stream, err := client.WatchProjectionChanges(context.Background(), connect.NewRequest(&v1.WatchProjectionChangesRequest{AfterCursor: "malformed"}))
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Receive() {
		t.Fatal(stream.Err())
	}
	if !stream.Msg().GetResyncRequired() || stream.Msg().GetThroughCursor() == "" {
		t.Fatalf("resync window = %#v", stream.Msg())
	}
	position, decodeErr := query.DecodeProjectionCursor(stream.Msg().GetThroughCursor())
	if decodeErr != nil || position.Generation != "test" {
		t.Fatalf("resync cursor = %#v, error = %v", position, decodeErr)
	}
}

func TestConnectServerReturnsLatestCheckpointWhenWatchHistoryExpired(t *testing.T) {
	reader := newLiveReader()
	reader.position.Sequence = 12
	reader.readErr = query.ErrProjectionCursorExpired
	_, handler := New(reader, reader, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)
	after := query.EncodeProjectionCursor(query.ProjectionPosition{Generation: "test", Sequence: 1})
	stream, err := client.WatchProjectionChanges(context.Background(), connect.NewRequest(&v1.WatchProjectionChangesRequest{AfterCursor: after}))
	if err != nil {
		t.Fatal(err)
	}
	if !stream.Receive() {
		t.Fatal(stream.Err())
	}
	response := stream.Msg()
	position, decodeErr := query.DecodeProjectionCursor(response.GetThroughCursor())
	if !response.GetResyncRequired() || decodeErr != nil || position != reader.position {
		t.Fatalf("expired watch response = %#v, position = %#v, decode error = %v", response, position, decodeErr)
	}
}

func TestConnectServerMapsBoundedSessionActivitySync(t *testing.T) {
	reader := newLiveReader()
	reader.position.Sequence = 3
	activity := query.Activity{ID: "activity-1", Source: "codex", RunID: "session-1"}
	reader.syncPage = query.ActivitySyncPage{
		Mutations: []query.ActivityMutation{{Operation: query.ActivityMutationUpsert, ActivityID: activity.ID, Activity: &activity}},
		Through:   reader.position, Offset: 100, NextOffset: 101, HasMore: true,
	}
	_, handler := New(reader, reader, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)
	after := query.EncodeProjectionCursor(query.ProjectionPosition{Generation: "test", Sequence: 1})
	through := query.EncodeProjectionCursor(reader.position)
	response, err := client.SyncSessionActivities(context.Background(), connect.NewRequest(&v1.SyncSessionActivitiesRequest{
		SourceId: "codex", SessionId: "session-1", AfterCursor: after, ThroughCursor: through,
		Page: &v1.PageRequest{PageSize: 1, PageToken: encodePageToken(100)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Msg.GetMutations()) != 1 || response.Msg.GetMutations()[0].GetActivityId() != "activity-1" || response.Msg.GetMutations()[0].GetActivity().GetId() != "activity-1" {
		t.Fatalf("sync response = %#v", response.Msg)
	}
	if response.Msg.GetPage().GetStartOffset() != 100 || response.Msg.GetPage().GetNextPageToken() != encodePageToken(101) {
		t.Fatalf("sync page = %#v", response.Msg.GetPage())
	}
}

func TestConnectServerReturnsLatestCheckpointWhenActivitySyncHistoryExpired(t *testing.T) {
	reader := newLiveReader()
	reader.position.Sequence = 12
	reader.syncErr = query.ErrProjectionCursorExpired
	_, handler := New(reader, reader, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)
	after := query.EncodeProjectionCursor(query.ProjectionPosition{Generation: "test", Sequence: 1})
	through := query.EncodeProjectionCursor(reader.position)

	sessionResponse, err := client.SyncSessionActivities(context.Background(), connect.NewRequest(&v1.SyncSessionActivitiesRequest{
		SourceId: "codex", SessionId: "session-1", AfterCursor: after, ThroughCursor: through,
		Page: &v1.PageRequest{PageSize: 100},
	}))
	if err != nil {
		t.Fatal(err)
	}
	traceResponse, err := client.SyncTraceActivities(context.Background(), connect.NewRequest(&v1.SyncTraceActivitiesRequest{
		TraceId: "0123456789abcdef0123456789abcdef", AfterCursor: after, ThroughCursor: through, Page: &v1.PageRequest{PageSize: 100},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for name, response := range map[string]struct {
		resync bool
		cursor string
	}{
		"session": {sessionResponse.Msg.GetResyncRequired(), sessionResponse.Msg.GetThroughCursor()},
		"trace":   {traceResponse.Msg.GetResyncRequired(), traceResponse.Msg.GetThroughCursor()},
	} {
		position, decodeErr := query.DecodeProjectionCursor(response.cursor)
		if !response.resync || decodeErr != nil || position != reader.position {
			t.Fatalf("%s expired sync response = %#v, position = %#v, decode error = %v", name, response, position, decodeErr)
		}
	}
}

func TestConnectServerAcceptsNewerSessionActivityPageDirection(t *testing.T) {
	reader := &readerStub{}
	_, handler := New(reader, nil, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)
	_, err := client.ListSessionActivities(context.Background(), connect.NewRequest(&v1.ListSessionActivitiesRequest{
		SourceId: "codex", SessionId: "session-1", Page: &v1.PageRequest{PageSize: 50, PageToken: encodePageToken(50)},
		Direction: v1.PageDirection_PAGE_DIRECTION_NEWER,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if reader.lastActivities.Direction != query.TimelineNewer || reader.lastActivities.Page.Offset() != 50 {
		t.Fatalf("newer session page = %#v", reader.lastActivities)
	}
}

func TestConnectServerBoundsSubscribersAndReleasesCanceledSlot(t *testing.T) {
	reader := newLiveReader()
	_, handler := New(reader, reader, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)
	cancels := make([]context.CancelFunc, 0, 8)
	for index := 0; index < 8; index++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		stream, err := client.WatchProjectionChanges(ctx, connect.NewRequest(&v1.WatchProjectionChangesRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		if !stream.Receive() {
			t.Fatal(stream.Err())
		}
	}
	t.Cleanup(func() {
		for _, cancel := range cancels {
			cancel()
		}
	})

	overflow, err := client.WatchProjectionChanges(context.Background(), connect.NewRequest(&v1.WatchProjectionChangesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if overflow.Receive() || connect.CodeOf(overflow.Err()) != connect.CodeResourceExhausted {
		t.Fatalf("ninth subscriber error = %v", overflow.Err())
	}

	cancels[0]()
	deadline := time.Now().Add(time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		stream, streamErr := client.WatchProjectionChanges(ctx, connect.NewRequest(&v1.WatchProjectionChangesRequest{}))
		if streamErr == nil && stream.Receive() {
			cancel()
			break
		}
		cancel()
		if time.Now().After(deadline) {
			t.Fatal("canceled subscriber slot was not released")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
