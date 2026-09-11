package compaction

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kotokumu/agentmetry/internal/archivefs"
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

func validateCandidate(ctx context.Context, path, archiveDirectory string, expected validationExpectation) error {
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
	if err := validateRetentionCatalog(ctx, database, archiveDirectory); err != nil {
		return err
	}
	return nil
}

func validateRetentionCatalog(ctx context.Context, database *sql.DB, archiveDirectory string) error {
	var violations int
	if err := database.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM retained_exports r WHERE r.state = 'archived' AND (SELECT COUNT(*) FROM current_archive_memberships m WHERE m.export_id = r.id) <> 1)
 + (SELECT COUNT(*) FROM retained_exports r WHERE r.state IN ('active', 'deleted') AND EXISTS (SELECT 1 FROM current_archive_memberships m WHERE m.export_id = r.id))
 + (SELECT COUNT(*) FROM current_archive_memberships m JOIN retained_exports r ON r.id = m.export_id JOIN archive_segments s ON s.id = m.segment_id WHERE r.state <> 'archived' OR s.reference_state <> 'current')
 + (SELECT COUNT(*) FROM archive_segments s WHERE s.reference_state = 'current' AND ((SELECT COUNT(*) FROM current_archive_memberships m WHERE m.segment_id = s.id) <> s.export_count OR s.file_name IS NULL))
 + (SELECT COUNT(*) FROM retained_exports WHERE state = 'deleted' AND (deleted_at IS NULL OR deletion_policy_revision IS NULL OR deletion_cutoff_days IS NULL OR deletion_evaluated_at IS NULL OR deletion_operation_id IS NULL))
 + (SELECT COUNT(*) FROM retention_export_authorities a JOIN retention_operations o ON o.id = a.operation_id WHERE o.status NOT IN ('pending', 'running'))`).Scan(&violations); err != nil {
		return fmt.Errorf("validate candidate retention invariants: %w", err)
	}
	if violations != 0 {
		return fmt.Errorf("candidate retention catalog has %d invariant violations", violations)
	}
	if err := database.QueryRowContext(ctx, `WITH RECURSIVE reach(origin, node) AS (
 SELECT original_segment_id, replacement_segment_id FROM archive_segment_replacements
 UNION
 SELECT reach.origin, replacements.replacement_segment_id FROM reach
 JOIN archive_segment_replacements replacements ON replacements.original_segment_id = reach.node
) SELECT COUNT(*) FROM reach WHERE origin = node`).Scan(&violations); err != nil {
		return fmt.Errorf("validate candidate replacement graph: %w", err)
	}
	if violations != 0 {
		return fmt.Errorf("candidate archive replacement graph contains a cycle")
	}
	rows, err := database.QueryContext(ctx, `SELECT id, file_sha256, membership_sha256, export_count, stored_bytes
FROM archive_segments WHERE reference_state = 'current' ORDER BY id`)
	if err != nil {
		return err
	}
	type current struct {
		id, fileHash, membershipHash string
		count                        int
		storedBytes                  int64
	}
	var segments []current
	for rows.Next() {
		var segment current
		if err := rows.Scan(&segment.id, &segment.fileHash, &segment.membershipHash, &segment.count, &segment.storedBytes); err != nil {
			_ = rows.Close()
			return err
		}
		segments = append(segments, segment)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	archives := archivefs.New(archiveDirectory)
	for _, segment := range segments {
		verified, err := archives.OpenVerified(ctx, segment.id)
		if err != nil {
			return fmt.Errorf("validate current archive %s: %w", segment.id, err)
		}
		if verified.FileSHA256 != segment.fileHash || verified.MembershipSHA256 != segment.membershipHash || len(verified.Exports) != segment.count || verified.StoredBytes != segment.storedBytes {
			return fmt.Errorf("current archive %s differs from retention catalog", segment.id)
		}
		membershipRows, err := database.QueryContext(ctx, `SELECT immutable.export_id, immutable.ordinal, current.segment_id, current.ordinal, retained.received_at
FROM archive_segment_members immutable
JOIN retained_exports retained ON retained.id = immutable.export_id
LEFT JOIN current_archive_memberships current ON current.export_id = immutable.export_id
WHERE immutable.segment_id = ? ORDER BY immutable.ordinal`, segment.id)
		if err != nil {
			return err
		}
		index := 0
		for membershipRows.Next() {
			if index >= len(verified.Members) {
				_ = membershipRows.Close()
				return fmt.Errorf("current archive %s has extra catalog membership", segment.id)
			}
			var exportID, immutableOrdinal int64
			var currentSegment sql.NullString
			var currentOrdinal sql.NullInt64
			var receivedAt string
			if err := membershipRows.Scan(&exportID, &immutableOrdinal, &currentSegment, &currentOrdinal, &receivedAt); err != nil {
				_ = membershipRows.Close()
				return err
			}
			member := verified.Members[index]
			if member.ID != exportID || immutableOrdinal != int64(index) || !currentSegment.Valid || currentSegment.String != segment.id || !currentOrdinal.Valid || currentOrdinal.Int64 != int64(index) || member.ReceivedAt != receivedAt {
				_ = membershipRows.Close()
				return fmt.Errorf("current archive %s member %d differs from SQLite authority", segment.id, index)
			}
			index++
		}
		if err := membershipRows.Close(); err != nil {
			return err
		}
		if index != len(verified.Members) {
			return fmt.Errorf("current archive %s catalog omits manifest members", segment.id)
		}
	}
	return nil
}
