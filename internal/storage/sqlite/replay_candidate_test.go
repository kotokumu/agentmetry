package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/billing"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/source/builtin"
)

func TestReplayCandidateDeferredProjectionMatchesSequentialCommit(t *testing.T) {
	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	exports := replayEquivalenceExports(at)
	originalRandRead := randRead
	t.Cleanup(func() { randRead = originalRandRead })

	randRead = deterministicRandRead()
	rates := billing.BuiltinOpenAIRates()
	reference, err := open(filepath.Join(t.TempDir(), "reference.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if err := reference.ReplaceRateHistoryForReplay(context.Background(), rates); err != nil {
		t.Fatal(err)
	}
	bounds := [][2]int{{0, 2}, {2, len(exports)}}
	referenceAgentPrefixes := make([]string, 0, len(bounds))
	for _, bound := range bounds {
		for _, exported := range exports[bound[0]:bound[1]] {
			if err := reference.CommitExport(context.Background(), exported); err != nil {
				t.Fatal(err)
			}
		}
		referenceAgentPrefixes = append(referenceAgentPrefixes, logicalDatabaseSnapshotForTables(t, reference.db, map[string]struct{}{"session_agents": {}, "trace_agents": {}}))
	}
	referenceSnapshot := logicalDatabaseSnapshot(t, reference.db)
	referencePublic := publicReplaySnapshot(t, reference, at)

	randRead = deterministicRandRead()
	candidate, err := OpenReplayCandidate(context.Background(), filepath.Join(t.TempDir(), "candidate.db"), ReplayCandidateConfig{
		Profiles: builtin.Registry(), RateHistory: rates,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = candidate.Close() })
	for index, bound := range bounds {
		if err := candidate.CommitReplayBatch(context.Background(), exports[bound[0]:bound[1]]); err != nil {
			t.Fatal(err)
		}
		if got := logicalDatabaseSnapshotForTables(t, candidate.store.db, map[string]struct{}{"session_agents": {}, "trace_agents": {}}); got != referenceAgentPrefixes[index] {
			t.Fatalf("replay agent prefix %d differs from sequential commit\n%s", index+1, firstSnapshotDifference(referenceAgentPrefixes[index], got))
		}
	}
	for _, table := range []string{"session_memberships", "session_rollups", "session_traces", "trace_rollups", "trace_conversations"} {
		var count int
		if err := candidate.store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("deferred table %s contains %d rows before finalization", table, count)
		}
	}
	var pendingCosts int
	if err := candidate.store.db.QueryRow(`SELECT COUNT(*) FROM model_call_attributions WHERE primary_reason = 'pending_replay_finalization'`).Scan(&pendingCosts); err != nil {
		t.Fatal(err)
	}
	if pendingCosts == 0 {
		t.Fatal("Codex cost facts were not deferred")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := candidate.FinalizeReplay(cancelled); err == nil {
		t.Fatal("cancelled replay finalization succeeded")
	}
	if err := candidate.FinalizeReplay(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := logicalDatabaseSnapshot(t, candidate.store.db); got != referenceSnapshot {
		t.Fatalf("finalized candidate differs from sequential commit\n%s", firstSnapshotDifference(referenceSnapshot, got))
	}
	if got := publicReplaySnapshot(t, candidate.store, at); got != referencePublic {
		t.Fatalf("finalized public queries differ from sequential commit\nwant: %s\n got: %s", referencePublic, got)
	}
	if err := candidate.store.db.QueryRow(`SELECT COUNT(*) FROM model_call_attributions WHERE primary_reason = 'pending_replay_finalization'`).Scan(&pendingCosts); err != nil {
		t.Fatal(err)
	}
	if pendingCosts != 0 {
		t.Fatalf("finalized candidate retains %d pending costs", pendingCosts)
	}
	for _, table := range []string{"spans", "logs", "metrics", "model_calls"} {
		var nonzero int
		if err := candidate.store.db.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE projection_sequence <> 0").Scan(&nonzero); err != nil {
			t.Fatal(err)
		}
		if nonzero != 0 {
			t.Fatalf("finalized table %s retains %d temporary projection sequences", table, nonzero)
		}
	}
	if err := candidate.FinalizeReplay(context.Background()); err == nil {
		t.Fatal("second replay finalization succeeded")
	}
	if err := candidate.CommitReplayBatch(context.Background(), exports[:1]); err == nil {
		t.Fatal("replay append after finalization succeeded")
	}
	if err := reference.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReplayCandidateRollsBackWholeBatchAtFirstInvalidExport(t *testing.T) {
	candidate, err := OpenReplayCandidate(context.Background(), filepath.Join(t.TempDir(), "candidate.db"), ReplayCandidateConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = candidate.Close() })
	before := logicalDatabaseSnapshot(t, candidate.store.db)
	exports := replayEquivalenceExports(time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC))[:2]
	exports[1].Journal.Harness.State = "bogus"

	if err := candidate.CommitReplayBatch(context.Background(), exports); err == nil || !strings.Contains(err.Error(), "export 2") {
		t.Fatalf("CommitReplayBatch() error = %v, want export 2 failure", err)
	}
	if after := logicalDatabaseSnapshot(t, candidate.store.db); after != before {
		t.Fatalf("failed replay batch changed candidate\n%s", firstSnapshotDifference(before, after))
	}
	if err := candidate.CommitReplayBatch(context.Background(), nil); err == nil {
		t.Fatal("empty replay batch succeeded")
	}
}

func TestFinalizedReplayStartsLiveProjectionFeedWithoutSequenceCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.db")
	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	candidate, err := OpenReplayCandidate(context.Background(), path, ReplayCandidateConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := candidate.CommitReplayBatch(context.Background(), []ingest.AcceptedExport{
		replayAgentMetadataExport(at, "00000000000000000000000000000021", "0000000000000021", "", "", "", ""),
	}); err != nil {
		t.Fatal(err)
	}
	if err := candidate.FinalizeReplay(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}

	live, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = live.Close() })
	position, err := live.CurrentProjectionPosition(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if position.Sequence != 0 {
		t.Fatalf("initial live projection sequence = %d, want 0", position.Sequence)
	}
	if err := live.CommitBatch(context.Background(), replayAgentMetadataExport(at.Add(time.Second), "00000000000000000000000000000022", "0000000000000022", "", "", "", "").Projection); err != nil {
		t.Fatal(err)
	}
	position, err = live.CurrentProjectionPosition(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if position.Sequence != 1 {
		t.Fatalf("first live projection sequence = %d, want 1", position.Sequence)
	}
	var activities int
	if err := live.db.QueryRow(`SELECT activity_count FROM session_rollups WHERE source = 'codex' AND run_id = 'metadata-session'`).Scan(&activities); err != nil {
		t.Fatal(err)
	}
	if activities != 2 {
		t.Fatalf("session activity count after first live commit = %d, want 2", activities)
	}
}

func BenchmarkReplayCommitStrategy(b *testing.B) {
	exports := make([]ingest.AcceptedExport, 1_024)
	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	for index := range exports {
		exports[index] = ingest.AcceptedExport{
			Envelope: ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, at, []byte{0x0a, 0x00}),
			Journal:  ingest.JournalMetadata{Source: "codex", NormalizerVersion: 1, NormalizationStatus: "projected"},
			Projection: canonical.Batch{Spans: []canonical.Span{{
				Source: "codex", TraceID: fmt.Sprintf("%032x", index), SpanID: fmt.Sprintf("%016x", index),
				Kind: canonical.ActivityResponse, StartedAt: at, EndedAt: at,
				Agent: canonical.AgentContext{RunID: "benchmark-session", AgentID: "main"},
			}}},
		}
	}
	benchmarkReplayStrategies(b, exports)
}

func BenchmarkReplayCostProjectionStrategy(b *testing.B) {
	const calls = 128
	at := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	usage := canonical.TokenUsage{Input: 100, Output: 20, CacheRead: 10, CacheWrite: 5}
	exports := make([]ingest.AcceptedExport, 0, calls*2)
	for index := range calls {
		usageID := fmt.Sprintf("usage-%04d", index)
		exports = append(exports,
			costExport(at.Add(time.Duration(index*2)*time.Second), "codex", "codex.sse_event", "gen_ai.response.completed", "cost-benchmark-session", "gpt-6-astra", usage, map[string]any{
				"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": usageID,
			}),
			benchmarkCodexCorroboratingExport(at.Add(time.Duration(index*2+1)*time.Second), index, usageID),
		)
	}
	benchmarkReplayStrategies(b, exports)
}

func benchmarkReplayStrategies(b *testing.B, exports []ingest.AcceptedExport) {
	b.Helper()
	b.Run("sequential", func(b *testing.B) {
		for iteration := 0; iteration < b.N; iteration++ {
			path := filepath.Join(b.TempDir(), fmt.Sprintf("sequential-%d.db", iteration))
			database, err := open(path, false)
			if err != nil {
				b.Fatal(err)
			}
			for _, exported := range exports {
				if err := database.CommitExport(context.Background(), exported); err != nil {
					b.Fatal(err)
				}
			}
			if err := database.Close(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("batched-derived", func(b *testing.B) {
		for iteration := 0; iteration < b.N; iteration++ {
			path := filepath.Join(b.TempDir(), fmt.Sprintf("batched-derived-%d.db", iteration))
			database, err := open(path, false)
			if err != nil {
				b.Fatal(err)
			}
			for start := 0; start < len(exports); start += 256 {
				end := min(start+256, len(exports))
				transaction, err := database.db.BeginTx(context.Background(), nil)
				if err != nil {
					b.Fatal(err)
				}
				for _, exported := range exports[start:end] {
					prepared, err := prepareExport(exported)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := database.commitExportTx(context.Background(), transaction, exported, prepared); err != nil {
						b.Fatal(err)
					}
				}
				if err := transaction.Commit(); err != nil {
					b.Fatal(err)
				}
			}
			if err := database.Close(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("deferred-derived", func(b *testing.B) {
		for iteration := 0; iteration < b.N; iteration++ {
			path := filepath.Join(b.TempDir(), fmt.Sprintf("batched-%d.db", iteration))
			candidate, err := OpenReplayCandidate(context.Background(), path, ReplayCandidateConfig{})
			if err != nil {
				b.Fatal(err)
			}
			for start := 0; start < len(exports); start += 256 {
				end := min(start+256, len(exports))
				if err := candidate.CommitReplayBatch(context.Background(), exports[start:end]); err != nil {
					b.Fatal(err)
				}
			}
			if err := candidate.FinalizeReplay(context.Background()); err != nil {
				b.Fatal(err)
			}
			if err := candidate.Close(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func benchmarkCodexCorroboratingExport(at time.Time, index int, usageID string) ingest.AcceptedExport {
	traceID := fmt.Sprintf("%032x", index+10_000)
	spanID := fmt.Sprintf("%016x", index+10_000)
	return ingest.AcceptedExport{
		Envelope: ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, at, []byte{0x0a, 0x02}),
		Journal:  ingest.JournalMetadata{Source: "codex", NormalizerVersion: 1, NormalizationStatus: "projected"},
		Projection: canonical.Batch{Spans: []canonical.Span{{
			Source: "codex", TraceID: traceID, SpanID: spanID, Kind: canonical.ActivityResponse,
			StartedAt: at, EndedAt: at, Attributes: map[string]any{
				"gen_ai.usage.role": "corroborating", "gen_ai.usage.id": usageID,
			}, Agent: canonical.AgentContext{RunID: "cost-benchmark-session"},
		}}},
	}
}

func replayEquivalenceExports(at time.Time) []ingest.AcceptedExport {
	first := costExport(at, "codex", "codex.sse_event", "gen_ai.response.completed", "replay-session", "gpt-6-astra", canonical.TokenUsage{Input: 10, Output: 2}, map[string]any{
		"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": "usage-1",
	})
	rawCost := 0.125
	unpriced := costExport(at.Add(500*time.Millisecond), "codex", "codex.sse_event", "gen_ai.response.completed", "unpriced-session", "unpriced-model", canonical.TokenUsage{Input: 3, Output: 1}, map[string]any{
		"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": "usage-unpriced",
	})
	unpriced.Projection.Logs[0].CostUSD = &rawCost
	second := codexCorroboratingSpanExport(at.Add(time.Second), "replay-session", "usage-1")
	third := costExport(at.Add(2*time.Second), "claude", "api_request", "gen_ai.model.request", "claude-replay", "claude-model", canonical.TokenUsage{Input: 4, Output: 1}, map[string]any{
		"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": "request-1",
	})
	claudeRuntime := claudeCorroboratingSpanExport(at.Add(3*time.Second), "claude-replay", "request-1")
	claudeRuntime.Projection.Spans[0].Agent.AgentID = "runtime-agent"
	claudeRuntime.Projection.Spans[0].Agent.ParentAgentID = "runtime-parent"
	failed := first
	failed.Envelope = ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, at.Add(4*time.Second), []byte{0x0a, 0x00})
	failed.Observations = nil
	failed.Projection = canonical.Batch{}
	failed.Journal.NormalizationStatus = "failed"
	failed.NormalizationError = "preserved normalization failure"
	metadataRevision := replayAgentMetadataExport(at.Add(7*time.Second), "00000000000000000000000000000011", "0000000000000012", "a-definition", "a-type", "a-parent", "a-model")
	metadataRevision.Projection.Spans[0].StartedAt = at.Add(time.Second)
	metadataRevision.Projection.Spans[0].EndedAt = at.Add(time.Second)
	return append([]ingest.AcceptedExport{first, unpriced, second, third, claudeRuntime, failed},
		replayAgentMetadataExport(at.Add(5*time.Second), "00000000000000000000000000000011", "0000000000000011", "z-definition", "z-type", "z-parent", "z-model"),
		replayAgentMetadataExport(at.Add(6*time.Second), "00000000000000000000000000000011", "0000000000000012", "a-definition", "a-type", "a-parent", "a-model"),
		metadataRevision,
		replaySessionLinkExport(at.Add(8*time.Second)),
	)
}

func replayAgentMetadataExport(at time.Time, traceID, spanID, definition, agentType, parentID, model string) ingest.AcceptedExport {
	return ingest.AcceptedExport{
		Envelope: ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, at, []byte{0x0a, 0x00}),
		Journal:  ingest.JournalMetadata{Source: "codex", NormalizerVersion: 1, NormalizationStatus: "projected"},
		Projection: canonical.Batch{Spans: []canonical.Span{{
			Source: "codex", TraceID: traceID, SpanID: spanID, Kind: canonical.ActivityResponse,
			StartedAt: at, EndedAt: at, Agent: canonical.AgentContext{
				RunID: "metadata-session", AgentID: "worker", AgentDefinition: definition,
				AgentType: agentType, ParentAgentID: parentID, Model: model,
			},
		}}},
	}
}

func replaySessionLinkExport(at time.Time) ingest.AcceptedExport {
	return ingest.AcceptedExport{
		Envelope: ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, at, []byte{0x0a, 0x03}),
		Journal:  ingest.JournalMetadata{Source: "codex", NormalizerVersion: 1, NormalizationStatus: "projected"},
		Projection: canonical.Batch{SessionLinks: []canonical.SessionLink{
			{Source: "codex", ParentSessionID: "root", ChildSessionID: "child", ObservedAt: at},
			{Source: "codex", ParentSessionID: "child", ChildSessionID: "grandchild", ObservedAt: at},
			{Source: "codex", ParentSessionID: "alternate", ChildSessionID: "child", ObservedAt: at},
		}},
	}
}

func deterministicRandRead() func([]byte) (int, error) {
	var call byte
	return func(value []byte) (int, error) {
		call++
		for index := range value {
			value[index] = call + byte(index)
		}
		return len(value), nil
	}
}

func logicalDatabaseSnapshot(t *testing.T, database *sql.DB) string {
	return logicalDatabaseSnapshotForTables(t, database, nil)
}

func logicalDatabaseSnapshotForTables(t *testing.T, database *sql.DB, included map[string]struct{}) string {
	t.Helper()
	rows, err := database.Query(`SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, table)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	var snapshot strings.Builder
	for _, table := range tables {
		if included != nil {
			if _, ok := included[table]; !ok {
				continue
			}
		}
		if table == "projection_feed_state" || table == "projection_changes" || table == "activity_changes" {
			continue
		}
		order := tablePrimaryKeyOrder(t, database, table)
		query := `SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `" ORDER BY ` + order
		tableRows, err := database.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := tableRows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&snapshot, "TABLE %s %v\n", table, columns)
		for tableRows.Next() {
			values := make([]any, len(columns))
			destinations := make([]any, len(columns))
			for index := range values {
				destinations[index] = &values[index]
			}
			if err := tableRows.Scan(destinations...); err != nil {
				t.Fatal(err)
			}
			for index, column := range columns {
				if column == "projection_sequence" {
					values[index] = "<sequence>"
				}
				if bytes, ok := values[index].([]byte); ok {
					values[index] = hex.EncodeToString(bytes)
				}
			}
			fmt.Fprintf(&snapshot, "%#v\n", values)
		}
		if err := tableRows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return snapshot.String()
}

func publicReplaySnapshot(t *testing.T, store *Store, at time.Time) string {
	t.Helper()
	ctx := context.Background()
	dashboard, err := store.GetDashboard(ctx, query.DashboardFilter{Since: at.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListSessions(ctx, query.SessionListFilter{Since: at.Add(-time.Hour), Page: replayPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	traces, err := store.ListTraces(ctx, query.TraceListFilter{Since: at.Add(-time.Hour), Page: replayPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	identity := mustConversationIdentityInternal(t, "codex", "replay-session")
	summary, err := store.GetSessionSummary(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	activities, err := store.ListSessionActivities(ctx, query.ActivityPageFilter{Identity: identity, Page: replayPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(struct {
		Dashboard  query.Overview
		Sessions   query.SessionPage
		Traces     query.TracePage
		Summary    query.Session
		Activities query.ActivityPage
	}{dashboard, sessions, traces, summary, activities})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func replayPage(t *testing.T, offset, size int) query.Page {
	t.Helper()
	page, err := query.NewPage(offset, size)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func tablePrimaryKeyOrder(t *testing.T, database *sql.DB, table string) string {
	t.Helper()
	rows, err := database.Query(`SELECT name FROM pragma_table_info(?) WHERE pk > 0 ORDER BY pk`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns := make([]string, 0)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, `"`+strings.ReplaceAll(column, `"`, `""`)+`"`)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(columns) == 0 {
		return "rowid"
	}
	return strings.Join(columns, ", ")
}

func firstSnapshotDifference(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	limit := len(wantLines)
	if len(gotLines) < limit {
		limit = len(gotLines)
	}
	for index := 0; index < limit; index++ {
		if wantLines[index] != gotLines[index] {
			return fmt.Sprintf("line %d\nwant: %s\n got: %s", index+1, wantLines[index], gotLines[index])
		}
	}
	return fmt.Sprintf("snapshot lengths differ: want=%d got=%d", len(want), len(got))
}
