package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/source/builtin"
)

func TestReplayCandidateBatchMatchesSequentialCommitAtEveryPrefix(t *testing.T) {
	exports := replayEquivalenceExports(time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC))
	originalRandRead := randRead
	t.Cleanup(func() { randRead = originalRandRead })

	randRead = deterministicRandRead()
	reference, err := open(filepath.Join(t.TempDir(), "reference.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if err := reference.ReplaceRateHistoryForReplay(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	referenceSnapshots := make([]string, 0, 2)
	for _, bounds := range [][2]int{{0, 2}, {2, len(exports)}} {
		for _, exported := range exports[bounds[0]:bounds[1]] {
			if err := reference.CommitExport(context.Background(), exported); err != nil {
				t.Fatal(err)
			}
		}
		referenceSnapshots = append(referenceSnapshots, logicalDatabaseSnapshot(t, reference.db))
	}
	if err := reference.Close(); err != nil {
		t.Fatal(err)
	}

	randRead = deterministicRandRead()
	candidate, err := OpenReplayCandidate(context.Background(), filepath.Join(t.TempDir(), "candidate.db"), ReplayCandidateConfig{
		Profiles: builtin.Registry(), RateHistory: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = candidate.Close() })
	for index, bounds := range [][2]int{{0, 2}, {2, len(exports)}} {
		if err := candidate.CommitReplayBatch(context.Background(), exports[bounds[0]:bounds[1]]); err != nil {
			t.Fatal(err)
		}
		if got := logicalDatabaseSnapshot(t, candidate.store.db); got != referenceSnapshots[index] {
			t.Fatalf("logical candidate differs at prefix %d\n%s", bounds[1], firstSnapshotDifference(referenceSnapshots[index], got))
		}
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
	b.Run("batches-of-256", func(b *testing.B) {
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
			if err := candidate.Close(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func replayEquivalenceExports(at time.Time) []ingest.AcceptedExport {
	first := costExport(at, "codex", "codex.sse_event", "gen_ai.response.completed", "replay-session", "gpt-6-astra", canonical.TokenUsage{Input: 10, Output: 2}, map[string]any{
		"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": "usage-1",
	})
	second := codexCorroboratingSpanExport(at.Add(time.Second), "replay-session", "usage-1")
	third := costExport(at.Add(2*time.Second), "claude", "api_request", "gen_ai.model.request", "claude-replay", "claude-model", canonical.TokenUsage{Input: 4, Output: 1}, map[string]any{
		"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": "request-1",
	})
	failed := first
	failed.Envelope = ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, at.Add(3*time.Second), []byte{0x0a, 0x00})
	failed.Observations = nil
	failed.Projection = canonical.Batch{}
	failed.Journal.NormalizationStatus = "failed"
	failed.NormalizationError = "preserved normalization failure"
	return []ingest.AcceptedExport{first, second, third, failed}
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
		query := `SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `" ORDER BY rowid`
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
				if table == "projection_changes" && column == "committed_at" {
					values[index] = "<clock>"
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
