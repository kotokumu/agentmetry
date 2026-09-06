package connectapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/gen/agentmetry/v1/agentmetryv1connect"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/harness"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/observation"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/storage/sqlite"
	"github.com/kotokumu/agentmetry/internal/transport/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
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
	traceErr       error
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
	return reader.trace, reader.traceErr
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
		Sessions:   []query.SessionListEntry{{Session: query.Session{ID: "session-1", SourceID: "claude", ActivityCount: 101}}},
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
	conditions := query.SessionConditions{ObservedFailure: true, MinDurationMS: new(10.25), MaxDurationMS: new(200.5), Model: "fixture-model", Tool: "exec_command"}
	wire := &v1.SessionConditions{ObservedFailure: true, MinDurationMs: new(10.25), MaxDurationMs: new(200.5), Model: "fixture-model", Tool: "exec_command"}
	reader.sessions.AppliedConditions = &conditions
	filtered, err := client.ListSessions(context.Background(), connect.NewRequest(&v1.ListSessionsRequest{Filter: &v1.TimeFilter{SourceId: "codex", Search: "needle"}, Conditions: wire, Page: &v1.PageRequest{PageSize: 1}}))
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(conditions, reader.lastSessions.Conditions); diff != "" {
		t.Errorf("conditions not forwarded: %s", diff)
	}
	if reader.lastSessions.SourceID != "codex" || reader.lastSessions.Search != "needle" {
		t.Errorf("existing predicates lost: %#v", reader.lastSessions)
	}
	if diff := cmp.Diff(wire, filtered.Msg.GetAppliedConditions(), protocmp.Transform()); diff != "" {
		t.Errorf("applied conditions not mapped: %s", diff)
	}
	reader.sessions.AppliedConditions = nil
	unacknowledged, err := client.ListSessions(context.Background(), connect.NewRequest(&v1.ListSessionsRequest{Conditions: wire}))
	if err != nil {
		t.Fatal(err)
	}
	if unacknowledged.Msg.GetAppliedConditions() != nil {
		t.Error("adapter invented condition acknowledgement")
	}
	for _, invalid := range []*v1.SessionConditions{
		{MinDurationMs: new(-1.0)}, {MinDurationMs: new(2.0), MaxDurationMs: new(1.0)},
		{MinDurationMs: new(math.NaN())}, {MaxDurationMs: new(math.Inf(1))}, {Model: strings.Repeat("m", 201)},
	} {
		_, err := client.ListSessions(context.Background(), connect.NewRequest(&v1.ListSessionsRequest{Conditions: invalid}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("invalid conditions accepted: %v", err)
		}
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
	harnessContext, err := query.ClassifyHarnessEvidence(query.HarnessEvidenceFacts{
		Counts: query.HarnessEvidenceCounts{EligibleRecords: 8, ReportedRecords: 8, DistinctIdentities: 1},
		Identities: []query.ReportedIdentityEvidence{{
			Identity: query.HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"},
			Records:  8, Labels: []string{"AGENTS v2"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reader := &readerStub{rework: query.SessionRework{
		SourceID: "codex", RunID: "run-1", SessionTokens: canonical.TokenUsage{Input: 100, Output: 20},
		Harness: harnessContext,
		Report: query.ReworkReport{
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
	if response.Msg.GetSessionTokens().GetTotal() != 120 || response.Msg.GetHarnessContext().GetUniform().GetIdentity().GetLabel() != "AGENTS v2" {
		t.Fatalf("snapshot context missing: %#v", response.Msg)
	}
	t.Run("stored comparison is identical through Connect and MCP without bodies", func(t *testing.T) {
		database, err := sqlite.Open(filepath.Join(t.TempDir(), "comparison.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = database.Close() })
		start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
		for _, fixture := range []struct {
			run   string
			day   int
			steps []struct {
				tool, command    string
				duration, tokens int
				success          bool
			}
		}{
			{"baseline", 0, []struct {
				tool, command    string
				duration, tokens int
				success          bool
			}{
				{"", "", 1, 100, true}, {"exec_command", "go test ./...", 1, 100, false}, {"apply_patch", "", 1, 100, true},
				{"exec_command", "go test ./...", 1, 100, false}, {"apply_patch", "", 1, 100, true}, {"exec_command", "go test ./...", 1, 100, true},
			}},
			{"current", 1, []struct {
				tool, command    string
				duration, tokens int
				success          bool
			}{
				{"", "", 2, 200, true}, {"exec_command", "go vet ./...", 1, 100, true}, {"exec_command", "go test ./...", 1, 100, false},
				{"apply_patch", "", 1, 100, true}, {"exec_command", "go test ./...", 1, 100, true},
			}},
		} {
			at := start.Add(time.Duration(fixture.day) * 24 * time.Hour)
			accepted := ingest.AcceptedExport{
				Envelope:   ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, at, []byte{0x0a, 0x00}),
				Journal:    ingest.JournalMetadata{Harness: harness.ReceiptEvidence{State: harness.ReceiptReported, Scope: "fixture", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", Label: fixture.run}},
				Projection: canonical.Batch{Signal: canonical.SignalTrace},
			}
			for i, step := range fixture.steps {
				ended := at.Add(time.Duration(step.duration) * time.Second)
				kind, name := canonical.ActivityTool, "gen_ai.tool.call"
				model := ""
				if step.tool == "" {
					kind, name = canonical.ActivityResponse, "gen_ai.response.completed"
					model = "fixture-model"
				}
				status := "Ok"
				if !step.success {
					status = "Error"
				}
				attrs := map[string]any{"success": step.success, "gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": fmt.Sprintf("%s-%d", fixture.run, i), "tool_input": "CONTEXT_SENTINEL"}
				if step.command != "" {
					attrs["command"] = step.command
				}
				if step.tool == "apply_patch" {
					attrs["file_path"] = "main.go"
				}
				if !step.success {
					attrs["error"] = "same fixture failure"
				}
				span := canonical.Span{Source: "codex", TraceID: "trace-" + fixture.run, SpanID: fmt.Sprintf("%016x", i+1), Name: name, Kind: kind, ToolName: step.tool,
					StartedAt: at, EndedAt: ended, Status: status, Content: "PROMPT_SENTINEL TOOL_SENTINEL", Attributes: attrs,
					Agent: canonical.AgentContext{RunID: fixture.run, AgentID: "main", Model: model, Tokens: canonical.TokenUsage{Input: int64(step.tokens), Presence: canonical.TokenPresence{Output: true}}},
				}
				accepted.Projection.Spans = append(accepted.Projection.Spans, span)
				accepted.Observations = append(accepted.Observations, observation.Observation{Ordinal: i, Signal: canonical.SignalTrace, Kind: kind, Source: "codex", SourceEventName: name, SessionID: fixture.run, TraceID: span.TraceID, SpanID: span.SpanID, OccurredAt: at, ObservedAt: ended, NormalizerVersion: 2})
				at = ended
			}
			if err := database.CommitExport(context.Background(), accepted); err != nil {
				t.Fatal(err)
			}
		}
		_, handler := New(database, nil, func() time.Time { return start.Add(48 * time.Hour) })
		connectServer := httptest.NewServer(handler)
		t.Cleanup(connectServer.Close)
		client := agentmetryv1connect.NewAgentmetryQueryServiceClient(connectServer.Client(), connectServer.URL)
		comparison, err := client.CompareRework(context.Background(), connect.NewRequest(&v1.CompareReworkRequest{
			Baseline: &v1.ReworkComparisonReference{SourceId: "codex", SessionId: "baseline"}, Current: &v1.ReworkComparisonReference{SourceId: "codex", SessionId: "current"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if comparison.Msg.Status != "ready" || comparison.Msg.Baseline.SessionId != "baseline" || comparison.Msg.Current.SessionId != "current" {
			t.Fatalf("unexpected identities/status: %v", comparison.Msg)
		}
		if comparison.Msg.Baseline.Coverage.CanonicalEvents != 6 || comparison.Msg.Current.Coverage.CanonicalEvents != 5 || comparison.Msg.Baseline.ProjectionCoverage != "complete" || comparison.Msg.Current.ProjectionCoverage != "complete" {
			t.Fatalf("unexpected coverage: %v", comparison.Msg)
		}
		if comparison.Msg.Baseline.HarnessContext.GetUniform().GetIdentity().GetLabel() != "baseline" || comparison.Msg.Current.HarnessContext.Counts.ReportedRecords != 5 {
			t.Fatalf("harness context missing from comparison: %v", comparison.Msg)
		}
		payload, err := protojson.Marshal(comparison.Msg)
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			Rows []query.ReworkComparisonRow `json:"rows"`
		}
		if err := json.Unmarshal(payload, &wire); err != nil {
			t.Fatal(err)
		}

		// Independent fixture oracle: baseline has 3 tests, 2 failures, 1 recurring loop,
		// 5 tool outcomes and a 5-second/500-token retry interval. Current has 3 validations
		// (vet + two tests), 1 failure, no recurring loop, 4 tool outcomes and a 3-second/300-token interval.
		// Both runs have 6 seconds of observed effort and 600 observed tokens.
		want := []struct {
			id, unit                      string
			bn, bd, bv, cn, cd, cv, delta float64
		}{
			{"initial_validation_success_proxy", "percent", 0, 1, 0, 1, 2, 50, 50},
			{"rework_token_share", "percent", 500, 600, 250.0 / 3, 300, 600, 50, -100.0 / 3},
			{"retry_cycle_effort_share", "percent", 5000, 6000, 250.0 / 3, 3000, 6000, 50, -100.0 / 3},
			{"tool_failure_rate", "percent", 2, 5, 40, 1, 4, 25, -15},
			{"recurring_loops_per_100_validations", "per100", 1, 3, 100.0 / 3, 0, 3, 0, -100.0 / 3},
		}
		if len(wire.Rows) != len(want) {
			t.Fatalf("row count = %d", len(wire.Rows))
		}
		for i, expected := range want {
			row := wire.Rows[i]
			if row.ID != expected.id || row.Unit != expected.unit || row.Availability != "comparable" || row.Baseline.Availability != "available" || row.Current.Availability != "available" || row.Baseline.Reason != "" || row.Current.Reason != "" {
				t.Errorf("row semantics = %#v, expected id/unit/available for %#v", row, expected)
			}
			got := []*float64{row.Baseline.Numerator, row.Baseline.Denominator, row.Baseline.Value, row.Current.Numerator, row.Current.Denominator, row.Current.Value, row.Delta}
			values := []float64{expected.bn, expected.bd, expected.bv, expected.cn, expected.cd, expected.cv, expected.delta}
			for j, value := range got {
				if value == nil || math.Abs(*value-values[j]) > 1e-9 {
					t.Errorf("%s field %d = %v, want %v", row.ID, j, value, values[j])
				}
			}
		}
		for _, sentinel := range []string{"PROMPT_SENTINEL", "TOOL_SENTINEL", "CONTEXT_SENTINEL"} {
			if strings.Contains(string(payload), sentinel) {
				t.Errorf("comparison exposed %s", sentinel)
			}
		}

		mcpServer := httptest.NewServer(mcpserver.New(database, func() time.Time { return start.Add(48 * time.Hour) }))
		t.Cleanup(mcpServer.Close)
		mcpClient := mcp.NewClient(&mcp.Implementation{Name: "comparison-parity-test", Version: "v1"}, nil)
		mcpSession, err := mcpClient.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: mcpServer.URL}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = mcpSession.Close() })
		result, err := mcpSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "compare_rework", Arguments: map[string]any{
			"baseline": map[string]any{"source": "codex", "runId": "baseline"}, "current": map[string]any{"source": "codex", "runId": "current"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("MCP comparison error: %v", result.Content)
		}
		mcpJSON, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var mcpWire struct {
			Status            string                      `json:"status"`
			Rows              []query.ReworkComparisonRow `json:"rows"`
			Baseline, Current struct {
				SourceID           string               `json:"sourceId"`
				SessionID          string               `json:"sessionId"`
				ProjectionCoverage string               `json:"projectionCoverage"`
				Coverage           query.ReworkCoverage `json:"coverage"`
				HarnessContext     struct {
					Classification string `json:"classification"`
					Counts         struct {
						ReportedRecords int64 `json:"reportedRecords"`
					} `json:"counts"`
				} `json:"harnessContext"`
			}
		}
		if err := json.Unmarshal(mcpJSON, &mcpWire); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(wire.Rows, mcpWire.Rows, cmpopts.EquateApprox(1e-12, 1e-12)); diff != "" {
			t.Errorf("Connect/MCP rows (-Connect +MCP): %s", diff)
		}
		if mcpWire.Status != "ready" || mcpWire.Baseline.SessionID != "baseline" || mcpWire.Current.SessionID != "current" || mcpWire.Baseline.Coverage.CanonicalEvents != 6 || mcpWire.Current.Coverage.CanonicalEvents != 5 || mcpWire.Baseline.HarnessContext.Classification != "uniform" || mcpWire.Current.HarnessContext.Counts.ReportedRecords != 5 {
			t.Errorf("MCP summary mismatch: %s", mcpJSON)
		}
		for _, sentinel := range []string{"PROMPT_SENTINEL", "TOOL_SENTINEL", "CONTEXT_SENTINEL"} {
			if strings.Contains(string(mcpJSON), sentinel) {
				t.Errorf("MCP comparison exposed %s", sentinel)
			}
		}
		tools, err := mcpSession.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		readOnly := false
		for _, tool := range tools.Tools {
			if tool.Name == "compare_rework" {
				readOnly = tool.Annotations != nil && tool.Annotations.ReadOnlyHint
			}
		}
		if !readOnly {
			t.Error("compare_rework is not annotated read-only")
		}
		// Keep aggregate comparison separate from diagnostic pair eligibility.
		if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{
			{Source: "claude", TraceID: "overlapping-claude", SpanID: "0000000000000001", Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, StartedAt: start, EndedAt: start.Add(time.Second), Agent: canonical.AgentContext{RunID: "baseline", AgentID: "main", Tokens: canonical.TokenUsage{Input: 7, Presence: canonical.TokenPresence{Output: true}}}},
			{Source: "codex", TraceID: "sparse-codex", SpanID: "0000000000000001", Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, StartedAt: start.Add(48 * time.Hour), EndedAt: start.Add(48*time.Hour + time.Second), Agent: canonical.AgentContext{RunID: "sparse", AgentID: "main", Tokens: canonical.TokenUsage{Input: 10, Presence: canonical.TokenPresence{Output: true}}}},
		}}); err != nil {
			t.Fatal(err)
		}
		for _, count := range []int{1, 10} {
			runs := make([]map[string]string, count)
			for i := range runs {
				source := "codex"
				if i%2 == 1 {
					source = "claude"
				}
				runs[i] = map[string]string{"source": source, "runId": "baseline"}
			}
			for _, dimensions := range [][]string{nil, {"totalTokens"}} {
				args := map[string]any{"runs": runs}
				if dimensions != nil {
					args["dimensions"] = dimensions
				}
				result, err := mcpSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "compare_runs", Arguments: args})
				if err != nil {
					t.Fatal(err)
				}
				if result.IsError {
					t.Fatalf("legacy aggregate comparison rejected count=%d: %v", count, result.Content)
				}
				encoded, err := json.Marshal(result.StructuredContent)
				if err != nil {
					t.Fatal(err)
				}
				var aggregate mcpserver.CompareRunsOutput
				if err := json.Unmarshal(encoded, &aggregate); err != nil {
					t.Fatal(err)
				}
				wantDimensions := dimensions
				if wantDimensions == nil {
					wantDimensions = []string{"wallDuration", "activityCount", "agentCount", "totalTokens", "costUsd"}
				}
				if diff := cmp.Diff(wantDimensions, aggregate.Dimensions); diff != "" {
					t.Errorf("aggregate dimensions mismatch: %s", diff)
				}
				if len(aggregate.Runs) != count {
					t.Fatalf("aggregate run count = %d", len(aggregate.Runs))
				}
				for i, run := range aggregate.Runs {
					wantTokens, wantActivities, wantDuration := int64(600), int64(6), int64(5000)
					if i%2 == 1 {
						wantTokens, wantActivities, wantDuration = 7, 1, 0
					}
					if run.SourceID != runs[i]["source"] || run.RunID != "baseline" || run.TotalTokens == nil || *run.TotalTokens != wantTokens || run.ActivityCount != wantActivities || run.WallDurationMs != wantDuration || run.AgentCount != 1 || run.CostUSD != nil {
						t.Errorf("aggregate result %d mismatch: %#v", i, run)
					}
				}
			}
		}
		for _, count := range []int{0, 11} {
			runs := make([]map[string]string, count)
			for i := range runs {
				runs[i] = map[string]string{"source": "codex", "runId": "baseline"}
			}
			result, err := mcpSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "compare_runs", Arguments: map[string]any{"runs": runs}})
			if err == nil && !result.IsError {
				t.Errorf("aggregate accepted %d runs", count)
			}
		}
		for _, include := range []bool{false, true} {
			result, err := mcpSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_run_timeline", Arguments: map[string]any{"source": "codex", "runId": "baseline", "includeContent": include, "pageSize": 2}})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError {
				t.Fatalf("timeline failed: %v", result.Content)
			}
			encoded, err := json.Marshal(result.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "PROMPT_SENTINEL") != include {
				t.Errorf("timeline body opt-in=%t mismatch: %s", include, encoded)
			}
		}
		result, err = mcpSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_run_timeline", Arguments: map[string]any{"source": "codex", "runId": "baseline", "pageSize": 101}})
		if err == nil && !result.IsError {
			t.Error("timeline accepted pageSize=101")
		}

		sparse, err := client.CompareRework(context.Background(), connect.NewRequest(&v1.CompareReworkRequest{
			Baseline: &v1.ReworkComparisonReference{SourceId: "codex", SessionId: "baseline"}, Current: &v1.ReworkComparisonReference{SourceId: "codex", SessionId: "sparse"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if sparse.Msg.Status != "ready" || sparse.Msg.Rows[1].Current.Numerator != nil || sparse.Msg.Rows[1].Current.Denominator == nil || *sparse.Msg.Rows[1].Current.Denominator != 10 || sparse.Msg.Rows[1].Current.Value != nil || sparse.Msg.Rows[1].Delta != nil || sparse.Msg.Rows[1].Current.Reason == "" {
			t.Errorf("sparse numerator lost null/denominator/reason: %v", sparse.Msg)
		}
		sparseResult, err := mcpSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "compare_rework", Arguments: map[string]any{"baseline": map[string]string{"source": "codex", "runId": "baseline"}, "current": map[string]string{"source": "codex", "runId": "sparse"}}})
		if err != nil {
			t.Fatal(err)
		}
		if sparseResult.IsError {
			t.Fatalf("sparse comparison: %v", sparseResult.Content)
		}
		sparseJSON, err := json.Marshal(sparseResult.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var sparseWire struct {
			Rows []query.ReworkComparisonRow `json:"rows"`
		}
		if err := json.Unmarshal(sparseJSON, &sparseWire); err != nil {
			t.Fatal(err)
		}
		if sparseWire.Rows[1].Current.Numerator != nil || sparseWire.Rows[1].Current.Value != nil || sparseWire.Rows[1].Delta != nil || sparseWire.Rows[1].Current.Denominator == nil || *sparseWire.Rows[1].Current.Denominator != 10 || sparseWire.Rows[1].Current.Reason != sparse.Msg.Rows[1].Current.Reason {
			t.Errorf("MCP sparse evidence mismatch: %s", sparseJSON)
		}
		invalid, err := client.CompareRework(context.Background(), connect.NewRequest(&v1.CompareReworkRequest{Baseline: &v1.ReworkComparisonReference{SourceId: "codex", SessionId: "baseline"}, Current: &v1.ReworkComparisonReference{SourceId: "claude", SessionId: "baseline"}}))
		if err != nil {
			t.Fatal(err)
		}
		if invalid.Msg.Status != "invalid" || invalid.Msg.Code != "baseline_ineligible" || len(invalid.Msg.Rows) != 0 {
			t.Errorf("cross-source diagnostic pair was accepted: %v", invalid.Msg)
		}
		invalidResult, err := mcpSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "compare_rework", Arguments: map[string]any{"baseline": map[string]string{"source": "codex", "runId": "baseline"}, "current": map[string]string{"source": "claude", "runId": "baseline"}}})
		if err != nil {
			t.Fatal(err)
		}
		if invalidResult.IsError {
			t.Fatalf("pair eligibility should be structured: %v", invalidResult.Content)
		}
		invalidJSON, err := json.Marshal(invalidResult.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var invalidWire struct {
			Status, Code string
			Rows         []query.ReworkComparisonRow
		}
		if err := json.Unmarshal(invalidJSON, &invalidWire); err != nil {
			t.Fatal(err)
		}
		if invalidWire.Status != "invalid" || invalidWire.Code != "baseline_ineligible" || len(invalidWire.Rows) != 0 {
			t.Errorf("MCP pair eligibility differs: %s", invalidJSON)
		}
		_, err = client.CompareRework(context.Background(), connect.NewRequest(&v1.CompareReworkRequest{}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("missing references accepted: %v", err)
		}
		_, err = client.CompareRework(context.Background(), connect.NewRequest(&v1.CompareReworkRequest{Baseline: &v1.ReworkComparisonReference{SourceId: "codex", SessionId: "missing"}, Current: &v1.ReworkComparisonReference{SourceId: "codex", SessionId: "current"}}))
		if connect.CodeOf(err) != connect.CodeNotFound {
			t.Errorf("missing run mapping: %v", err)
		}
		missingResult, err := mcpSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "compare_rework", Arguments: map[string]any{"baseline": map[string]string{"source": "codex", "runId": "missing"}, "current": map[string]string{"source": "codex", "runId": "current"}}})
		if err == nil && !missingResult.IsError {
			t.Error("MCP accepted missing baseline")
		}
		conditions := query.SessionConditions{ObservedFailure: true, MinDurationMS: new(6000.0), MaxDurationMS: new(6000.0), Model: "fixture-model", Tool: "exec_command"}
		wireConditions := &v1.SessionConditions{ObservedFailure: true, MinDurationMs: new(6000.0), MaxDurationMs: new(6000.0), Model: "fixture-model", Tool: "exec_command"}
		filtered, err := client.ListSessions(context.Background(), connect.NewRequest(&v1.ListSessionsRequest{Filter: &v1.TimeFilter{Range: v1.TimeRange_TIME_RANGE_SEVEN_DAYS, SourceId: "codex", Search: "baseline"}, Page: &v1.PageRequest{PageSize: 1}, Conditions: wireConditions}))
		if err != nil {
			t.Fatal(err)
		}
		if len(filtered.Msg.Sessions) != 1 || filtered.Msg.Sessions[0].Id != "baseline" || filtered.Msg.GetPage().GetHasMore() {
			t.Errorf("Connect stored filters = %v", filtered.Msg)
		}
		if diff := cmp.Diff(wireConditions, filtered.Msg.AppliedConditions, protocmp.Transform()); diff != "" {
			t.Errorf("Connect stored filter acknowledgement: %s", diff)
		}
		listed, err := mcpSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_runs", Arguments: map[string]any{"range": "7d", "source": "codex", "search": "baseline", "pageSize": 1, "conditions": conditions}})
		if err != nil {
			t.Fatal(err)
		}
		if listed.IsError {
			t.Fatalf("MCP stored filter error: %v", listed.Content)
		}
		listedJSON, err := json.Marshal(listed.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var listedWire mcpserver.OverviewOutput
		if err := json.Unmarshal(listedJSON, &listedWire); err != nil {
			t.Fatal(err)
		}
		if len(listedWire.Overview.Sessions) != 1 || listedWire.Overview.Sessions[0].ID != "baseline" || listedWire.NextPageToken != "" {
			t.Errorf("MCP stored filters = %s", listedJSON)
		}
		if diff := cmp.Diff(&conditions, listedWire.AppliedConditions); diff != "" {
			t.Errorf("MCP stored filter acknowledgement: %s", diff)
		}
		invalidConditions, err := mcpSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_runs", Arguments: map[string]any{"conditions": map[string]any{"minDurationMs": 2, "maxDurationMs": 1}}})
		if err == nil && !invalidConditions.IsError {
			t.Error("MCP accepted inverted filter bounds")
		}

	})

}

func TestMapSessionReworkRejectsMissingHarnessContext(t *testing.T) {
	if _, err := mapSessionRework(query.SessionRework{}); err == nil {
		t.Fatal("mapSessionRework accepted a missing harness context")
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

	_, err = client.GetTrace(context.Background(), connect.NewRequest(&v1.GetTraceRequest{
		TraceId: traceID, AnchorSpanId: "ABCDEFABCDEFABCD", LiveTail: true, Page: &v1.PageRequest{PageSize: 3, PageToken: encodePageToken(110)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if reader.lastTrace.SpanID.String() != "abcdefabcdefabcd" || reader.lastTrace.Page.Size() != 3 || reader.lastTrace.Page.Offset() != 110 {
		t.Fatalf("anchor not forwarded in typed trace filter: %#v", reader.lastTrace)
	}
	for _, anchor := range []string{"not-a-span", "0000000000000000", " ABCDEFABCDEFABCD"} {
		reader.lastTrace = query.TraceFilter{}
		_, err := client.GetTrace(context.Background(), connect.NewRequest(&v1.GetTraceRequest{TraceId: traceID, AnchorSpanId: anchor}))
		if connect.CodeOf(err) != connect.CodeInvalidArgument || reader.lastTrace.TraceID.String() != "" {
			t.Errorf("invalid anchor %q reached reader or wrong error: %v", anchor, err)
		}
	}
	_, err = client.GetTrace(context.Background(), connect.NewRequest(&v1.GetTraceRequest{
		TraceId: traceID, AnchorSpanId: "abcdefabcdefabcd", Page: &v1.PageRequest{PageSize: 101},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("anchor bypassed page bound: %v", err)
	}
	reader.traceErr = query.ErrTraceTargetNotFound
	_, err = client.GetTrace(context.Background(), connect.NewRequest(&v1.GetTraceRequest{TraceId: traceID, AnchorSpanId: "abcdefabcdefabcd"}))
	if connect.CodeOf(err) != connect.CodeNotFound || !strings.Contains(err.Error(), "target span not found") {
		t.Errorf("missing native target mapping = %v", err)
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

func Test_mapActivities(t *testing.T) {
	type args struct {
		values []query.Activity
	}
	tests := []struct {
		name string
		args args
		want []*v1.Activity
	}{
		{
			name: "redacted prompt has evidence but no readable body",
			args: args{values: []query.Activity{{ID: "a", Source: "codex", Signal: "log", Name: "gen_ai.user_prompt", Content: "[REDACTED]", Attributes: map[string]any{"prompt": "[REDACTED]"}}}},
			want: []*v1.Activity{{Id: "a", Source: "codex", Signal: "log", Name: "gen_ai.user_prompt", Tokens: &v1.TokenUsage{}, ContentEvidence: &v1.ContentEvidence{Source: "codex", ActivityId: "a", Signal: "log", Kind: "prompt", Evidence: "unknown", Availability: "redacted", Fields: []string{"prompt"}, RedactionReason: "producer_redacted"}}},
		},
		{
			name: "reference text remains distinct from a received body",
			args: args{values: []query.Activity{{ID: "a", Source: "claude", Signal: "log", Content: "file:///private/request.json", Attributes: map[string]any{"body_ref": "file:///private/request.json"}}}},
			want: []*v1.Activity{{Id: "a", Source: "claude", Signal: "log", Content: "file:///private/request.json", Tokens: &v1.TokenUsage{}, ContentEvidence: &v1.ContentEvidence{Source: "claude", ActivityId: "a", Signal: "log", Kind: "reference", Evidence: "reference", Availability: "not_reported", Fields: []string{"body_ref"}}}},
		},
		{
			name: "read output metadata includes no arbitrary attribute values",
			args: args{values: []query.Activity{{ID: "a", Source: "codex", Signal: "log", Content: "Result: body sentinel", Attributes: map[string]any{"output": "body sentinel", "secret": "attribute sentinel"}}}},
			want: []*v1.Activity{{Id: "a", Source: "codex", Signal: "log", Content: "Result: body sentinel", Tokens: &v1.TokenUsage{}, ContentEvidence: &v1.ContentEvidence{Source: "codex", ActivityId: "a", Signal: "log", Kind: "tool_output", Evidence: "read_output", Availability: "available", Fields: []string{"output"}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapActivities(tt.args.values); !cmp.Equal(tt.want, got, protocmp.Transform()) {
				t.Errorf("mapActivities() = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got, protocmp.Transform()))
			}
		})
	}
}

func TestActivityContentEvidencePublicTransportParity(t *testing.T) {
	harnessContext, err := query.ClassifyHarnessEvidence(query.HarnessEvidenceFacts{})
	if err != nil {
		t.Fatal(err)
	}
	reader := &readerStub{activities: query.ActivityPage{Total: 3, Activities: []query.Activity{
		{ID: "body", Source: "codex", Signal: "log", Content: "Result: BODY_SENTINEL", Attributes: map[string]any{"output": "BODY_SENTINEL", "secret": "ATTRIBUTE_SENTINEL"}},
		{ID: "absent", Source: "codex", Signal: "log", Name: "gen_ai.user_prompt"},
		{ID: "redacted", Source: "codex", Signal: "log", Content: "[REDACTED]", Attributes: map[string]any{"prompt": "[REDACTED]"}},
	}}, rework: query.SessionRework{Harness: harnessContext, Report: query.ReworkReport{Coverage: query.ReworkCoverage{ActivityCoverage: query.ActivityCoverageComplete, CanonicalEvents: 3}}}}
	_, handler := New(reader, nil, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)
	response, err := client.ListSessionActivities(context.Background(), connect.NewRequest(&v1.ListSessionActivitiesRequest{SourceId: "codex", SessionId: "run"}))
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := client.GetSessionRework(context.Background(), connect.NewRequest(&v1.GetSessionReworkRequest{SourceId: "codex", SessionId: "run"}))
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Msg.Coverage.ActivityCoverage != query.ActivityCoverageComplete {
		t.Fatalf("analysis coverage changed: %v", analysis.Msg.Coverage)
	}
	want := []query.ContentEvidence{
		{Source: "codex", ActivityID: "body", Signal: "log", Kind: "tool_output", Evidence: "read_output", Availability: "available", Fields: []string{"output"}},
		{Source: "codex", ActivityID: "absent", Signal: "log", Kind: "prompt", Evidence: "unknown", Availability: "not_reported"},
		{Source: "codex", ActivityID: "redacted", Signal: "log", Kind: "prompt", Evidence: "unknown", Availability: "redacted", Fields: []string{"prompt"}, RedactionReason: "producer_redacted"},
	}
	if len(response.Msg.Activities) != len(want) {
		t.Fatalf("activity count: %d", len(response.Msg.Activities))
	}
	for i, activity := range response.Msg.Activities {
		payload, err := protojson.Marshal(activity.ContentEvidence)
		if err != nil {
			t.Fatal(err)
		}
		var got query.ContentEvidence
		if err := json.Unmarshal(payload, &got); err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(want[i], got); diff != "" {
			t.Fatalf("Connect evidence (-want +got): %s", diff)
		}
	}
	mcpServer := httptest.NewServer(mcpserver.New(reader, time.Now))
	t.Cleanup(mcpServer.Close)
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "content-parity-test", Version: "v1"}, nil)
	session, err := mcpClient.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: mcpServer.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	for _, include := range []bool{false, true} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "get_run_timeline", Arguments: map[string]any{"source": "codex", "runId": "run", "includeContent": include}})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("MCP error: %v", result.Content)
		}
		payload, err := json.Marshal(result.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Activities []struct {
				Content         string                `json:"content"`
				ContentEvidence query.ContentEvidence `json:"contentEvidence"`
			} `json:"activities"`
		}
		if err := json.Unmarshal(payload, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Activities) != len(want) {
			t.Fatalf("MCP activity count: %d", len(got.Activities))
		}
		for i, activity := range got.Activities {
			expected := want[i]
			if !include {
				expected.Availability = "not_returned"
			}
			if diff := cmp.Diff(expected, activity.ContentEvidence); diff != "" {
				t.Fatalf("MCP evidence (-want +got): %s", diff)
			}
			if include && activity.Content != response.Msg.Activities[i].Content {
				t.Fatalf("body mismatch: %q vs %q", activity.Content, response.Msg.Activities[i].Content)
			}
		}
		if strings.Contains(string(payload), "ATTRIBUTE_SENTINEL") || (!include && strings.Contains(string(payload), "BODY_SENTINEL")) || strings.Contains(string(payload), "[REDACTED]") {
			t.Fatalf("content leaked: %s", payload)
		}
	}
}
