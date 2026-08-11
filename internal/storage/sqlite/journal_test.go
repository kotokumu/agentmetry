package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/ingest"
	"github.com/theoden9014/agentmetry/internal/observation"
)

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
			Signal:        canonical.SignalLog,
			Transport:     ingest.TransportGRPC,
			ReceivedAt:    now,
			Protobuf:      payload,
			CanonicalJSON: json.RawMessage(`{"resourceLogs":[]}`),
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
			Payload:           json.RawMessage(`{"complete":true}`),
			SourceAttributes:  json.RawMessage(`{"vendor":"kept"}`),
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
	var storedJSON, status string
	if err := database.db.QueryRow("SELECT payload_protobuf, payload_json, normalization_status FROM otlp_exports").Scan(&storedPayload, &storedJSON, &status); err != nil {
		t.Fatal(err)
	}
	if string(storedPayload) != string(payload) || !json.Valid([]byte(storedJSON)) || status != "projected" {
		t.Fatalf("unexpected journal row: payload=%x json=%q status=%q", storedPayload, storedJSON, status)
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
		Envelope: ingest.NewEnvelope(
			canonical.SignalLog,
			ingest.TransportHTTPProtobuf,
			now,
			payload,
			json.RawMessage(`{"resourceLogs":[]}`),
		),
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
