package compaction

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

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
	journalCount  int64
	journalDigest [sha256.Size]byte
	observations  int64
	logs          int64
	metrics       int64
	planSnapshots int64
	rates         int64
}

func (expected *validationExpectation) add(record storedExport, accepted ingest.AcceptedExport) error {
	identity := journalIdentity{
		ReceivedAt: record.ReceivedAt.UTC().Format(time.RFC3339Nano),
		Signal:     string(record.Signal), Transport: string(record.Transport), Size: record.Size,
		Hash: record.Hash, Source: record.Metadata.Source,
		NormalizerVersion:  record.Metadata.NormalizerVersion,
		Status:             record.Metadata.NormalizationStatus,
		NormalizationError: record.NormalizationError,
		HarnessState:       string(record.Metadata.Harness.State), HarnessScope: record.Metadata.Harness.Scope,
		HarnessFingerprint: record.Metadata.Harness.Fingerprint, HarnessLabel: record.Metadata.Harness.Label,
	}
	expected.journalCount++
	digest, err := extendJournalDigest(expected.journalDigest, identity)
	if err != nil {
		return err
	}
	expected.journalDigest = digest
	expected.observations += int64(len(accepted.Observations))
	expected.logs += int64(len(accepted.Projection.Logs))
	expected.metrics += int64(len(accepted.Projection.Metrics))
	return nil
}

func extendJournalDigest(previous [sha256.Size]byte, identity journalIdentity) ([sha256.Size]byte, error) {
	payload, err := json.Marshal(identity)
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("encode journal identity: %w", err)
	}
	hasher := sha256.New()
	_, _ = hasher.Write(previous[:])
	_, _ = hasher.Write(payload)
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
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
	var journalDigest [sha256.Size]byte
	var index int64
	for rows.Next() {
		if index >= expected.journalCount {
			return fmt.Errorf("candidate contains unexpected export %d", index+1)
		}
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
		journalDigest, err = extendJournalDigest(journalDigest, got)
		if err != nil {
			return err
		}
		index++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if index != expected.journalCount {
		return fmt.Errorf("candidate exports %d, want %d", index, expected.journalCount)
	}
	if journalDigest != expected.journalDigest {
		return fmt.Errorf("candidate journal identity digest differs")
	}
	wantCounts := map[string]int64{
		"observations":         expected.observations,
		"logs":                 expected.logs,
		"metrics":              expected.metrics,
		"plan_usage_snapshots": expected.planSnapshots,
		"model_rates":          expected.rates,
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
	foreignKeys, err := database.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("validate candidate foreign keys: %w", err)
	}
	if foreignKeys.Next() {
		_ = foreignKeys.Close()
		return fmt.Errorf("candidate contains a foreign-key violation")
	}
	if err := foreignKeys.Close(); err != nil {
		return err
	}
	var calls, attributions, missingSupport, missingMembership, pendingCosts, replaySequences int64
	if err := database.QueryRowContext(ctx, `SELECT
  (SELECT COUNT(*) FROM model_calls),
  (SELECT COUNT(*) FROM model_call_attributions),
  (SELECT COUNT(*) FROM model_call_trace_memberships m WHERE NOT EXISTS (
    SELECT 1 FROM model_call_trace_supports s WHERE s.call_id = m.call_id AND s.trace_id = m.trace_id
  )),
  (SELECT COUNT(*) FROM model_call_trace_supports s WHERE NOT EXISTS (
    SELECT 1 FROM model_call_trace_memberships m WHERE m.call_id = s.call_id AND m.trace_id = s.trace_id
  )),
  (SELECT COUNT(*) FROM model_call_attributions WHERE primary_reason = 'pending_replay_finalization'),
  (SELECT COUNT(*) FROM spans WHERE projection_sequence <> 0)
    + (SELECT COUNT(*) FROM logs WHERE projection_sequence <> 0)
    + (SELECT COUNT(*) FROM metrics WHERE projection_sequence <> 0)
    + (SELECT COUNT(*) FROM model_calls WHERE projection_sequence <> 0)`).Scan(
		&calls, &attributions, &missingSupport, &missingMembership, &pendingCosts, &replaySequences,
	); err != nil {
		return fmt.Errorf("validate candidate cost projection: %w", err)
	}
	if calls != attributions || missingSupport != 0 || missingMembership != 0 || pendingCosts != 0 || replaySequences != 0 {
		return fmt.Errorf("candidate projection is incomplete: calls=%d attributions=%d membership_without_support=%d support_without_membership=%d pending_costs=%d replay_sequences=%d", calls, attributions, missingSupport, missingMembership, pendingCosts, replaySequences)
	}
	return nil
}
