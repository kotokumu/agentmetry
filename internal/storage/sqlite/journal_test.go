package sqlite

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/harness"
	"github.com/theoden9014/agentmetry/internal/ingest"
	"github.com/theoden9014/agentmetry/internal/journal"
	"github.com/theoden9014/agentmetry/internal/observation"
)

func TestOpenRefusesPendingDataMigrationInsteadOfCreatingAnEmptyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentmetry.db")
	if err := os.WriteFile(path+".migration.json", []byte(`{"phase":"source-preserved"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("database opened while its canonical path was under migration")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Open created a replacement database: %v", err)
	}
}

func TestCommitExportRejectsInvalidHarnessValuesWithoutPersistingThem(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	accepted := ingest.AcceptedExport{
		Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, time.Now(), []byte{0x0a, 0x00}),
		Journal: ingest.JournalMetadata{Harness: harness.ReceiptEvidence{
			State: harness.ReceiptInvalid, Scope: "secret-invalid-raw-value",
		}},
	}

	if err := database.CommitExport(context.Background(), accepted); err == nil {
		t.Fatal("CommitExport accepted invalid raw harness values")
	}
	var exports int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM otlp_exports").Scan(&exports); err != nil {
		t.Fatal(err)
	}
	if exports != 0 {
		t.Fatalf("exports = %d, want 0", exports)
	}
}

func TestCommitExportAtomicallyStoresJournalObservationsAndReadModel(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	now := time.Date(2026, 8, 11, 4, 0, 0, 0, time.UTC)
	payload := []byte{0x0a, 0x00}
	accepted := ingest.AcceptedExport{
		Envelope: ingest.Envelope{
			Signal:     canonical.SignalLog,
			Transport:  ingest.TransportGRPC,
			ReceivedAt: now,
			Protobuf:   payload,
		},
		Observations: []observation.Observation{{
			Ordinal:           0,
			Signal:            canonical.SignalLog,
			Kind:              canonical.ActivityResponse,
			Source:            "example",
			SourceEventName:   "gen_ai.response.completed",
			OccurredAt:        now,
			SessionID:         "session-1",
			Model:             "gpt-example",
			Usage:             canonical.TokenUsage{Input: 10, Output: 2},
			NormalizerVersion: 1,
		}},
		Projection: canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{{
			ObservedAt: now,
			Name:       "gen_ai.response.completed",
			Kind:       canonical.ActivityResponse,
			Agent: canonical.AgentContext{
				RunID:  "session-1",
				Model:  "gpt-example",
				Tokens: canonical.TokenUsage{Input: 10, Output: 2},
			},
		}}},
	}

	if err := database.CommitExport(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}

	var exportCount, observationCount, logCount int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM otlp_exports").Scan(&exportCount); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow("SELECT COUNT(*) FROM observations").Scan(&observationCount); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&logCount); err != nil {
		t.Fatal(err)
	}
	if exportCount != 1 || observationCount != 1 || logCount != 1 {
		t.Fatalf("counts = exports:%d observations:%d logs:%d", exportCount, observationCount, logCount)
	}

	var storedPayload []byte
	var codec, storedHash, status string
	var originalSize int
	if err := database.db.QueryRow("SELECT payload_protobuf, payload_codec, payload_size, payload_sha256, normalization_status FROM otlp_exports").Scan(&storedPayload, &codec, &originalSize, &storedHash, &status); err != nil {
		t.Fatal(err)
	}
	hashBytes, err := hex.DecodeString(storedHash)
	if err != nil {
		t.Fatal(err)
	}
	var hash [32]byte
	copy(hash[:], hashBytes)
	decoded, err := journal.Restore(journal.Codec(codec), storedPayload, originalSize, hash)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(payload) || status != "projected" {
		t.Fatalf("unexpected journal row: payload=%x status=%q", decoded, status)
	}
}

func TestCommitExportProjectsOnlySemanticTraceRecords(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	now := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	accepted := ingest.AcceptedExport{
		Envelope: ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, now, []byte{0x0a, 0x00}),
		Observations: []observation.Observation{
			{Ordinal: 1, Signal: canonical.SignalTrace, Kind: canonical.ActivityTool, SourceEventName: "call", OccurredAt: now, ObservedAt: now, SessionID: "conversation-1"},
		},
		Projection: canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{
			{TraceID: "trace-1", SpanID: "runtime", Name: "handle_responses", Kind: canonical.ActivityResponse, StartedAt: now, EndedAt: now, Attributes: map[string]any{}},
			{TraceID: "trace-1", SpanID: "semantic", Name: "call", Kind: canonical.ActivityTool, StartedAt: now, EndedAt: now, Agent: canonical.AgentContext{RunID: "conversation-1"}, Attributes: map[string]any{}},
		}},
	}

	if err := database.CommitExport(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}
	var observations, spans int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM observations").Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow("SELECT COUNT(*) FROM spans").Scan(&spans); err != nil {
		t.Fatal(err)
	}
	if observations != 1 || spans != 1 {
		t.Fatalf("projected runtime telemetry: observations=%d spans=%d", observations, spans)
	}
}

func TestCommitExportRetainsRawPayloadWhenNormalizationFails(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	now := time.Date(2026, 8, 11, 4, 30, 0, 0, time.UTC)
	payload := []byte{0x0a, 0x00}
	accepted := ingest.AcceptedExport{
		Envelope:           ingest.NewEnvelope(canonical.SignalLog, ingest.TransportHTTPProtobuf, now, payload),
		NormalizationError: "unsupported source revision",
		Projection: canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{{
			ObservedAt: now,
			Name:       "must-not-be-projected",
		}}},
	}

	if err := database.CommitExport(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}

	var storedPayload []byte
	var status, normalizationError string
	if err := database.db.QueryRow(`
SELECT payload_protobuf, normalization_status, normalization_error
FROM otlp_exports
`).Scan(&storedPayload, &status, &normalizationError); err != nil {
		t.Fatal(err)
	}
	var observationCount, logCount int
	if err := database.db.QueryRow("SELECT COUNT(*) FROM observations").Scan(&observationCount); err != nil {
		t.Fatal(err)
	}
	if err := database.db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&logCount); err != nil {
		t.Fatal(err)
	}
	if string(storedPayload) != string(payload) || status != "failed" || normalizationError != "unsupported source revision" {
		t.Fatalf("unexpected failed journal row: payload=%x status=%q error=%q", storedPayload, status, normalizationError)
	}
	if observationCount != 0 || logCount != 0 {
		t.Fatalf("failed normalization created derived rows: observations=%d logs=%d", observationCount, logCount)
	}
}
