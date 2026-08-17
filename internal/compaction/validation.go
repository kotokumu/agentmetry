package compaction

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/journal"
)

type journalIdentity struct {
	ReceivedAt         string
	Signal             string
	Transport          string
	Size               int
	Hash               [sha256.Size]byte
	Source             string
	NormalizerVersion  int
	Status             string
	NormalizationError string
	HarnessState       string
	HarnessScope       string
	HarnessFingerprint string
	HarnessLabel       string
}

type validationExpectation struct {
	journals      []journalIdentity
	spanKeys      map[string]struct{}
	sessionLinks  map[string]struct{}
	sessionNodes  map[string]struct{}
	observations  int64
	logs          int64
	metrics       int64
	planSnapshots int64
}

func (expected *validationExpectation) add(record storedExport, accepted ingest.AcceptedExport) {
	expected.journals = append(expected.journals, journalIdentity{
		ReceivedAt: record.ReceivedAt.UTC().Format(time.RFC3339Nano),
		Signal:     string(record.Signal), Transport: string(record.Transport), Size: record.Size,
		Hash: record.Hash, Source: record.Metadata.Source,
		NormalizerVersion:  record.Metadata.NormalizerVersion,
		Status:             record.Metadata.NormalizationStatus,
		NormalizationError: record.NormalizationError,
		HarnessState:       string(record.Metadata.Harness.State), HarnessScope: record.Metadata.Harness.Scope,
		HarnessFingerprint: record.Metadata.Harness.Fingerprint, HarnessLabel: record.Metadata.Harness.Label,
	})
	expected.observations += int64(len(accepted.Observations))
	expected.logs += int64(len(accepted.Projection.Logs))
	expected.metrics += int64(len(accepted.Projection.Metrics))
	if expected.spanKeys == nil {
		expected.spanKeys = make(map[string]struct{})
	}
	for _, span := range accepted.Projection.Spans {
		if canonical.IsSemanticSpan(span) {
			expected.spanKeys[span.TraceID+"\x00"+span.SpanID] = struct{}{}
		}
	}
	if expected.sessionLinks == nil {
		expected.sessionLinks = make(map[string]struct{})
		expected.sessionNodes = make(map[string]struct{})
	}
	for _, link := range accepted.Projection.SessionLinks {
		if link.ParentSessionID == "" || link.ChildSessionID == "" || link.ParentSessionID == link.ChildSessionID {
			continue
		}
		sourceID := link.Source
		if sourceID == "" {
			sourceID = "unknown"
		}
		expected.sessionLinks[sourceID+"\x00"+link.ParentSessionID+"\x00"+link.ChildSessionID] = struct{}{}
		expected.sessionNodes[sourceID+"\x00"+link.ParentSessionID] = struct{}{}
		expected.sessionNodes[sourceID+"\x00"+link.ChildSessionID] = struct{}{}
	}
}

func validateCandidate(ctx context.Context, path string, expected validationExpectation) error {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open candidate for validation: %w", err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return err
	}
	var integrity string
	if err := database.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return fmt.Errorf("candidate integrity check: %q: %w", integrity, err)
	}
	var version int
	if err := database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != CurrentStorageGeneration {
		return fmt.Errorf("candidate storage generation %d, want %d: %w", version, CurrentStorageGeneration, err)
	}
	rows, err := database.QueryContext(ctx, `SELECT received_at, signal, transport,
payload_protobuf, payload_codec, payload_size, payload_sha256, source,
normalizer_version, normalization_status, normalization_error,
harness_receipt_state, harness_scope, harness_fingerprint, harness_label
FROM otlp_exports ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		if index >= len(expected.journals) {
			return fmt.Errorf("candidate contains unexpected export %d", index+1)
		}
		want := expected.journals[index]
		var received, signal, transport, codecText, hashText, sourceID, status, normalizationError string
		var harnessState, harnessScope, harnessFingerprint, harnessLabel string
		var stored []byte
		var size, normalizerVersion int
		if err := rows.Scan(&received, &signal, &transport, &stored, &codecText, &size, &hashText, &sourceID, &normalizerVersion, &status, &normalizationError, &harnessState, &harnessScope, &harnessFingerprint, &harnessLabel); err != nil {
			return err
		}
		hashBytes, err := hex.DecodeString(hashText)
		if err != nil || len(hashBytes) != sha256.Size {
			return fmt.Errorf("candidate export %d has invalid hash", index+1)
		}
		var hash [sha256.Size]byte
		copy(hash[:], hashBytes)
		if _, err := journal.Restore(journal.Codec(codecText), stored, size, hash); err != nil {
			return fmt.Errorf("validate candidate export %d: %w", index+1, err)
		}
		got := journalIdentity{
			ReceivedAt: received, Signal: signal, Transport: transport, Size: size, Hash: hash,
			Source: sourceID, NormalizerVersion: normalizerVersion, Status: status, NormalizationError: normalizationError,
			HarnessState: harnessState, HarnessScope: harnessScope, HarnessFingerprint: harnessFingerprint, HarnessLabel: harnessLabel,
		}
		if got != want {
			return fmt.Errorf("candidate export %d metadata differs: got %#v want %#v", index+1, got, want)
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if index != len(expected.journals) {
		return fmt.Errorf("candidate exports %d, want %d", index, len(expected.journals))
	}
	wantCounts := map[string]int64{
		"observations":         expected.observations,
		"spans":                int64(len(expected.spanKeys)),
		"logs":                 expected.logs,
		"metrics":              expected.metrics,
		"session_links":        int64(len(expected.sessionLinks)),
		"session_memberships":  int64(len(expected.sessionNodes)),
		"plan_usage_snapshots": expected.planSnapshots,
	}
	for table, want := range wantCounts {
		var got int64
		if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("candidate %s count %d, want %d", table, got, want)
		}
	}
	return nil
}
