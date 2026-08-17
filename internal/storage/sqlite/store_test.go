package sqlite_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/harness"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/observation"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/source/claude"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
	"github.com/kotokumu/agentmetry/sourceplugin"
)

func mustConversationIdentity(t *testing.T, sourceID, conversationID string) query.ConversationIdentity {
	t.Helper()
	identity, err := query.NewConversationIdentity(sourceID, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func mustTraceID(t *testing.T, value string) query.TraceID {
	t.Helper()
	identity, err := query.ParseTraceID(value)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func mustActivityAnchor(t *testing.T, traceID, spanID string) query.ActivityAnchor {
	t.Helper()
	anchor, err := query.NewActivityAnchor(traceID, spanID)
	if err != nil {
		t.Fatal(err)
	}
	return anchor
}

func mustPage(t *testing.T, offset, size int) query.Page {
	t.Helper()
	page, err := query.NewPage(offset, size)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestCommitBatchReplacesSpanRevisionAndBuildsOverview(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	now := time.Now().UTC().Truncate(time.Millisecond)
	span := canonical.Span{
		TraceID:   "trace-1",
		SpanID:    "span-1",
		Name:      "delegate to reviewer",
		Kind:      canonical.ActivityDelegation,
		StartedAt: now.Add(-time.Second),
		EndedAt:   now,
		Agent: canonical.AgentContext{
			AgentID: "reviewer",
			RunID:   "run-1",
			Model:   "example-model",
			Tokens:  canonical.TokenUsage{Input: 10, Output: 5},
		},
	}
	ctx := context.Background()
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	span.Agent.Tokens = canonical.TokenUsage{Input: 20, Output: 7}
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	logAgent := span.Agent
	logAgent.Tokens = canonical.TokenUsage{Input: 3, Output: 2}
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{{
		ObservedAt: now,
		Severity:   "INFO",
		Body:       "review started",
		Kind:       canonical.ActivityResponse,
		Agent:      logAgent,
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalMetric, Metrics: []canonical.MetricPoint{{
		ObservedAt: now,
		Name:       "agentmetry.test.metric",
		Kind:       "gauge",
		Value:      1,
		Agent:      span.Agent,
	}}}); err != nil {
		t.Fatal(err)
	}

	overview, err := database.GetOverview(ctx, query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if overview.SignalCounts != (query.SignalCounts{Traces: 1, Logs: 1, Metrics: 1}) {
		t.Fatalf("unexpected counts: %#v", overview.SignalCounts)
	}
	if overview.AgentCount != 1 || overview.RunCount != 1 {
		t.Fatalf("unexpected identity counts: %#v", overview)
	}
	if overview.Tokens.Input != 23 || overview.Tokens.Output != 9 || overview.Tokens.Total() != 32 {
		t.Fatalf("span replacement inflated tokens: %#v", overview.Tokens)
	}
	if len(overview.RecentActivity) != 2 {
		t.Fatalf("recent activity length = %d, want 2", len(overview.RecentActivity))
	}
	dashboard, err := database.GetDashboard(ctx, query.DashboardFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.RunCount != 1 || dashboard.AgentCount != 1 {
		t.Fatalf("unexpected dashboard identity counts: %#v", dashboard)
	}
	summary, err := database.GetSessionSummary(ctx, mustConversationIdentity(t, "unknown", "run-1"))
	if err != nil {
		t.Fatal(err)
	}
	if summary.ActivityCount != 2 || summary.Tokens.Total() != 32 || len(summary.Agents) != 1 || summary.Agents[0].AgentID != "reviewer" {
		t.Fatalf("unexpected session summary: %#v", summary)
	}
}

func TestListSessionActivitiesHydratesInternalAnalysisAttributes(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	span := canonical.Span{
		Source: "codex", TraceID: "trace-attributes", SpanID: "span-attributes",
		Name: "gen_ai.tool.call", Kind: canonical.ActivityTool, ToolName: "exec_command",
		StartedAt: now.Add(-time.Second), EndedAt: now,
		Attributes: map[string]any{"arguments": map[string]any{"cmd": "go test ./..."}, "exit_code": 1, "gen_ai.usage.role": "authoritative_call"},
		Agent: canonical.AgentContext{
			RunID:  "run-attributes",
			Tokens: canonical.TokenUsage{Input: 10, Presence: canonical.TokenPresence{Output: true}},
		},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}

	page, err := database.ListSessionActivities(context.Background(), query.ActivityPageFilter{
		Identity: mustConversationIdentity(t, "codex", "run-attributes"), Page: mustPage(t, 0, 100),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Activities) != 1 {
		t.Fatalf("activities = %#v", page.Activities)
	}
	arguments, ok := page.Activities[0].Attributes["arguments"].(map[string]any)
	if !ok || arguments["cmd"] != "go test ./..." {
		t.Fatalf("analysis attributes were not hydrated: %#v", page.Activities[0].Attributes)
	}
}

func TestGetSessionReworkAnalyzesTheCompleteStoredSession(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	start := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	spans := []canonical.Span{
		{Source: "codex", TraceID: "trace-rework", SpanID: "fail", Name: "gen_ai.tool.call", Kind: canonical.ActivityTool, ToolName: "exec_command", StartedAt: start, EndedAt: start.Add(time.Second), Status: "Error", Attributes: map[string]any{"command": "go test ./..."}, Agent: canonical.AgentContext{RunID: "run-rework"}},
		{Source: "codex", TraceID: "trace-rework", SpanID: "edit", Name: "gen_ai.tool.call", Kind: canonical.ActivityTool, ToolName: "apply_patch", StartedAt: start.Add(2 * time.Second), EndedAt: start.Add(3 * time.Second), Status: "Ok", Attributes: map[string]any{"file_path": "main.go"}, Agent: canonical.AgentContext{RunID: "run-rework"}},
		{Source: "codex", TraceID: "trace-rework", SpanID: "retry", Name: "gen_ai.tool.call", Kind: canonical.ActivityTool, ToolName: "exec_command", StartedAt: start.Add(4 * time.Second), EndedAt: start.Add(5 * time.Second), Status: "Ok", Attributes: map[string]any{"command": "go test ./..."}, Agent: canonical.AgentContext{RunID: "run-rework"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: spans}); err != nil {
		t.Fatal(err)
	}

	analysis, err := database.GetSessionRework(context.Background(), mustConversationIdentity(t, "codex", "run-rework"))
	if err != nil {
		t.Fatal(err)
	}
	if analysis.SourceID != "codex" || analysis.RunID != "run-rework" || analysis.Report.ValidationFailures != 1 || analysis.Report.FailFixRetryCycles != 1 {
		t.Fatalf("unexpected session rework analysis: %#v", analysis)
	}
	if analysis.Report.ReworkDuration != 3*time.Second || analysis.Report.Coverage.ActivityCoverage != query.ActivityCoverageComplete {
		t.Fatalf("unexpected effort/coverage: %#v", analysis.Report)
	}
	harnessView, err := query.InspectHarnessContext(analysis.Harness)
	if err != nil || harnessView.Classification != query.HarnessNoEligibleRecords {
		t.Fatalf("harness context = %#v, want no eligible records", analysis.Harness)
	}
}

func TestGetSessionReworkClassifiesCompleteHarnessEvidence(t *testing.T) {
	tests := []struct {
		name     string
		receipts []harness.ReceiptEvidence
		want     query.HarnessContextView
	}{
		{
			name:     "unreported",
			receipts: []harness.ReceiptEvidence{{State: harness.ReceiptUnreported}, {State: harness.ReceiptUnreported}},
			want:     query.HarnessContextView{Classification: query.HarnessUnreported, Counts: query.HarnessEvidenceCounts{EligibleRecords: 2, UnreportedRecords: 2}},
		},
		{
			name: "uniform",
			receipts: []harness.ReceiptEvidence{
				{State: harness.ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", Label: "zeta"},
				{State: harness.ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", Label: "alpha"},
			},
			want: query.HarnessContextView{Classification: query.HarnessUniform, Counts: query.HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 2, DistinctIdentities: 1}, Identity: &query.HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", Label: "alpha"}},
		},
		{
			name: "mixed",
			receipts: []harness.ReceiptEvidence{
				{State: harness.ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"},
				{State: harness.ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:dfbc1de58f3b905c7b0c0fd79361699336b5f9da617b1db8f35c76673f95b29d"},
			},
			want: query.HarnessContextView{Classification: query.HarnessMixed, Counts: query.HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 2, DistinctIdentities: 2}},
		},
		{
			name: "incomplete",
			receipts: []harness.ReceiptEvidence{
				{State: harness.ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"},
				{State: harness.ReceiptUnreported},
			},
			want: query.HarnessContextView{Classification: query.HarnessIncomplete, Counts: query.HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 1, UnreportedRecords: 1, DistinctIdentities: 1}},
		},
		{
			name: "invalid",
			receipts: []harness.ReceiptEvidence{
				{State: harness.ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"},
				{State: harness.ReceiptInvalid},
			},
			want: query.HarnessContextView{Classification: query.HarnessInvalid, Counts: query.HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 1, InvalidRecords: 1, DistinctIdentities: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			started := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
			for index, receipt := range tt.receipts {
				observedAt := started.Add(time.Duration(index) * time.Second)
				accepted := ingest.AcceptedExport{
					Envelope:     ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, observedAt, []byte{0x0a, byte(index)}),
					Journal:      ingest.JournalMetadata{Harness: receipt},
					Observations: []observation.Observation{{Ordinal: 0, Signal: canonical.SignalLog, Kind: canonical.ActivityResponse, Source: "codex", SourceEventName: "response", OccurredAt: observedAt, ObservedAt: observedAt, SessionID: "run-harness", NormalizerVersion: 1}},
					Projection:   canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{{Source: "codex", ObservedAt: observedAt, Name: "response", Kind: canonical.ActivityResponse, Attributes: map[string]any{}, Agent: canonical.AgentContext{RunID: "run-harness", Tokens: canonical.TokenUsage{Input: 10}}}}},
				}
				if err := database.CommitExport(context.Background(), accepted); err != nil {
					t.Fatal(err)
				}
			}
			unrelated := ingest.AcceptedExport{
				Envelope:     ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, started.Add(time.Minute), []byte{0x0a, 0xff}),
				Journal:      ingest.JournalMetadata{Harness: harness.ReceiptEvidence{State: harness.ReceiptInvalid}},
				Observations: []observation.Observation{{Ordinal: 0, Signal: canonical.SignalLog, Kind: canonical.ActivityResponse, Source: "claude", SourceEventName: "response", OccurredAt: started.Add(time.Minute), ObservedAt: started.Add(time.Minute), SessionID: "run-harness", NormalizerVersion: 1}},
				Projection:   canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{{Source: "claude", ObservedAt: started.Add(time.Minute), Name: "response", Kind: canonical.ActivityResponse, Attributes: map[string]any{}, Agent: canonical.AgentContext{RunID: "run-harness"}}}},
			}
			if err := database.CommitExport(context.Background(), unrelated); err != nil {
				t.Fatal(err)
			}
			analysis, err := database.GetSessionRework(context.Background(), mustConversationIdentity(t, "codex", "run-harness"))
			if err != nil {
				t.Fatal(err)
			}
			view, err := query.InspectHarnessContext(analysis.Harness)
			if err != nil || !reflect.DeepEqual(view, tt.want) {
				t.Fatalf("harness context = %#v (%v), want %#v", view, err, tt.want)
			}
			if analysis.SessionTokens.Total() != int64(len(tt.receipts))*10 {
				t.Fatalf("session token total = %d, want %d", analysis.SessionTokens.Total(), int64(len(tt.receipts))*10)
			}
		})
	}
}

func TestGetSessionReworkAggregatesChildActivitiesIntoCanonicalRoot(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	start := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	spans := []canonical.Span{
		{Source: "codex", TraceID: "trace-child-rework", SpanID: "fail", Name: "gen_ai.tool.call", Kind: canonical.ActivityTool, ToolName: "exec_command", StartedAt: start, EndedAt: start.Add(time.Second), Status: "Error", Attributes: map[string]any{"command": "go test ./..."}, Agent: canonical.AgentContext{RunID: "child"}},
		{Source: "codex", TraceID: "trace-child-rework", SpanID: "edit", Name: "gen_ai.tool.call", Kind: canonical.ActivityTool, ToolName: "apply_patch", StartedAt: start.Add(2 * time.Second), EndedAt: start.Add(3 * time.Second), Status: "Ok", Attributes: map[string]any{"file_path": "main.go"}, Agent: canonical.AgentContext{RunID: "child"}},
		{Source: "codex", TraceID: "trace-child-rework", SpanID: "retry", Name: "gen_ai.tool.call", Kind: canonical.ActivityTool, ToolName: "exec_command", StartedAt: start.Add(4 * time.Second), EndedAt: start.Add(5 * time.Second), Status: "Ok", Attributes: map[string]any{"command": "go test ./..."}, Agent: canonical.AgentContext{RunID: "child"}},
	}
	batch := canonical.Batch{
		Signal: canonical.SignalTrace,
		Spans:  spans,
		SessionLinks: []canonical.SessionLink{{
			Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: start,
		}},
	}
	if err := database.CommitBatch(context.Background(), batch); err != nil {
		t.Fatal(err)
	}

	analysis, err := database.GetSessionRework(context.Background(), mustConversationIdentity(t, "codex", "child"))
	if err != nil {
		t.Fatal(err)
	}
	if analysis.RunID != "parent" || analysis.Report.ValidationFailures != 1 || analysis.Report.FailFixRetryCycles != 1 {
		t.Fatalf("aggregated child rework = %#v", analysis)
	}
}

func TestGetSessionReworkAggregatesChildHarnessEvidenceIntoCanonicalRoot(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	observedAt := time.Date(2026, 8, 17, 1, 0, 0, 0, time.UTC)
	receipt := harness.ReceiptEvidence{State: harness.ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", Label: "AGENTS v2"}
	accepted := ingest.AcceptedExport{
		Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, observedAt, []byte{0x0a, 0x00}),
		Journal:  ingest.JournalMetadata{Harness: receipt},
		Observations: []observation.Observation{{
			Ordinal: 0, Signal: canonical.SignalLog, Kind: canonical.ActivityResponse, Source: "codex",
			SourceEventName: "response", OccurredAt: observedAt, ObservedAt: observedAt,
			SessionID: "child", NormalizerVersion: 1,
		}},
		Projection: canonical.Batch{
			Signal:       canonical.SignalLog,
			Logs:         []canonical.Log{{Source: "codex", ObservedAt: observedAt, Name: "response", Kind: canonical.ActivityResponse, Attributes: map[string]any{}, Agent: canonical.AgentContext{RunID: "child"}}},
			SessionLinks: []canonical.SessionLink{{Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: observedAt}},
		},
	}
	if err := database.CommitExport(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}

	analysis, err := database.GetSessionRework(context.Background(), mustConversationIdentity(t, "codex", "child"))
	if err != nil {
		t.Fatal(err)
	}
	want := query.HarnessContextView{
		Classification: query.HarnessUniform,
		Counts:         query.HarnessEvidenceCounts{EligibleRecords: 1, ReportedRecords: 1, DistinctIdentities: 1},
		Identity:       &query.HarnessIdentity{Scope: receipt.Scope, Fingerprint: receipt.Fingerprint, Label: receipt.Label},
	}
	view, err := query.InspectHarnessContext(analysis.Harness)
	if err != nil || analysis.RunID != "parent" || !reflect.DeepEqual(view, want) {
		t.Fatalf("aggregated child harness context = %#v", analysis)
	}
}

func TestSpanRevisionRepairsSessionTimeExtrema(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	span := canonical.Span{
		Source: "codex", TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef",
		Kind: canonical.ActivityResponse, StartedAt: now.Add(-time.Second), EndedAt: now,
		Agent: canonical.AgentContext{RunID: "revised-session", AgentID: "main"},
	}
	ctx := context.Background()
	if err := database.CommitBatch(ctx, canonical.Batch{Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	span.EndedAt = now.Add(time.Minute)
	if err := database.CommitBatch(ctx, canonical.Batch{Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	session, err := database.GetSessionSummary(ctx, mustConversationIdentity(t, "codex", "revised-session"))
	if err != nil {
		t.Fatal(err)
	}
	if !session.StartedAt.Equal(span.EndedAt) || !session.EndedAt.Equal(span.EndedAt) || session.ActivityCount != 1 {
		t.Fatalf("revised session extrema = %s..%s count=%d, want %s", session.StartedAt, session.EndedAt, session.ActivityCount, span.EndedAt)
	}
}

func TestSpanRevisionToUnknownRemovesEmptySessionAndTraceRollups(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	span := canonical.Span{
		Source: "codex", TraceID: "0123456789abcdef0123456789abcdef", SpanID: "0123456789abcdef",
		Kind: canonical.ActivityResponse, StartedAt: now, EndedAt: now,
		Agent: canonical.AgentContext{RunID: "kind-session", AgentID: "main"},
	}
	ctx := context.Background()
	if err := database.CommitBatch(ctx, canonical.Batch{Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	span.Kind = canonical.ActivityUnknown
	if err := database.CommitBatch(ctx, canonical.Batch{Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetSessionSummary(ctx, mustConversationIdentity(t, "codex", "kind-session")); !errors.Is(err, query.ErrConversationNotFound) {
		t.Fatalf("session error = %v, want not found", err)
	}
	if _, err := database.GetTrace(ctx, query.TraceFilter{TraceID: mustTraceID(t, span.TraceID), Page: mustPage(t, 0, 100)}); !errors.Is(err, query.ErrTraceNotFound) {
		t.Fatalf("trace error = %v, want not found", err)
	}
}

func TestListSessionsSearchesSessionID(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{
			ObservedAt: now,
			Name:       "gen_ai.agent.message",
			Body:       "ordinary activity",
			Kind:       canonical.ActivityMessage,
			Agent:      canonical.AgentContext{RunID: "session-Alpha-123"},
		},
		{
			ObservedAt: now.Add(time.Second),
			Name:       "gen_ai.agent.message",
			Body:       "ordinary activity",
			Kind:       canonical.ActivityMessage,
			Agent:      canonical.AgentContext{RunID: "session-beta-456"},
		},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}

	page, err := database.ListSessions(context.Background(), query.SessionListFilter{
		Since:  now.Add(-time.Hour),
		Search: "ALPHA-12",
		Page:   mustPage(t, 0, 100),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 || page.Sessions[0].ID != "session-Alpha-123" {
		t.Fatalf("session ID search returned %#v", page.Sessions)
	}
}

func TestOverviewKeepsConversationsSeparateWhenTheyShareATrace(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{ObservedAt: now, Name: "gen_ai.session.start", TraceID: "trace-shared", Agent: canonical.AgentContext{RunID: "parent-run", Model: "gpt-parent"}},
		{ObservedAt: now.Add(time.Second), Name: "gen_ai.tool.result", TraceID: "trace-shared", Kind: canonical.ActivityDelegation, ToolName: "spawn_agent", TargetAgentType: "explorer", Agent: canonical.AgentContext{RunID: "parent-run", Model: "gpt-parent"}},
		{ObservedAt: now.Add(2 * time.Second), Name: "gen_ai.session.start", TraceID: "trace-shared", Agent: canonical.AgentContext{RunID: "child-run", Model: "gpt-child"}},
		{ObservedAt: now.Add(3 * time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child-run", Model: "gpt-child", Tokens: canonical.TokenUsage{Input: 11, Output: 4}}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 2 {
		t.Fatalf("unexpected sessions: %#v", overview.Sessions)
	}
	byID := make(map[string]query.Session, len(overview.Sessions))
	for _, session := range overview.Sessions {
		byID[session.ID] = session
	}
	if len(byID["parent-run"].Agents) != 1 || len(byID["child-run"].Agents) != 1 {
		t.Fatalf("conversation agents were merged: %#v", overview.Sessions)
	}
	if byID["child-run"].Tokens.Total() != 15 {
		t.Fatalf("unexpected child usage: %#v", byID["child-run"].Tokens)
	}
	if len(byID["parent-run"].TraceIDs) != 1 || len(byID["child-run"].TraceIDs) != 1 {
		t.Fatalf("shared trace correlation was lost: %#v", overview.Sessions)
	}
}

func TestCodexSpawnAggregatesChildConversationIntoParentSession(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{
			Source: "codex", ObservedAt: now, Name: "gen_ai.user_prompt", Kind: canonical.ActivityPrompt,
			Body: "delegate analysis", Agent: canonical.AgentContext{RunID: "parent-thread", Model: "gpt-5.6-sol"},
		},
		{
			Source: "codex", ObservedAt: now.Add(time.Second), Name: "gen_ai.agent.delegation", Kind: canonical.ActivityDelegation,
			Agent: canonical.AgentContext{AgentID: "parent-thread"}, TargetAgentID: "child-thread",
			Attributes: map[string]any{"state": "send", "kind": "spawn", "communication_id": "communication-1"},
		},
		{
			Source: "codex", ObservedAt: now.Add(2 * time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse,
			Body: "analysis complete", Agent: canonical.AgentContext{
				RunID: "child-thread", Model: "gpt-5.6-luna", Tokens: canonical.TokenUsage{Input: 11, Output: 4},
			},
		},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs, SessionLinks: []canonical.SessionLink{{Source: "codex", ParentSessionID: "parent-thread", ChildSessionID: "child-thread", ObservedAt: now.Add(time.Second)}}}); err != nil {
		t.Fatal(err)
	}

	page, err := database.ListSessions(context.Background(), query.SessionListFilter{
		Since: now.Add(-time.Hour), Page: mustPage(t, 0, 100),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 || page.Sessions[0].ID != "parent-thread" || page.Sessions[0].ActivityCount != 2 || page.Sessions[0].AgentCount != 2 {
		t.Fatalf("spawned conversation was not aggregated into its parent: %#v", page.Sessions)
	}
	searched, err := database.ListSessions(context.Background(), query.SessionListFilter{
		Since: now.Add(-time.Hour), Search: "analysis complete", Page: mustPage(t, 0, 100),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(searched.Sessions) != 1 || searched.Sessions[0].ID != "parent-thread" || searched.Sessions[0].ActivityCount != 2 {
		t.Fatalf("child search did not return the complete parent group: %#v", searched.Sessions)
	}
	dashboard, err := database.GetDashboard(context.Background(), query.DashboardFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.RunCount != 1 || dashboard.AgentCount != 2 || dashboard.Tokens.Total() != 15 {
		t.Fatalf("dashboard did not aggregate the spawned conversation: %#v", dashboard)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 1 || overview.Sessions[0].ID != "parent-thread" || overview.Sessions[0].AgentCount != 2 || len(overview.Sessions[0].Agents) != 2 {
		t.Fatalf("overview did not aggregate the spawned conversation: %#v", overview.Sessions)
	}

	for _, requestedID := range []string{"parent-thread", "child-thread"} {
		summary, err := database.GetSessionSummary(context.Background(), mustConversationIdentity(t, "codex", requestedID))
		if err != nil {
			t.Fatal(err)
		}
		if summary.ID != "parent-thread" || summary.ActivityCount != 2 || summary.AgentCount != 2 || summary.Tokens.Total() != 15 {
			t.Fatalf("unexpected aggregate for %q: %#v", requestedID, summary)
		}
		agents := make(map[string]query.AgentSession, len(summary.Agents))
		for _, agent := range summary.Agents {
			agents[agent.AgentID] = agent
		}
		if agents["parent-thread"].Model != "gpt-5.6-sol" || agents["child-thread"].Model != "gpt-5.6-luna" || agents["child-thread"].ParentAgentID != "parent-thread" {
			t.Fatalf("unexpected aggregate agent topology for %q: %#v", requestedID, summary.Agents)
		}
	}

	activityPage, err := database.ListSessionActivities(context.Background(), query.ActivityPageFilter{
		Identity: mustConversationIdentity(t, "codex", "parent-thread"), AgentID: "child-thread", Page: mustPage(t, 0, 100),
	})
	if err != nil {
		t.Fatal(err)
	}
	if activityPage.Total != 1 || len(activityPage.Activities) != 1 || activityPage.Activities[0].RunID != "child-thread" || activityPage.Activities[0].AgentID != "child-thread" {
		t.Fatalf("child agent activity filter returned %#v", activityPage)
	}
	childPage, err := database.ListSessions(context.Background(), query.SessionListFilter{
		Since: now.Add(-time.Hour), SourceID: "codex", SessionID: "child-thread", Page: mustPage(t, 0, 100),
	})
	if err != nil || len(childPage.Sessions) != 1 || childPage.Sessions[0].ID != "parent-thread" {
		t.Fatalf("child session lookup returned page=%#v err=%v", childPage, err)
	}
	childActivities, err := database.ListSessionActivities(context.Background(), query.ActivityPageFilter{
		Identity: mustConversationIdentity(t, "codex", "child-thread"), Page: mustPage(t, 0, 100),
	})
	if err != nil || childActivities.Total != 2 {
		t.Fatalf("child activity lookup returned page=%#v err=%v", childActivities, err)
	}
	conversation, err := database.GetConversation(context.Background(), query.ConversationFilter{
		Identity: mustConversationIdentity(t, "codex", "child-thread"), Page: mustPage(t, 0, 100), PageMode: query.ConversationPageFromOffset,
	})
	if err != nil || conversation.ID != "parent-thread" || conversation.ActivityCount != 2 {
		t.Fatalf("child conversation lookup returned session=%#v err=%v", conversation, err)
	}
	_, err = database.ListSessionActivities(context.Background(), query.ActivityPageFilter{
		Identity: mustConversationIdentity(t, "codex", "parent-thread"), AgentID: "missing", Page: mustPage(t, 0, 100),
	})
	if !errors.Is(err, query.ErrConversationNotFound) {
		t.Fatalf("missing agent error = %v", err)
	}
}

func TestSessionAggregationTraversesNestedSpawnsWithoutDoubleCounting(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	activity := func(runID string, offset time.Duration) canonical.Log {
		return canonical.Log{Source: "codex", ObservedAt: now.Add(offset), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: runID}}
	}
	edge := func(parent, child, communicationID string, offset time.Duration) canonical.Log {
		return canonical.Log{
			Source: "codex", ObservedAt: now.Add(offset), Name: "gen_ai.agent.delegation", Kind: canonical.ActivityDelegation,
			Agent: canonical.AgentContext{AgentID: parent}, TargetAgentID: child,
			Attributes: map[string]any{"state": "send", "kind": "spawn", "communication_id": communicationID},
		}
	}
	logs := []canonical.Log{
		activity("parent", 0), edge("parent", "child", "one", time.Second),
		edge("parent", "child", "one", 2*time.Second), activity("child", 3*time.Second),
		edge("child", "grandchild", "two", 4*time.Second), activity("grandchild", 5*time.Second),
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs, SessionLinks: []canonical.SessionLink{
		{Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now.Add(time.Second)},
		{Source: "codex", ParentSessionID: "child", ChildSessionID: "grandchild", ObservedAt: now.Add(4 * time.Second)},
	}}); err != nil {
		t.Fatal(err)
	}

	page, err := database.ListSessions(context.Background(), query.SessionListFilter{Since: now.Add(-time.Hour), Page: mustPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 || page.Sessions[0].ID != "parent" || page.Sessions[0].ActivityCount != 3 || page.Sessions[0].AgentCount != 3 {
		t.Fatalf("nested spawn group = %#v", page.Sessions)
	}
}

func TestSessionAggregationGroupsBeforePagination(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "codex", ObservedAt: now, Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "root-a"}},
		{Source: "codex", ObservedAt: now.Add(time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "root-b"}},
		{Source: "codex", ObservedAt: now.Add(2 * time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "parent"}},
		{Source: "codex", ObservedAt: now.Add(3 * time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs, SessionLinks: []canonical.SessionLink{{
		Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now.Add(2 * time.Second),
	}}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"parent", "root-b", "root-a"}
	for offset, wantID := range want {
		page, err := database.ListSessions(context.Background(), query.SessionListFilter{Since: now.Add(-time.Hour), Page: mustPage(t, offset, 1)})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Sessions) != 1 || page.Sessions[0].ID != wantID {
			t.Fatalf("page %d = %#v, want %q", offset, page.Sessions, wantID)
		}
		if page.HasMore != (offset < len(want)-1) {
			t.Fatalf("page %d HasMore = %v", offset, page.HasMore)
		}
	}
}

func TestSessionAggregationPagesAroundAChildActivityAnchor(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	traceID := "11111111111111111111111111111111"
	spanIDs := []string{"0000000000000001", "0000000000000002", "0000000000000003", "0000000000000004"}
	logs := []canonical.Log{
		{Source: "codex", ObservedAt: now, TraceID: traceID, SpanID: spanIDs[0], Name: "gen_ai.agent.message", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "parent"}},
		{Source: "codex", ObservedAt: now.Add(time.Second), TraceID: traceID, SpanID: spanIDs[1], Name: "gen_ai.agent.message", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "child"}},
		{Source: "codex", ObservedAt: now.Add(2 * time.Second), TraceID: traceID, SpanID: spanIDs[2], Name: "gen_ai.agent.message", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "parent"}},
		{Source: "codex", ObservedAt: now.Add(3 * time.Second), TraceID: traceID, SpanID: spanIDs[3], Name: "gen_ai.agent.message", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "child"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs, SessionLinks: []canonical.SessionLink{{
		Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now,
	}}}); err != nil {
		t.Fatal(err)
	}
	page, err := database.ListSessionActivities(context.Background(), query.ActivityPageFilter{
		Identity: mustConversationIdentity(t, "codex", "child"), Anchor: mustActivityAnchor(t, traceID, spanIDs[1]), Page: mustPage(t, 0, 2),
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, activity := range page.Activities {
		found = found || activity.SpanID == spanIDs[1]
	}
	if page.Total != 4 || page.Offset != 1 || !page.HasEarlier || !page.HasMore || !found {
		t.Fatalf("group anchor page = %#v", page)
	}
}

func TestSessionAggregationIgnoresNonSpawnSendEvents(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "codex", ObservedAt: now, Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "parent"}},
		{Source: "codex", ObservedAt: now.Add(time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child"}},
		{
			Source: "codex", ObservedAt: now.Add(2 * time.Second), Name: "gen_ai.agent.delegation", Kind: canonical.ActivityDelegation,
			Agent: canonical.AgentContext{AgentID: "parent"}, TargetAgentID: "child", Attributes: map[string]any{"state": "send", "kind": "followup"},
		},
		{
			Source: "codex", ObservedAt: now.Add(3 * time.Second), Name: "gen_ai.agent.delegation", Kind: canonical.ActivityDelegation,
			Agent: canonical.AgentContext{AgentID: "parent"}, TargetAgentID: "child", Attributes: map[string]any{"state": "receive", "kind": "spawn"},
		},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	page, err := database.ListSessions(context.Background(), query.SessionListFilter{Since: now.Add(-time.Hour), Page: mustPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 2 {
		t.Fatalf("non-spawn communication grouped sessions: %#v", page.Sessions)
	}
}

func TestSessionAggregationPersistsAmbiguousAndCyclicLinksSafely(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	ids := []string{"parent-1", "parent-2", "ambiguous", "cycle-1", "cycle-2"}
	logs := make([]canonical.Log, 0, len(ids))
	for index, id := range ids {
		logs = append(logs, canonical.Log{Source: "codex", ObservedAt: now.Add(time.Duration(index) * time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: id}})
	}
	links := []canonical.SessionLink{
		{Source: "codex", ParentSessionID: "parent-1", ChildSessionID: "ambiguous", ObservedAt: now},
		{Source: "codex", ParentSessionID: "parent-2", ChildSessionID: "ambiguous", ObservedAt: now},
		{Source: "codex", ParentSessionID: "cycle-1", ChildSessionID: "cycle-2", ObservedAt: now},
		{Source: "codex", ParentSessionID: "cycle-2", ChildSessionID: "cycle-1", ObservedAt: now},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs, SessionLinks: links}); err != nil {
		t.Fatal(err)
	}
	page, err := database.ListSessions(context.Background(), query.SessionListFilter{Since: now.Add(-time.Hour), Page: mustPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != len(ids) {
		t.Fatalf("unsafe links were grouped: %#v", page.Sessions)
	}
}

func TestSessionAggregationNamespacesExplicitAgentIDsByConversation(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "codex", ObservedAt: now.Add(-2 * time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "parent"}},
		{Source: "codex", ObservedAt: now.Add(-time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "parent", AgentID: "parent"}},
		{Source: "codex", ObservedAt: now, Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "parent", AgentID: "main"}},
		{Source: "codex", ObservedAt: now.Add(time.Second), Name: "gen_ai.agent.delegation", Kind: canonical.ActivityDelegation, Agent: canonical.AgentContext{AgentID: "parent"}, TargetAgentID: "child", Attributes: map[string]any{"state": "send", "kind": "spawn"}},
		{Source: "codex", ObservedAt: now.Add(2 * time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child", AgentID: "main"}},
		{Source: "codex", ObservedAt: now.Add(3 * time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs, SessionLinks: []canonical.SessionLink{{Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now.Add(time.Second)}}}); err != nil {
		t.Fatal(err)
	}
	summary, err := database.GetSessionSummary(context.Background(), mustConversationIdentity(t, "codex", "parent"))
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Agents) != 2 || summary.Agents[0].AgentID != "child" || summary.Agents[0].ParentAgentID != "parent" || summary.Agents[1].AgentID != "parent" {
		t.Fatalf("explicit primary agent IDs were not namespaced by conversation: %#v", summary.Agents)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 1 || len(overview.Sessions[0].Agents) != 2 {
		t.Fatalf("overview collapsed explicit primary agents: %#v", overview.Sessions)
	}
	page, err := database.ListSessions(context.Background(), query.SessionListFilter{Since: now.Add(-time.Hour), Page: mustPage(t, 0, 100)})
	if err != nil || len(page.Sessions) != 1 || page.Sessions[0].AgentCount != 2 {
		t.Fatalf("rollup primary agent identities diverged: page=%#v err=%v", page, err)
	}
}

func TestSessionAggregationAppliesTimeRangeAfterGrouping(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	old := now.Add(-2 * time.Hour)
	logs := []canonical.Log{
		{Source: "codex", ObservedAt: old, Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "parent", Tokens: canonical.TokenUsage{Input: 5, Output: 1}}},
		{Source: "codex", ObservedAt: now.Add(-time.Minute), Name: "gen_ai.agent.delegation", Kind: canonical.ActivityDelegation, Agent: canonical.AgentContext{AgentID: "parent"}, TargetAgentID: "child", Attributes: map[string]any{"state": "send", "kind": "spawn"}},
		{Source: "codex", ObservedAt: now, Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child", Tokens: canonical.TokenUsage{Input: 7, Output: 2}}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs, SessionLinks: []canonical.SessionLink{{Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now.Add(-time.Minute)}}}); err != nil {
		t.Fatal(err)
	}
	page, err := database.ListSessions(context.Background(), query.SessionListFilter{Since: now.Add(-time.Hour), Page: mustPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 1 || page.Sessions[0].ActivityCount != 2 || page.Sessions[0].AgentCount != 2 || !page.Sessions[0].StartedAt.Equal(old) {
		t.Fatalf("time range was applied before grouping: %#v", page.Sessions)
	}
	dashboard, err := database.GetDashboard(context.Background(), query.DashboardFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.RunCount != 1 || dashboard.AgentCount != 2 || dashboard.Tokens.Total() != 15 {
		t.Fatalf("dashboard returned a partial group: %#v", dashboard)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil || len(overview.Sessions) != 1 || overview.Sessions[0].Tokens.Total() != 15 || overview.Sessions[0].ActivityCount != 2 {
		t.Fatalf("overview returned a partial group: overview=%#v err=%v", overview, err)
	}
}

func TestDashboardSearchAggregatesTheCompleteMatchingGroup(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "codex", ObservedAt: now, Name: "gen_ai.response.completed", Body: "ordinary parent", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "parent", Tokens: canonical.TokenUsage{Input: 5, Output: 1}}},
		{Source: "codex", ObservedAt: now.Add(time.Second), Name: "gen_ai.agent.delegation", Kind: canonical.ActivityDelegation, Agent: canonical.AgentContext{AgentID: "parent"}, TargetAgentID: "child", Attributes: map[string]any{"state": "send", "kind": "spawn"}},
		{Source: "codex", ObservedAt: now.Add(2 * time.Second), Name: "gen_ai.response.completed", Body: "unique child needle", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child", Tokens: canonical.TokenUsage{Input: 7, Output: 2}}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs, SessionLinks: []canonical.SessionLink{{Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now.Add(time.Second)}}}); err != nil {
		t.Fatal(err)
	}
	dashboard, err := database.GetDashboard(context.Background(), query.DashboardFilter{Since: now.Add(-time.Hour), Search: "unique child needle"})
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.RunCount != 1 || dashboard.AgentCount != 2 || dashboard.Tokens.Total() != 15 {
		t.Fatalf("dashboard search returned only matching activities: %#v", dashboard)
	}
}

func TestSessionSearchMatchesOlderActivityInAnActiveGroup(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	old := now.Add(-2 * time.Hour)
	logs := []canonical.Log{
		{Source: "codex", ObservedAt: old, Name: "gen_ai.response.completed", Body: "older unique needle", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "parent", Tokens: canonical.TokenUsage{Input: 5}}},
		{Source: "codex", ObservedAt: now, Name: "gen_ai.response.completed", Body: "recent child", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child", Tokens: canonical.TokenUsage{Output: 7}}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs, SessionLinks: []canonical.SessionLink{{
		Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now,
	}}}); err != nil {
		t.Fatal(err)
	}
	filter := query.SessionListFilter{Since: now.Add(-time.Hour), Search: "older unique needle", Page: mustPage(t, 0, 100)}
	page, err := database.ListSessions(context.Background(), filter)
	if err != nil || len(page.Sessions) != 1 || page.Sessions[0].ID != "parent" || page.Sessions[0].ActivityCount != 2 {
		t.Fatalf("group search returned page=%#v err=%v", page, err)
	}
	dashboard, err := database.GetDashboard(context.Background(), query.DashboardFilter{Since: filter.Since, Search: filter.Search})
	if err != nil || dashboard.RunCount != 1 || dashboard.AgentCount != 2 || dashboard.Tokens.Total() != 12 {
		t.Fatalf("group dashboard search returned dashboard=%#v err=%v", dashboard, err)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: filter.Since, Search: filter.Search})
	if err != nil || len(overview.Sessions) != 1 || overview.Sessions[0].ID != "parent" || overview.Sessions[0].Tokens.Total() != 12 {
		t.Fatalf("group overview search returned overview=%#v err=%v", overview, err)
	}
}

func TestSessionAggregationDoesNotInterpretOtherSourcesAsCodexThreads(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "claude", ObservedAt: now, Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "parent"}},
		{Source: "claude", ObservedAt: now.Add(time.Second), Name: "gen_ai.agent.delegation", Kind: canonical.ActivityDelegation, Agent: canonical.AgentContext{AgentID: "parent"}, TargetAgentID: "child", Attributes: map[string]any{"state": "send", "kind": "spawn"}},
		{Source: "claude", ObservedAt: now.Add(2 * time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	page, err := database.ListSessions(context.Background(), query.SessionListFilter{Since: now.Add(-time.Hour), SourceID: "claude", Page: mustPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != 2 {
		t.Fatalf("non-Codex sessions were grouped: %#v", page.Sessions)
	}
}

func TestOverviewNamespacesConversationIdentityBySource(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "claude", ObservedAt: now, Name: "gen_ai.user.message", Kind: canonical.ActivityPrompt, Agent: canonical.AgentContext{RunID: "conversation-1"}},
		{Source: "codex", ObservedAt: now.Add(time.Second), Name: "gen_ai.user.message", Kind: canonical.ActivityPrompt, Agent: canonical.AgentContext{RunID: "conversation-1"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 2 {
		t.Fatalf("same native ID from two sources was merged: %#v", overview.Sessions)
	}
	sources := map[string]bool{}
	for _, session := range overview.Sessions {
		if session.ID != "conversation-1" {
			t.Fatalf("native conversation ID changed: %#v", session)
		}
		sources[session.SourceID] = true
	}
	if !sources["claude"] || !sources["codex"] {
		t.Fatalf("source-qualified identities missing: %#v", overview.Sessions)
	}
}

func TestGetTraceCorrelatesConversationsAndReportsIncompleteParents(t *testing.T) {
	const traceID = "11111111111111111111111111111111"
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	spans := []canonical.Span{
		{
			Source: "claude", TraceID: traceID, SpanID: "root", Name: "root operation",
			StartedAt: now, EndedAt: now.Add(4 * time.Second), Status: "Ok", Kind: canonical.ActivityReasoning,
			Agent: canonical.AgentContext{RunID: "conversation-a", AgentID: "main", Model: "model-a"},
		},
		{
			Source: "codex", TraceID: traceID, SpanID: "child", ParentSpanID: "missing", Name: "cross-source operation",
			StartedAt: now.Add(time.Second), EndedAt: now.Add(2 * time.Second), Status: "Error", Kind: canonical.ActivityTool,
			Agent: canonical.AgentContext{RunID: "conversation-b", AgentID: "reviewer", AgentDefinition: "repository-review", AgentType: "custom", Model: "model-b"},
		},
	}
	logs := []canonical.Log{{
		Source: "codex", ObservedAt: now.Add(1500 * time.Millisecond), TraceID: traceID, SpanID: "child",
		Name: "gen_ai.tool.message", Body: "correlated log", Kind: canonical.ActivityMessage,
		Agent: canonical.AgentContext{RunID: "conversation-b", AgentID: "reviewer", Tokens: canonical.TokenUsage{Input: 12}},
	}}
	ctx := context.Background()
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalTrace, Spans: spans}); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	trace, err := database.GetTrace(ctx, query.TraceFilter{TraceID: mustTraceID(t, traceID)})
	if err != nil {
		t.Fatal(err)
	}
	if trace.TraceID != traceID || trace.RootSpanCount != 1 || trace.MissingParentCount != 1 || trace.Status != query.TraceStatusError {
		t.Fatalf("unexpected trace summary: %#v", trace)
	}
	if trace.ActivityCount != 3 || len(trace.Conversations) != 2 || len(trace.Agents) != 2 || len(trace.Activities) != 3 {
		t.Fatalf("unexpected trace participants: %#v", trace)
	}
	if trace.Activities[0].SpanID != "root" || trace.Activities[1].Signal != canonical.SignalTrace || trace.Activities[2].Signal != canonical.SignalLog {
		t.Fatalf("trace activities not chronologically ordered: %#v", trace.Activities)
	}
	if trace.Agents[1].AgentDefinition != "repository-review" || trace.Agents[1].AgentType != "custom" || trace.Agents[1].Model != "model-b" {
		t.Fatalf("sparse event erased richer agent evidence: %#v", trace.Agents[1])
	}
	if !trace.Activities[2].ContributesToTotal {
		t.Fatalf("authoritative usage was not marked as a contribution: %#v", trace.Activities[2])
	}

	page, err := database.GetTrace(ctx, query.TraceFilter{TraceID: mustTraceID(t, traceID), Page: mustPage(t, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if page.ActivityOffset != 1 || page.ActivityCount != 3 || len(page.Activities) != 1 || !page.HasMore || page.Activities[0].SpanID != "child" {
		t.Fatalf("unexpected trace page: %#v", page)
	}
}

func TestGetTraceReturnsNotFoundForUnknownIdentity(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.GetTrace(context.Background(), query.TraceFilter{TraceID: mustTraceID(t, "22222222222222222222222222222222")})
	if !errors.Is(err, query.ErrTraceNotFound) {
		t.Fatalf("error = %v, want ErrTraceNotFound", err)
	}
}

func TestOverviewTotalsAreIndependentOfActivityDisplayLimit(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := make([]canonical.Log, 1105)
	for index := range logs {
		logs[index] = canonical.Log{
			ObservedAt: now.Add(time.Duration(index) * time.Millisecond),
			Name:       "gen_ai.response.completed",
			Body:       fmt.Sprintf("activity-%d", index),
			Kind:       canonical.ActivityResponse,
			Attributes: map[string]any{
				"gen_ai.usage.role": "authoritative_call",
				"gen_ai.usage.id":   "request-" + fmt.Sprint(index),
			},
			Agent: canonical.AgentContext{
				RunID:  "run-1",
				Tokens: canonical.TokenUsage{Input: 1, Output: 1},
			},
		}
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Tokens.Total() != 2210 || overview.Sessions[0].ActivityCount != 1105 {
		t.Fatalf("overview totals were truncated: %#v", overview)
	}
	if len(overview.Sessions[0].Activities) > 100 {
		t.Fatalf("activity display is unbounded: %d", len(overview.Sessions[0].Activities))
	}
	next, err := database.GetOverview(context.Background(), query.OverviewFilter{
		Since:          now.Add(-time.Hour),
		ActivityOffset: 100,
		ActivityLimit:  100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Sessions[0].Activities) != 100 {
		t.Fatalf("next activity page length = %d, want 100", len(next.Sessions[0].Activities))
	}
	if overview.Sessions[0].Activities[99].Content == next.Sessions[0].Activities[0].Content {
		t.Fatalf("activity pages overlap at %q", next.Sessions[0].Activities[0].Content)
	}
	if next.Tokens.Total() != overview.Tokens.Total() {
		t.Fatalf("pagination changed aggregate tokens: first=%d next=%d", overview.Tokens.Total(), next.Tokens.Total())
	}
}

func TestListSessionActivitiesFiltersByAgent(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "example", ObservedAt: now, Name: "main activity", Body: "main", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "run-1", AgentID: "main"}},
		{Source: "example", ObservedAt: now.Add(time.Second), Name: "review activity", Body: "review", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "run-1", AgentID: "reviewer"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	page, err := database.ListSessionActivities(context.Background(), query.ActivityPageFilter{Identity: mustConversationIdentity(t, "example", "run-1"), AgentID: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Activities) != 1 || page.Activities[0].AgentID != "reviewer" {
		t.Fatalf("agent-filtered activities = %#v, want one reviewer activity", page)
	}
}

func TestListSessionActivitiesKeepsAnchorInsideSmallPage(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	const traceID = "11111111111111111111111111111111"
	const targetIndex = 30
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := make([]canonical.Log, 0, 60)
	for index := range 60 {
		logs = append(logs, canonical.Log{
			Source: "example", ObservedAt: now.Add(time.Duration(index) * time.Second),
			TraceID: traceID, SpanID: fmt.Sprintf("%016x", index+1),
			Name: "activity", Kind: canonical.ActivityMessage,
			Agent: canonical.AgentContext{RunID: "run-1", AgentID: "main"},
		})
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}

	targetSpanID := fmt.Sprintf("%016x", targetIndex+1)
	page, err := database.ListSessionActivities(context.Background(), query.ActivityPageFilter{
		Identity: mustConversationIdentity(t, "example", "run-1"),
		Anchor:   mustActivityAnchor(t, traceID, targetSpanID),
		Page:     mustPage(t, 0, 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Activities) != 10 {
		t.Fatalf("activity count = %d, want 10", len(page.Activities))
	}
	for _, activity := range page.Activities {
		if activity.TraceID == traceID && activity.SpanID == targetSpanID {
			return
		}
	}
	t.Fatal("anchor fell outside the bounded activity page")
}

func TestSessionActivityPagesHaveStableTotalOrderForEqualTimestamps(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC()
	logs := make([]canonical.Log, 205)
	for index := range logs {
		logs[index] = canonical.Log{
			Source: "example", ObservedAt: now, Name: fmt.Sprintf("equal-%03d", index),
			Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "equal-time"},
		}
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Logs: logs}); err != nil {
		t.Fatal(err)
	}
	identity := mustConversationIdentity(t, "example", "equal-time")
	readIDs := func() []string {
		result := make([]string, 0, len(logs))
		for offset := 0; offset < len(logs); offset += 100 {
			page, err := database.ListSessionActivities(context.Background(), query.ActivityPageFilter{
				Identity: identity,
				Page:     mustPage(t, offset, min(100, len(logs)-offset)),
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, activity := range page.Activities {
				result = append(result, activity.ID)
			}
		}
		return result
	}
	first, second := readIDs(), readIDs()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("equal-time order changed: first=%v second=%v", first, second)
	}
	unique := make(map[string]struct{}, len(first))
	for _, id := range first {
		unique[id] = struct{}{}
	}
	if len(first) != len(logs) || len(unique) != len(logs) {
		t.Fatalf("equal-time page identities: total=%d unique=%d", len(first), len(unique))
	}
}

func TestOverviewCountsOneAuthoritativeUsageContribution(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	usage := canonical.TokenUsage{Input: 100, Output: 20, CacheRead: 70, Reasoning: 5}
	span := canonical.Span{
		Source: "example", TraceID: "trace-1", SpanID: "span-1", Name: "gen_ai.model.request.trace",
		StartedAt: now.Add(-time.Second), EndedAt: now, Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "corroborating", "gen_ai.usage.id": "request-1"},
		Agent:      canonical.AgentContext{RunID: "run-1", AgentID: "agent-1", Tokens: usage},
	}
	log := canonical.Log{
		Source: "example", ObservedAt: now, Name: "gen_ai.model.request", Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": "request-1"},
		Agent:      canonical.AgentContext{RunID: "run-1", AgentID: "agent-1", Tokens: usage},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{log, log}}); err != nil {
		t.Fatal(err)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Tokens != usage {
		t.Fatalf("usage was double counted: got %#v want %#v", overview.Tokens, usage)
	}
}

func TestOverviewDeduplicatesCompatibleCrossSignalUsageWithoutAStableID(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	traceUsage := canonical.TokenUsage{
		Input: 100, Output: 20, CacheRead: 70,
		Presence: canonical.TokenPresence{CacheWrite: true},
	}
	logUsage := canonical.TokenUsage{Input: 100, Output: 20, CacheRead: 70}
	span := canonical.Span{
		Source: "example", TraceID: "trace-1", SpanID: "span-1", Name: "gen_ai.model.request.trace",
		StartedAt: now.Add(-time.Second), EndedAt: now, Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "corroborating"},
		Agent:      canonical.AgentContext{RunID: "run-1", Tokens: traceUsage},
	}
	log := canonical.Log{
		Source: "example", ObservedAt: now.Add(500 * time.Millisecond), Name: "gen_ai.model.request", Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "authoritative_call"},
		Agent:      canonical.AgentContext{RunID: "run-1", Tokens: logUsage},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{log}}); err != nil {
		t.Fatal(err)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Tokens.Input != 100 || overview.Tokens.Output != 20 {
		t.Fatalf("compatible cross-signal usage was double counted: %#v", overview.Tokens)
	}
	if len(overview.Sessions[0].Activities) != 2 || overview.Sessions[0].Activities[1].Tokens.Input != 100 {
		t.Fatalf("corroborating evidence was erased: %#v", overview.Sessions[0].Activities)
	}
}

func TestOverviewCombinesComplementaryAgentMetadataForTheSameModelCall(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"), sourceplugin.NewRegistry(claude.New()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	span := canonical.Span{
		Source: "claude", TraceID: "trace-1", SpanID: "span-1", Name: "gen_ai.model.request.trace",
		StartedAt: now.Add(-time.Second), EndedAt: now, Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "corroborating", "gen_ai.usage.id": "request-1"},
		Agent: canonical.AgentContext{
			AgentID: "repo-overview@session-1", RunID: "session-1", Model: "claude-opus-5[1m]",
		},
	}
	rootLog := canonical.Log{
		Source: "claude", ObservedAt: now.Add(-2 * time.Second), Name: "gen_ai.user.message", Kind: canonical.ActivityPrompt,
		Agent: canonical.AgentContext{RunID: "session-1", Model: "claude-opus-5"},
	}
	log := canonical.Log{
		Source: "claude", ObservedAt: now, Name: "gen_ai.model.request", Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": "request-1"},
		Agent: canonical.AgentContext{
			AgentType: "agent:custom", RunID: "session-1", Model: "claude-opus-5",
			Tokens: canonical.TokenUsage{Input: 100, Output: 20},
		},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{rootLog, log}}); err != nil {
		t.Fatal(err)
	}

	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 1 || len(overview.Sessions[0].Agents) != 2 {
		t.Fatalf("unexpected topology: %#v", overview.Sessions)
	}
	var agent, root *query.AgentSession
	for index := range overview.Sessions[0].Agents {
		switch overview.Sessions[0].Agents[index].AgentID {
		case "repo-overview@session-1":
			agent = &overview.Sessions[0].Agents[index]
		case "main":
			root = &overview.Sessions[0].Agents[index]
		}
	}
	if agent == nil || root == nil {
		t.Fatalf("root and subagent topology was not retained: %#v", overview.Sessions[0].Agents)
	}
	if agent.AgentID != "repo-overview@session-1" || agent.AgentDefinition != "repo-overview" || agent.AgentType != "custom" || agent.Model != "claude-opus-5" {
		t.Fatalf("complementary agent metadata was not combined: %#v", agent)
	}
	if agent.Tokens.Total() != 120 || root.Tokens.AnyReported() || overview.Sessions[0].Tokens.Total() != 120 {
		t.Fatalf("model-call usage was not attributed to the subagent exactly once: agent=%#v session=%#v", agent.Tokens, overview.Sessions[0].Tokens)
	}
	for _, activity := range overview.Sessions[0].Activities {
		if activity.UsageID == "request-1" && (activity.AgentID != agent.AgentID || activity.AgentDefinition != agent.AgentDefinition || activity.AgentType != agent.AgentType) {
			t.Fatalf("activity was not enriched from correlated evidence: %#v", activity)
		}
	}
}

func TestSessionSummaryInfersAgentParentFromSpanParentage(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	spans := []canonical.Span{
		{Source: "example", TraceID: "trace-topology", SpanID: "root", Name: "delegation", StartedAt: now, EndedAt: now.Add(time.Second), Kind: canonical.ActivityDelegation, Agent: canonical.AgentContext{RunID: "run-topology"}},
		{Source: "example", TraceID: "trace-topology", SpanID: "child", ParentSpanID: "root", Name: "child work", StartedAt: now.Add(time.Second), EndedAt: now.Add(2 * time.Second), Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "run-topology", AgentID: "reviewer"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: spans}); err != nil {
		t.Fatal(err)
	}
	session, err := database.GetSessionSummary(context.Background(), mustConversationIdentity(t, "example", "run-topology"))
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range session.Agents {
		if agent.AgentID == "reviewer" {
			if agent.ParentAgentID != "main" {
				t.Fatalf("inferred parent = %q, want main", agent.ParentAgentID)
			}
			return
		}
	}
	t.Fatalf("reviewer agent missing from session summary: %#v", session.Agents)
}

func TestOverviewFiltersSessionsBySourceAndSearchesUnpagedActivity(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"), sourceplugin.NewRegistry(claude.New()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := make([]canonical.Log, 0, 106)
	for index := 0; index < 105; index++ {
		content := fmt.Sprintf("ordinary activity %d", index)
		if index == 0 {
			content = "Buried repository review needle"
		}
		logs = append(logs, canonical.Log{
			Source: "claude", ObservedAt: now.Add(time.Duration(index) * time.Second),
			Name: "gen_ai.agent.message", Body: content, Kind: canonical.ActivityMessage,
			Agent: canonical.AgentContext{RunID: "claude-session", Model: "claude-model"},
		})
	}
	logs = append(logs, canonical.Log{
		Source: "codex", ObservedAt: now, Name: "gen_ai.agent.message", Body: "unrelated",
		Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "codex-session", Model: "codex-model"},
	})
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}

	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{
		Since: now.Add(-time.Hour), SourceID: "claude", Search: "REPOSITORY REVIEW",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 1 || overview.Sessions[0].ID != "claude-session" {
		t.Fatalf("unexpected filtered sessions: %#v", overview.Sessions)
	}
	if overview.Sessions[0].ActivityCount != 105 || len(overview.Sessions[0].Activities) != 100 {
		t.Fatalf("search did not inspect unpaged activity while retaining the full session: %#v", overview.Sessions[0])
	}
	if len(overview.Sessions[0].Sources) != 1 || overview.Sessions[0].Sources[0].Label != "Claude Code" {
		t.Fatalf("session source was not described by its plugin: %#v", overview.Sessions[0].Sources)
	}
	if len(overview.Sources) != 2 {
		t.Fatalf("filter options should retain all observed sources: %#v", overview.Sources)
	}
}

func TestConversationQueryIgnoresDashboardRangeAndDisplayPage(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	old := time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Millisecond)
	logs := make([]canonical.Log, 0, 205)
	for index := 0; index < 205; index++ {
		logs = append(logs, canonical.Log{
			Source: "example", ObservedAt: old.Add(time.Duration(index) * time.Second),
			Name: "gen_ai.agent.message", Body: fmt.Sprintf("activity %d", index),
			Kind:  canonical.ActivityMessage,
			Agent: canonical.AgentContext{RunID: "old-conversation", AgentID: "main"},
		})
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	const traceID = "11111111111111111111111111111111"
	const spanID = "aaaaaaaaaaaaaaaa"
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{{
		Source: "example", TraceID: traceID, SpanID: spanID, Name: "target activity",
		StartedAt: old.Add(100*time.Second + 500*time.Millisecond), EndedAt: old.Add(100*time.Second + 750*time.Millisecond),
		Kind:  canonical.ActivityUnknown,
		Agent: canonical.AgentContext{RunID: "old-conversation", AgentID: "main"},
	}}}); err != nil {
		t.Fatal(err)
	}

	identity := mustConversationIdentity(t, "example", "old-conversation")
	anchor := mustActivityAnchor(t, traceID, spanID)
	conversation, err := database.GetConversation(context.Background(), query.ConversationFilter{Identity: identity, Anchor: anchor})
	if err != nil {
		t.Fatal(err)
	}
	if conversation.ActivityCount != 206 || len(conversation.Activities) != 100 {
		t.Fatalf("exact conversation was range-limited or paged: count=%d activities=%d", conversation.ActivityCount, len(conversation.Activities))
	}
	if conversation.ActivityOffset != 79 || !conversation.HasEarlier || !conversation.HasMore {
		t.Fatalf("unexpected target window metadata: offset=%d earlier=%v more=%v", conversation.ActivityOffset, conversation.HasEarlier, conversation.HasMore)
	}
	targetFound := false
	for _, activity := range conversation.Activities {
		if activity.TraceID == traceID && activity.SpanID == spanID {
			targetFound = true
			break
		}
	}
	if !targetFound {
		t.Fatal("requested unknown-kind target was not retained in the bounded exact projection")
	}
	continuation, err := database.GetConversation(context.Background(), query.ConversationFilter{
		Identity: identity, Anchor: anchor,
		Page:     mustPage(t, conversation.ActivityOffset+len(conversation.Activities), 100),
		PageMode: query.ConversationPageFromOffset,
	})
	if err != nil {
		t.Fatal(err)
	}
	if continuation.ActivityCount != conversation.ActivityCount || continuation.ActivityOffset != 179 || len(continuation.Activities) != 27 {
		t.Fatalf("continuation changed the target projection: count=%d offset=%d activities=%d", continuation.ActivityCount, continuation.ActivityOffset, len(continuation.Activities))
	}
	if continuation.Activities[0].Content != "activity 26" || continuation.Activities[len(continuation.Activities)-1].Content != "activity 0" {
		t.Fatalf("continuation skipped or duplicated the target boundary: first=%q last=%q", continuation.Activities[0].Content, continuation.Activities[len(continuation.Activities)-1].Content)
	}
	_, err = database.GetConversation(context.Background(), query.ConversationFilter{
		Identity: identity, Anchor: mustActivityAnchor(t, traceID, "bbbbbbbbbbbbbbbb"),
	})
	if !errors.Is(err, query.ErrConversationTargetNotFound) {
		t.Fatalf("wrong target error = %v, want ErrConversationTargetNotFound", err)
	}
}

func TestOpenEnablesWAL(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	mode, err := database.JournalMode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal mode = %q, want wal", mode)
	}
}
