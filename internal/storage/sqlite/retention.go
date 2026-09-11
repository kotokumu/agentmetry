package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/journal"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/retention"
)

func (store *Store) ArchiveDirectory() string { return store.path + ".archives" }

func (store *Store) RetentionPolicy(ctx context.Context) (retention.Policy, time.Time, error) {
	var enabled int
	var archiveDays, deleteDays sql.NullInt64
	var revision int64
	var updatedText string
	if err := store.readDB.QueryRowContext(ctx, `SELECT enabled, archive_days, delete_days, revision, updated_at FROM retention_policy WHERE id = 1`).Scan(&enabled, &archiveDays, &deleteDays, &revision, &updatedText); err != nil {
		return retention.Policy{}, time.Time{}, fmt.Errorf("read retention policy: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, updatedText)
	if err != nil {
		return retention.Policy{}, time.Time{}, fmt.Errorf("parse retention policy update time: %w", err)
	}
	if enabled == 0 {
		return retention.DisabledPolicy(revision), updatedAt, nil
	}
	policy, err := retention.NewPolicy(int(archiveDays.Int64), int(deleteDays.Int64), revision)
	return policy, updatedAt, err
}

func (store *Store) UpdateRetentionPolicy(ctx context.Context, enabled bool, archiveDays, deleteDays int, updatedAt time.Time) (retention.Policy, error) {
	var candidate retention.Policy
	var err error
	if enabled {
		candidate, err = retention.NewPolicy(archiveDays, deleteDays, 0)
		if err != nil {
			return retention.Policy{}, err
		}
	}
	if err := store.lockWrite(ctx); err != nil {
		return retention.Policy{}, fmt.Errorf("wait for retention policy writer: %w", err)
	}
	defer store.unlockWrite()
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return retention.Policy{}, fmt.Errorf("begin retention policy update: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	var revision int64
	if err := transaction.QueryRowContext(ctx, `SELECT revision FROM retention_policy WHERE id = 1`).Scan(&revision); err != nil {
		return retention.Policy{}, fmt.Errorf("read retention policy revision: %w", err)
	}
	revision++
	if enabled {
		_, err = transaction.ExecContext(ctx, `UPDATE retention_policy SET enabled = 1, archive_days = ?, delete_days = ?, revision = ?, updated_at = ? WHERE id = 1`, archiveDays, deleteDays, revision, formatTime(updatedAt))
		candidate.Revision = revision
	} else {
		_, err = transaction.ExecContext(ctx, `UPDATE retention_policy SET enabled = 0, archive_days = NULL, delete_days = NULL, revision = ?, updated_at = ? WHERE id = 1`, revision, formatTime(updatedAt))
		candidate = retention.DisabledPolicy(revision)
	}
	if err != nil {
		return retention.Policy{}, fmt.Errorf("update retention policy: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return retention.Policy{}, fmt.Errorf("commit retention policy: %w", err)
	}
	return candidate, nil
}

func (store *Store) EligibleActiveExports(ctx context.Context, policy retention.Policy, evaluatedAt time.Time) ([]retention.RawExport, error) {
	return store.eligibleActiveExports(ctx, policy, evaluatedAt, 0)
}

func (store *Store) EligibleActiveExportsForCycle(ctx context.Context, policy retention.Policy, evaluatedAt time.Time, cycleID string) ([]retention.RawExport, error) {
	var highWater int64
	if err := store.readDB.QueryRowContext(ctx, `SELECT COALESCE(cohort_max_export_id, 0) FROM retention_cycles WHERE id = ? AND status = 'running'`, cycleID).Scan(&highWater); err != nil {
		return nil, fmt.Errorf("read retention cycle cohort boundary: %w", err)
	}
	if highWater == 0 {
		return nil, nil
	}
	return store.eligibleActiveExports(ctx, policy, evaluatedAt, highWater)
}

func (store *Store) eligibleActiveExports(ctx context.Context, policy retention.Policy, evaluatedAt time.Time, highWater int64) ([]retention.RawExport, error) {
	if !policy.Enabled {
		return nil, nil
	}
	cutoff := evaluatedAt.UTC().Add(-policy.ArchiveAfter.Duration())
	highWaterClause := ""
	arguments := []any{formatTime(cutoff), formatTime(evaluatedAt)}
	if highWater > 0 {
		highWaterClause = " AND r.id <= ?"
		arguments = append(arguments, highWater)
	}
	rows, err := store.readDB.QueryContext(ctx, `SELECT e.id, e.received_at, e.signal, e.transport,
 e.payload_protobuf, e.payload_codec, e.payload_sha256, e.payload_occurrence, e.payload_size,
 e.source, e.normalizer_version, e.normalization_status, e.normalization_error,
 e.harness_receipt_state, e.harness_scope, e.harness_fingerprint, e.harness_label
FROM otlp_exports e JOIN retained_exports r ON r.id = e.id
WHERE r.state = 'active' AND r.received_at <= ? AND (r.hold_until IS NULL OR r.hold_until <= ?)
  `+highWaterClause+`
  AND NOT EXISTS (SELECT 1 FROM retention_export_authorities a WHERE a.export_id = r.id)
ORDER BY r.received_at, r.id`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("select archive-eligible exports: %w", err)
	}
	defer rows.Close()
	var exports []retention.RawExport
	for rows.Next() {
		var value retention.RawExport
		var receivedText, codecText, hashText string
		var stored []byte
		var originalSize int
		if err := rows.Scan(&value.ID, &receivedText, &value.Signal, &value.Transport, &stored, &codecText, &hashText,
			&value.PayloadOccurrence, &originalSize, &value.Source, &value.NormalizerVersion,
			&value.NormalizationStatus, &value.NormalizationError, &value.HarnessState, &value.HarnessScope,
			&value.HarnessFingerprint, &value.HarnessLabel); err != nil {
			return nil, fmt.Errorf("scan archive-eligible export: %w", err)
		}
		value.ReceivedAt, err = time.Parse(time.RFC3339Nano, receivedText)
		if err != nil {
			return nil, fmt.Errorf("parse retained export receive time: %w", err)
		}
		hashBytes, err := hex.DecodeString(hashText)
		if err != nil || len(hashBytes) != sha256.Size {
			return nil, fmt.Errorf("decode retained export %d digest", value.ID)
		}
		var hash [sha256.Size]byte
		copy(hash[:], hashBytes)
		value.Protobuf, err = journal.Restore(journal.Codec(codecText), stored, originalSize, hash)
		if err != nil {
			return nil, fmt.Errorf("decode retained export %d: %w", value.ID, err)
		}
		exports = append(exports, value)
	}
	return exports, rows.Err()
}

func (store *Store) ClaimArchive(ctx context.Context, exports []retention.RawExport, policy retention.Policy, evaluatedAt time.Time, cycleID string) (retention.ArchiveClaim, error) {
	if len(exports) == 0 {
		return retention.ArchiveClaim{}, fmt.Errorf("claim archive: empty export set")
	}
	if err := store.lockWrite(ctx); err != nil {
		return retention.ArchiveClaim{}, fmt.Errorf("wait for archive claim: %w", err)
	}
	defer store.unlockWrite()
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return retention.ArchiveClaim{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	var enabled int
	var archiveDays, revision int64
	if err := transaction.QueryRowContext(ctx, `SELECT enabled, COALESCE(archive_days, 0), revision FROM retention_policy WHERE id = 1`).Scan(&enabled, &archiveDays, &revision); err != nil {
		return retention.ArchiveClaim{}, err
	}
	if enabled == 0 || revision != policy.Revision || archiveDays != int64(policy.ArchiveAfter) {
		return retention.ArchiveClaim{}, fmt.Errorf("claim archive: retention policy changed")
	}
	operationID, err := store.newActivityID()
	if err != nil {
		return retention.ArchiveClaim{}, err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO retention_operations (
 id, cycle_id, kind, status, phase, requested_at, evaluated_at, affected_export_count, affected_segment_count
) VALUES (?, ?, 'archive', 'running', 'planned', ?, ?, ?, 1)`, operationID, nullableString(cycleID), formatTime(evaluatedAt), formatTime(evaluatedAt), len(exports)); err != nil {
		return retention.ArchiveClaim{}, fmt.Errorf("record archive operation: %w", err)
	}
	cutoff := evaluatedAt.UTC().Add(-policy.ArchiveAfter.Duration())
	for _, exported := range exports {
		var state, receivedText string
		var holdUntil sql.NullString
		if err := transaction.QueryRowContext(ctx, `SELECT state, received_at, hold_until FROM retained_exports WHERE id = ?`, exported.ID).Scan(&state, &receivedText, &holdUntil); err != nil {
			return retention.ArchiveClaim{}, fmt.Errorf("claim retained export %d: %w", exported.ID, err)
		}
		receivedAt, parseErr := time.Parse(time.RFC3339Nano, receivedText)
		if parseErr != nil || state != "active" || receivedAt.After(cutoff) {
			return retention.ArchiveClaim{}, fmt.Errorf("claim archive: export %d is no longer eligible", exported.ID)
		}
		if holdUntil.Valid {
			hold, parseErr := time.Parse(time.RFC3339Nano, holdUntil.String)
			if parseErr != nil || hold.After(evaluatedAt) {
				return retention.ArchiveClaim{}, fmt.Errorf("claim archive: export %d is held", exported.ID)
			}
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO retention_operation_exports (operation_id, export_id, role) VALUES (?, ?, 'affected')`, operationID, exported.ID); err != nil {
			return retention.ArchiveClaim{}, err
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO retention_export_authorities (export_id, operation_id, kind, role) VALUES (?, ?, 'archive', 'affected')`, exported.ID, operationID); err != nil {
			return retention.ArchiveClaim{}, fmt.Errorf("claim archive authority for export %d: %w", exported.ID, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return retention.ArchiveClaim{}, err
	}
	return retention.ArchiveClaim{OperationID: operationID, Exports: exports}, nil
}

func (store *Store) FailArchive(ctx context.Context, operationID string, failure error, completedAt time.Time) error {
	if operationID == "" {
		return nil
	}
	if err := store.lockWrite(ctx); err != nil {
		return err
	}
	defer store.unlockWrite()
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id = ?`, operationID); err != nil {
		return err
	}
	message := "archive failed"
	if failure != nil {
		message = failure.Error()
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET status = 'failed', phase = 'terminal', completed_at = ?, error = ?
WHERE id = ? AND kind = 'archive' AND status = 'running'`, formatTime(completedAt), message, operationID); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) PublishArchive(ctx context.Context, publication retention.ArchivePublication) error {
	if publication.OperationID == "" || len(publication.Exports) == 0 || publication.SegmentID == "" {
		return fmt.Errorf("publish archive: empty segment")
	}
	if err := store.lockWrite(ctx); err != nil {
		return fmt.Errorf("wait for archive publisher: %w", err)
	}
	defer store.unlockWrite()
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin archive publication: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	var operationStatus, operationPhase string
	if err := transaction.QueryRowContext(ctx, `SELECT status, phase FROM retention_operations WHERE id = ? AND kind = 'archive'`, publication.OperationID).Scan(&operationStatus, &operationPhase); err != nil {
		return fmt.Errorf("revalidate archive operation: %w", err)
	}
	if operationStatus != "running" || operationPhase != "planned" {
		return fmt.Errorf("archive operation %s is no longer publishable", publication.OperationID)
	}
	var enabled int
	var archiveDays, revision int64
	if err := transaction.QueryRowContext(ctx, `SELECT enabled, COALESCE(archive_days, 0), revision FROM retention_policy WHERE id = 1`).Scan(&enabled, &archiveDays, &revision); err != nil {
		return fmt.Errorf("revalidate archive policy: %w", err)
	}
	if enabled == 0 || revision != publication.PolicyRevision {
		return fmt.Errorf("publish archive: retention policy changed")
	}
	cutoff := publication.EvaluatedAt.UTC().Add(-time.Duration(archiveDays) * retention.Day)
	ids := make([]int64, len(publication.Exports))
	for index, exported := range publication.Exports {
		ids[index] = exported.ID
		var state, receivedText string
		var holdUntil sql.NullString
		if err := transaction.QueryRowContext(ctx, `SELECT state, received_at, hold_until FROM retained_exports WHERE id = ?`, exported.ID).Scan(&state, &receivedText, &holdUntil); err != nil {
			return fmt.Errorf("revalidate retained export %d: %w", exported.ID, err)
		}
		receivedAt, err := time.Parse(time.RFC3339Nano, receivedText)
		if err != nil || state != "active" || receivedAt.After(cutoff) {
			return fmt.Errorf("publish archive: export %d is no longer eligible", exported.ID)
		}
		if holdUntil.Valid {
			hold, parseErr := time.Parse(time.RFC3339Nano, holdUntil.String)
			if parseErr != nil || hold.After(publication.EvaluatedAt) {
				return fmt.Errorf("publish archive: export %d is held", exported.ID)
			}
		}
		var role string
		if err := transaction.QueryRowContext(ctx, `SELECT role FROM retention_export_authorities WHERE export_id = ? AND operation_id = ? AND kind = 'archive'`, exported.ID, publication.OperationID).Scan(&role); err != nil || role != "affected" {
			return fmt.Errorf("revalidate archive authority for export %d: %w", exported.ID, err)
		}
	}
	operationID := publication.OperationID
	var targetCount int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM retention_operation_exports WHERE operation_id = ?`, operationID).Scan(&targetCount); err != nil || targetCount != len(publication.Exports) {
		return fmt.Errorf("archive operation target set changed: durable=%d publication=%d: %w", targetCount, len(publication.Exports), err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET phase = 'file_ready' WHERE id = ?`, operationID); err != nil {
		return err
	}
	minAt, maxAt := publication.Exports[0].ReceivedAt, publication.Exports[0].ReceivedAt
	for _, exported := range publication.Exports[1:] {
		if exported.ReceivedAt.Before(minAt) {
			minAt = exported.ReceivedAt
		}
		if exported.ReceivedAt.After(maxAt) {
			maxAt = exported.ReceivedAt
		}
	}
	_, segmentErr := transaction.ExecContext(ctx, `INSERT INTO archive_segments (
 id, reference_state, file_name, representation_version, payload_integrity, metadata_integrity,
 min_received_at, max_received_at, export_count, original_bytes, stored_bytes, file_sha256,
 membership_sha256, verified_at, created_at
) VALUES (?, 'current', ?, 1, 'intact', 'verifiable', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		publication.SegmentID, filepath.Base(publication.FileName), formatTime(minAt), formatTime(maxAt), len(publication.Exports),
		publication.OriginalBytes, publication.StoredBytes, publication.FileSHA256, publication.MembershipSHA256,
		formatTime(publication.EvaluatedAt), formatTime(publication.EvaluatedAt))
	if segmentErr != nil {
		return fmt.Errorf("publish archive segment without reusing historical identity: %w", segmentErr)
	}
	for ordinal, exported := range publication.Exports {
		if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO archive_segment_members (segment_id, export_id, ordinal) VALUES (?, ?, ?)`, publication.SegmentID, exported.ID, ordinal); err != nil {
			return fmt.Errorf("publish archive member: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO current_archive_memberships (export_id, segment_id, ordinal) VALUES (?, ?, ?)`, exported.ID, publication.SegmentID, ordinal); err != nil {
			return fmt.Errorf("publish current archive membership: %w", err)
		}
	}
	if err := store.removeActiveExports(ctx, transaction, ids); err != nil {
		return err
	}
	placeholders, args := int64Placeholders(ids)
	if _, err := transaction.ExecContext(ctx, `UPDATE retained_exports SET state = 'archived', hold_until = NULL, prior_segment_id = ? WHERE id IN (`+placeholders+`)`, append([]any{publication.SegmentID}, args...)...); err != nil {
		return fmt.Errorf("publish archived export state: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id = ?`, operationID); err != nil {
		return fmt.Errorf("release archive authority: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET status = 'completed', phase = 'terminal', completed_at = ? WHERE id = ?`, formatTime(time.Now()), operationID); err != nil {
		return fmt.Errorf("complete archive operation: %w", err)
	}
	if err := pruneTerminalRetentionOperations(ctx, transaction); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit archive publication: %w", err)
	}
	store.signalProjectionChange()
	return nil
}

func (store *Store) removeActiveExports(ctx context.Context, transaction *sql.Tx, ids []int64) error {
	placeholders, args := int64Placeholders(ids)
	type affectedActivity struct {
		source, runID, traceID, activityID string
		sessionMember                      bool
	}
	activityArgs := append(append([]any(nil), args...), args...)
	activityRows, err := transaction.QueryContext(ctx, `SELECT source, run_id, trace_id, activity_id, CASE WHEN activity_kind <> 'unknown' THEN 1 ELSE 0 END FROM spans WHERE export_id IN (`+placeholders+`)
UNION ALL SELECT source, run_id, trace_id, activity_id, CASE WHEN activity_kind <> 'unknown' THEN 1 ELSE 0 END FROM logs WHERE export_id IN (`+placeholders+`)`, activityArgs...)
	if err != nil {
		return fmt.Errorf("select affected archive activities: %w", err)
	}
	var activities []affectedActivity
	for activityRows.Next() {
		var item affectedActivity
		var member int
		if err := activityRows.Scan(&item.source, &item.runID, &item.traceID, &item.activityID, &member); err != nil {
			_ = activityRows.Close()
			return err
		}
		item.sessionMember = member == 1
		activities = append(activities, item)
	}
	if err := activityRows.Close(); err != nil {
		return err
	}
	type spanKey struct{ traceID, spanID string }
	rows, err := transaction.QueryContext(ctx, `SELECT DISTINCT trace_id, span_id FROM span_projection_candidates WHERE export_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return fmt.Errorf("select affected span candidates: %w", err)
	}
	var keys []spanKey
	for rows.Next() {
		var key spanKey
		if err := rows.Scan(&key.traceID, &key.spanID); err != nil {
			_ = rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, statement := range []string{
		`DELETE FROM model_calls WHERE export_id IN (` + placeholders + `)`,
		`DELETE FROM model_call_evidence WHERE export_id IN (` + placeholders + `)`,
		`DELETE FROM logs WHERE export_id IN (` + placeholders + `)`,
		`DELETE FROM metrics WHERE export_id IN (` + placeholders + `)`,
		`DELETE FROM session_link_evidence WHERE export_id IN (` + placeholders + `)`,
		`DELETE FROM span_projection_candidates WHERE export_id IN (` + placeholders + `)`,
	} {
		if _, err := transaction.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("remove export-owned projection: %w", err)
		}
	}
	for _, key := range keys {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM spans WHERE trace_id = ? AND span_id = ?`, key.traceID, key.spanID); err != nil {
			return err
		}
		var exportID, sequence int64
		var projectionJSON []byte
		err := transaction.QueryRowContext(ctx, `SELECT c.export_id, c.projection_sequence, c.projection_json
FROM span_projection_candidates c JOIN retained_exports r ON r.id = c.export_id
WHERE c.trace_id = ? AND c.span_id = ? AND r.state = 'active'
ORDER BY c.export_id DESC LIMIT 1`, key.traceID, key.spanID).Scan(&exportID, &sequence, &projectionJSON)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return fmt.Errorf("select fallback span projection: %w", err)
		}
		var span canonical.Span
		if err := json.Unmarshal(projectionJSON, &span); err != nil {
			return fmt.Errorf("decode fallback span projection: %w", err)
		}
		if err := putSpan(ctx, transaction, exportID, span, sequence, store.activityID("span:"+span.TraceID+":"+span.SpanID)); err != nil {
			return err
		}
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM session_links`); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO session_links (source, parent_session_id, child_session_id, observed_at)
SELECT source, parent_session_id, child_session_id, MAX(observed_at) FROM session_link_evidence
GROUP BY source, parent_session_id, child_session_id`); err != nil {
		return fmt.Errorf("rebuild session links: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_calls WHERE source = 'claude'`); err != nil {
		return err
	}
	if err := store.rebuildReplayCostProjections(ctx, transaction); err != nil {
		return err
	}
	for _, table := range []string{"session_memberships", "session_agents", "trace_agents"} {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM `+table); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	if _, err := transaction.ExecContext(ctx, sessionAgentAggregateInsert(``, ``)); err != nil {
		return fmt.Errorf("rebuild session agents: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, traceAgentInsert(``, ``)); err != nil {
		return fmt.Errorf("rebuild trace agents: %w", err)
	}
	if err := rebuildReplaySessionMemberships(ctx, transaction); err != nil {
		return err
	}
	if err := rebuildReplaySessionAggregates(ctx, transaction); err != nil {
		return err
	}
	if err := rebuildReplayTraceAggregates(ctx, transaction); err != nil {
		return err
	}
	sequence, err := appendProjectionChange(ctx, transaction, []query.ChangeTarget{query.OverviewTarget(), query.AllSourcesTarget(), query.AllSessionsTarget(), query.AllTracesTarget()})
	if err != nil {
		return err
	}
	ordinal := 0
	for _, old := range activities {
		if old.sessionMember && old.runID != "" {
			ordinal++
			if err := appendActivityChange(ctx, transaction, sequence, ordinal, "session", old.source, old.runID, old.activityID, "remove"); err != nil {
				return err
			}
		}
		if old.traceID != "" {
			ordinal++
			if err := appendActivityChange(ctx, transaction, sequence, ordinal, "trace", "", old.traceID, old.activityID, "remove"); err != nil {
				return err
			}
		}
		var current affectedActivity
		var member int
		err := transaction.QueryRowContext(ctx, `SELECT source, run_id, trace_id, activity_id, CASE WHEN activity_kind <> 'unknown' THEN 1 ELSE 0 END FROM spans WHERE activity_id = ?
UNION ALL SELECT source, run_id, trace_id, activity_id, CASE WHEN activity_kind <> 'unknown' THEN 1 ELSE 0 END FROM logs WHERE activity_id = ? LIMIT 1`, old.activityID, old.activityID).Scan(&current.source, &current.runID, &current.traceID, &current.activityID, &member)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		current.sessionMember = member == 1
		if current.sessionMember && current.runID != "" {
			ordinal++
			if err := appendActivityChange(ctx, transaction, sequence, ordinal, "session", current.source, current.runID, current.activityID, "upsert"); err != nil {
				return err
			}
		}
		if current.traceID != "" {
			ordinal++
			if err := appendActivityChange(ctx, transaction, sequence, ordinal, "trace", "", current.traceID, current.activityID, "upsert"); err != nil {
				return err
			}
		}
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM observations WHERE export_id IN (`+placeholders+`)`, args...); err != nil {
		return fmt.Errorf("remove archived observations: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM otlp_exports WHERE id IN (`+placeholders+`)`, args...); err != nil {
		return fmt.Errorf("remove archived raw exports: %w", err)
	}
	return nil
}

func int64Placeholders(values []int64) (string, []any) {
	parts := make([]string, len(values))
	args := make([]any, len(values))
	for index, value := range values {
		parts[index], args[index] = "?", value
	}
	return strings.Join(parts, ","), args
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (store *Store) ListArchiveSegments(ctx context.Context, after string, limit int) ([]retention.Segment, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := store.readDB.QueryContext(ctx, `SELECT id, reference_state, COALESCE(file_name, ''),
	payload_integrity, metadata_integrity, min_received_at, max_received_at, export_count,
 original_bytes, stored_bytes, file_sha256, membership_sha256, verified_at, created_at, COALESCE(verification_error, '')
FROM archive_segments WHERE reference_state = 'current' AND id > ? ORDER BY id LIMIT ?`, after, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("list archive segments: %w", err)
	}
	defer rows.Close()
	segments := make([]retention.Segment, 0, limit)
	for rows.Next() {
		var segment retention.Segment
		var reference, payload, metadata, minText, maxText, verifiedText, createdText string
		if err := rows.Scan(&segment.ID, &reference, &segment.FileName, &payload, &metadata, &minText, &maxText,
			&segment.ExportCount, &segment.OriginalBytes, &segment.StoredBytes, &segment.FileSHA256,
			&segment.MembershipSHA256, &verifiedText, &createdText, &segment.IntegrityError); err != nil {
			return nil, "", err
		}
		segment.ReferenceState = retention.SegmentReferenceState(reference)
		segment.PayloadIntegrity = retention.PayloadIntegrity(payload)
		segment.MetadataIntegrity = retention.MetadataIntegrity(metadata)
		var parseErr error
		if segment.MinReceivedAt, parseErr = time.Parse(time.RFC3339Nano, minText); parseErr != nil {
			return nil, "", parseErr
		}
		if segment.MaxReceivedAt, parseErr = time.Parse(time.RFC3339Nano, maxText); parseErr != nil {
			return nil, "", parseErr
		}
		if segment.VerifiedAt, parseErr = time.Parse(time.RFC3339Nano, verifiedText); parseErr != nil {
			return nil, "", parseErr
		}
		if segment.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdText); parseErr != nil {
			return nil, "", parseErr
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(segments) > limit {
		next = segments[limit-1].ID
		segments = segments[:limit]
	}
	return segments, next, nil
}

func (store *Store) ClassifyRestoreScope(ctx context.Context, scope retention.RestoreScope) (retention.RestoreClassification, error) {
	if scope.Kind == retention.RestoreByPeriod {
		result := retention.RestoreClassification{}
		var restoring int
		if err := store.readDB.QueryRowContext(ctx, `SELECT COUNT(*)
FROM retained_exports r JOIN retention_export_authorities a ON a.export_id = r.id
WHERE a.kind = 'restore' AND r.received_at >= ? AND r.received_at < ?`, formatTime(scope.Start), formatTime(scope.End)).Scan(&restoring); err != nil {
			return result, err
		}
		if restoring > 0 {
			return result, fmt.Errorf("%w: receive-time scope intersects %d restoring exports", retention.ErrRestoreConflict, restoring)
		}
		if err := store.readDB.QueryRowContext(ctx, `SELECT
 COALESCE(SUM(CASE WHEN state = 'active' THEN 1 ELSE 0 END), 0),
 COALESCE(SUM(CASE WHEN state = 'archived' THEN 1 ELSE 0 END), 0),
 COALESCE(SUM(CASE WHEN state = 'deleted' THEN 1 ELSE 0 END), 0)
FROM retained_exports WHERE received_at >= ? AND received_at < ?`, formatTime(scope.Start), formatTime(scope.End)).Scan(&result.ActiveMatches, &result.ArchivedMatches, &result.DeletedMatches); err != nil {
			return result, err
		}
		if result.ArchivedMatches == 0 {
			result.Result = "no_archived_match"
		} else {
			result.Result = "current"
		}
		return result, nil
	}
	result := retention.RestoreClassification{}
	var state string
	var exportCount int64
	if err := store.readDB.QueryRowContext(ctx, `SELECT reference_state, export_count FROM archive_segments WHERE id = ?`, scope.SegmentID).Scan(&state, &exportCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			result.Result = "not_found"
			return result, nil
		}
		return result, err
	}
	result.Result = state
	if state == string(retention.SegmentDeleted) {
		result.DeletedMatches = exportCount
	}
	if state == string(retention.SegmentCurrent) {
		result.CurrentSegmentIDs = []string{scope.SegmentID}
		return result, nil
	}
	if state != string(retention.SegmentSuperseded) {
		return result, nil
	}
	rows, err := store.readDB.QueryContext(ctx, `WITH RECURSIVE reach(id) AS (
 SELECT replacement_segment_id FROM archive_segment_replacements WHERE original_segment_id = ?
 UNION
 SELECT replacements.replacement_segment_id FROM reach
 JOIN archive_segment_replacements replacements ON replacements.original_segment_id = reach.id
) SELECT reach.id FROM reach JOIN archive_segments segments ON segments.id = reach.id
WHERE segments.reference_state = 'current' ORDER BY reach.id`, scope.SegmentID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return result, err
		}
		result.CurrentSegmentIDs = append(result.CurrentSegmentIDs, id)
	}
	return result, rows.Err()
}

func (store *Store) CurrentSegmentsForRestore(ctx context.Context, scope retention.RestoreScope) ([]retention.Segment, error) {
	if scope.Kind == retention.RestoreBySegment {
		var state string
		if err := store.readDB.QueryRowContext(ctx, `SELECT reference_state FROM archive_segments WHERE id = ?`, scope.SegmentID).Scan(&state); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("archive segment %s is unknown", scope.SegmentID)
			}
			return nil, err
		}
		if state != string(retention.SegmentCurrent) {
			return nil, fmt.Errorf("archive segment %s is %s", scope.SegmentID, state)
		}
		after := ""
		for {
			segments, next, err := store.ListArchiveSegments(ctx, after, 100)
			if err != nil {
				return nil, err
			}
			for _, segment := range segments {
				if segment.ID == scope.SegmentID {
					return []retention.Segment{segment}, nil
				}
			}
			if next == "" {
				break
			}
			after = next
		}
		return nil, fmt.Errorf("archive segment %s is not restorable", scope.SegmentID)
	}
	rows, err := store.readDB.QueryContext(ctx, `SELECT DISTINCT m.segment_id
FROM current_archive_memberships m JOIN retained_exports r ON r.id = m.export_id
WHERE r.state = 'archived' AND r.received_at >= ? AND r.received_at < ? ORDER BY m.segment_id`, formatTime(scope.Start), formatTime(scope.End))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var result []retention.Segment
	after := ""
	for {
		segments, next, err := store.ListArchiveSegments(ctx, after, 100)
		if err != nil {
			return nil, err
		}
		for _, segment := range segments {
			if _, ok := ids[segment.ID]; ok {
				result = append(result, segment)
			}
		}
		if next == "" {
			break
		}
		after = next
	}
	return result, nil
}

// ClaimRestore durably owns the complete affected set before archive bytes are
// read or normalized. A competing restore or deletion therefore fails instead
// of observing partially prepared work.
func (store *Store) ClaimRestore(ctx context.Context, scope retention.RestoreScope, requestedAt time.Time) (retention.RestoreClaim, error) {
	if err := store.lockWrite(ctx); err != nil {
		return retention.RestoreClaim{}, fmt.Errorf("wait for restore claim: %w", err)
	}
	defer store.unlockWrite()
	if err := store.finishCommittedDeletionsForScope(ctx, scope); err != nil {
		return retention.RestoreClaim{}, err
	}
	classification, err := store.ClassifyRestoreScope(ctx, scope)
	if err != nil {
		return retention.RestoreClaim{}, err
	}
	if scope.Kind == retention.RestoreBySegment && classification.Result != string(retention.SegmentCurrent) {
		return retention.RestoreClaim{Scope: scope, Classification: classification}, nil
	}
	if scope.Kind == retention.RestoreByPeriod && classification.ArchivedMatches == 0 {
		return retention.RestoreClaim{Scope: scope, Classification: classification}, nil
	}
	segments, err := store.CurrentSegmentsForRestore(ctx, scope)
	if err != nil {
		return retention.RestoreClaim{}, err
	}
	if len(segments) == 0 {
		return retention.RestoreClaim{Scope: scope, Classification: classification}, nil
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return retention.RestoreClaim{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	operationID, err := store.newActivityID()
	if err != nil {
		return retention.RestoreClaim{}, err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO retention_operations (id, kind, status, phase, requested_at)
VALUES (?, 'restore', 'running', 'planned', ?)`, operationID, formatTime(requestedAt)); err != nil {
		return retention.RestoreClaim{}, err
	}
	type claimTarget struct {
		exportID int64
		role     string
	}
	var targets []claimTarget
	for _, segment := range segments {
		rows, err := transaction.QueryContext(ctx, `SELECT m.export_id, r.received_at
FROM current_archive_memberships m JOIN retained_exports r ON r.id = m.export_id
WHERE m.segment_id = ? AND r.state = 'archived' ORDER BY m.ordinal`, segment.ID)
		if err != nil {
			return retention.RestoreClaim{}, err
		}
		for rows.Next() {
			var exportID int64
			var receivedText string
			if err := rows.Scan(&exportID, &receivedText); err != nil {
				_ = rows.Close()
				return retention.RestoreClaim{}, err
			}
			role := "affected"
			receivedAt, parseErr := time.Parse(time.RFC3339Nano, receivedText)
			if parseErr != nil {
				_ = rows.Close()
				return retention.RestoreClaim{}, parseErr
			}
			if scope.Kind == retention.RestoreBySegment || (!receivedAt.Before(scope.Start) && receivedAt.Before(scope.End)) {
				role = "selected"
			}
			targets = append(targets, claimTarget{exportID: exportID, role: role})
		}
		if err := rows.Close(); err != nil {
			return retention.RestoreClaim{}, err
		}
	}
	for _, target := range targets {
		var competingID, competingKind, competingPhase string
		var stagingToken sql.NullString
		err := transaction.QueryRowContext(ctx, `SELECT a.operation_id, a.kind, o.phase, o.staging_token
FROM retention_export_authorities a JOIN retention_operations o ON o.id = a.operation_id
WHERE a.export_id = ?`, target.exportID).Scan(&competingID, &competingKind, &competingPhase, &stagingToken)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return retention.RestoreClaim{}, err
		}
		if err == nil {
			if competingKind != "delete" || (competingPhase != "staging" && competingPhase != "staged") || !stagingToken.Valid {
				return retention.RestoreClaim{}, fmt.Errorf("%w: export %d is owned by %s operation %s", retention.ErrRestoreConflict, target.exportID, competingKind, competingID)
			}
			file, err := store.deletionFile(stagingToken.String)
			if err != nil {
				return retention.RestoreClaim{}, err
			}
			presence, err := file.Inspect()
			if err != nil {
				return retention.RestoreClaim{}, err
			}
			if presence == retention.DeletionStaged {
				if err := file.RestoreAndSync(); err != nil {
					return retention.RestoreClaim{}, fmt.Errorf("preempt staged deletion: %w", err)
				}
				presence, err = file.Inspect()
				if err != nil {
					return retention.RestoreClaim{}, err
				}
			}
			if presence != retention.DeletionInstalled {
				return retention.RestoreClaim{}, fmt.Errorf("%w: preempted deletion has no installed archive content", retention.ErrRestoreConflict)
			}
			if _, err := transaction.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id = ?`, competingID); err != nil {
				return retention.RestoreClaim{}, err
			}
			if _, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET status = 'cancelled', phase = 'terminal', completed_at = ?, error = 'preempted by restore before deletion cutover' WHERE id = ? AND status = 'running'`, formatTime(requestedAt), competingID); err != nil {
				return retention.RestoreClaim{}, err
			}
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO retention_operation_exports (operation_id, export_id, role) VALUES (?, ?, ?)`, operationID, target.exportID, target.role); err != nil {
			return retention.RestoreClaim{}, err
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO retention_export_authorities (export_id, operation_id, kind, role) VALUES (?, ?, 'restore', ?)`, target.exportID, operationID, target.role); err != nil {
			return retention.RestoreClaim{}, fmt.Errorf("%w: export %d: %v", retention.ErrRestoreConflict, target.exportID, err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET affected_export_count = ?, affected_segment_count = ? WHERE id = ?`, len(targets), len(segments), operationID); err != nil {
		return retention.RestoreClaim{}, err
	}
	if err := transaction.Commit(); err != nil {
		return retention.RestoreClaim{}, err
	}
	return retention.RestoreClaim{OperationID: operationID, Segments: segments, Scope: scope, Classification: classification}, nil
}

func (store *Store) finishCommittedDeletionsForScope(ctx context.Context, scope retention.RestoreScope) error {
	query := `SELECT DISTINCT o.id, o.staging_token
FROM retention_operations o
JOIN retention_export_authorities a ON a.operation_id = o.id
JOIN retained_exports r ON r.id = a.export_id
WHERE o.kind = 'delete' AND o.status = 'running' AND o.phase IN ('delete_committing', 'content_removed')`
	var arguments []any
	if scope.Kind == retention.RestoreBySegment {
		query += ` AND o.staging_token = ?`
		arguments = append(arguments, scope.SegmentID)
	} else {
		query += ` AND r.received_at >= ? AND r.received_at < ?`
		arguments = append(arguments, formatTime(scope.Start), formatTime(scope.End))
	}
	query += ` ORDER BY o.id`
	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return err
	}
	type committedDeletion struct{ operationID, segmentID string }
	var operations []committedDeletion
	for rows.Next() {
		var operation committedDeletion
		if err := rows.Scan(&operation.operationID, &operation.segmentID); err != nil {
			_ = rows.Close()
			return err
		}
		operations = append(operations, operation)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, operation := range operations {
		file, err := store.deletionFile(operation.segmentID)
		if err != nil {
			return err
		}
		if err := store.recoverDeletionOperation(ctx, operation.operationID, operation.segmentID, file); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) FailRestore(ctx context.Context, operationID string, failure error, completedAt time.Time) error {
	if operationID == "" {
		return nil
	}
	if err := store.lockWrite(ctx); err != nil {
		return err
	}
	defer store.unlockWrite()
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id = ?`, operationID); err != nil {
		return err
	}
	message := "restore failed"
	if failure != nil {
		message = failure.Error()
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET status = 'failed', phase = 'terminal', completed_at = ?, error = ?
WHERE id = ? AND kind = 'restore' AND status = 'running'`, formatTime(completedAt), message, operationID); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) RecordSegmentVerificationFailure(ctx context.Context, segmentID string, payload retention.PayloadIntegrity, metadata retention.MetadataIntegrity, verifiedAt time.Time, cause error) error {
	if err := store.lockWrite(ctx); err != nil {
		return err
	}
	defer store.unlockWrite()
	result, err := store.db.ExecContext(ctx, `UPDATE archive_segments SET payload_integrity = ?, metadata_integrity = ?, verified_at = ?, verification_error = ?
WHERE id = ? AND reference_state = 'current'`, payload, metadata, formatTime(verifiedAt), cause.Error(), segmentID)
	if err != nil {
		return fmt.Errorf("record corrupt archive segment %s: %w", segmentID, err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("record corrupt archive segment %s after %v: segment is no longer current", segmentID, cause)
	}
	return nil
}

func (store *Store) RecordRestoreRetry(ctx context.Context, operationID string, cause error, attemptedAt time.Time) error {
	if err := store.lockWrite(ctx); err != nil {
		return err
	}
	defer store.unlockWrite()
	message := "restore cleanup retry"
	if cause != nil {
		message = cause.Error()
	}
	_, err := store.db.ExecContext(ctx, `UPDATE retention_operations SET error = ?, evaluated_at = ?
WHERE id = ? AND kind = 'restore' AND status = 'running' AND phase = 'content_removed'`, message, formatTime(attemptedAt), operationID)
	return err
}

func (store *Store) CompleteRestore(ctx context.Context, operationID string, completedAt time.Time) error {
	if err := store.lockWrite(ctx); err != nil {
		return err
	}
	defer store.unlockWrite()
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id = ?`, operationID); err != nil {
		return err
	}
	result, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET status = 'completed', phase = 'terminal', completed_at = ?, error = NULL
WHERE id = ? AND kind = 'restore' AND status = 'running' AND phase = 'content_removed'`, formatTime(completedAt), operationID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("restore operation %s is not ready to complete", operationID)
	}
	if err := pruneTerminalRetentionOperations(ctx, transaction); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) PublishRestore(ctx context.Context, publication retention.RestorePublication) error {
	if len(publication.Selected) == 0 {
		return nil
	}
	if err := store.lockWrite(ctx); err != nil {
		return fmt.Errorf("wait for restore publisher: %w", err)
	}
	defer store.unlockWrite()
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin restore publication: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	operationID := publication.OperationID
	var operationStatus, operationPhase string
	if err := transaction.QueryRowContext(ctx, `SELECT status, phase FROM retention_operations WHERE id = ? AND kind = 'restore'`, operationID).Scan(&operationStatus, &operationPhase); err != nil {
		return fmt.Errorf("revalidate restore operation: %w", err)
	}
	if operationStatus != "running" || operationPhase != "planned" {
		return fmt.Errorf("restore operation %s is no longer publishable", operationID)
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET phase = 'file_ready' WHERE id = ?`, operationID); err != nil {
		return err
	}
	selectedIDs := make(map[int64]struct{}, len(publication.Selected))
	affectedIDs := make(map[int64]struct{})
	for _, accepted := range publication.Selected {
		if accepted.Identity.ID <= 0 {
			return fmt.Errorf("restore publication has no stable identity")
		}
		if _, duplicate := selectedIDs[accepted.Identity.ID]; duplicate {
			return fmt.Errorf("restore publication duplicates selected export %d", accepted.Identity.ID)
		}
		selectedIDs[accepted.Identity.ID] = struct{}{}
		var state, segmentID, receivedText string
		if err := transaction.QueryRowContext(ctx, `SELECT r.state, m.segment_id, r.received_at
FROM retained_exports r JOIN current_archive_memberships m ON m.export_id = r.id WHERE r.id = ?`, accepted.Identity.ID).Scan(&state, &segmentID, &receivedText); err != nil {
			return fmt.Errorf("revalidate restore export %d: %w", accepted.Identity.ID, err)
		}
		if state != "archived" || receivedText != formatTime(accepted.Envelope.ReceivedAt) {
			return fmt.Errorf("restore export %d changed", accepted.Identity.ID)
		}
	}
	originalMembers := make(map[string]map[int64]struct{}, len(publication.OriginalSegments))
	for _, segmentID := range publication.OriginalSegments {
		if _, duplicate := originalMembers[segmentID]; duplicate {
			return fmt.Errorf("restore publication duplicates original segment %s", segmentID)
		}
		var referenceState, metadataIntegrity string
		if err := transaction.QueryRowContext(ctx, `SELECT reference_state, metadata_integrity FROM archive_segments WHERE id = ?`, segmentID).Scan(&referenceState, &metadataIntegrity); err != nil {
			return fmt.Errorf("revalidate original segment %s: %w", segmentID, err)
		}
		if referenceState != "current" || metadataIntegrity != "verifiable" {
			return fmt.Errorf("original segment %s is no longer current and verifiable", segmentID)
		}
		members := make(map[int64]struct{})
		rows, err := transaction.QueryContext(ctx, `SELECT export_id FROM current_archive_memberships WHERE segment_id = ? ORDER BY ordinal`, segmentID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			affectedIDs[id] = struct{}{}
			members[id] = struct{}{}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(members) == 0 {
			return fmt.Errorf("original segment %s has no current membership", segmentID)
		}
		originalMembers[segmentID] = members
	}
	var claimedCount int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM retention_operation_exports WHERE operation_id = ?`, operationID).Scan(&claimedCount); err != nil {
		return err
	}
	if claimedCount != len(affectedIDs) {
		return fmt.Errorf("restore affected set changed: claimed=%d current=%d", claimedCount, len(affectedIDs))
	}
	for id := range affectedIDs {
		var role string
		if err := transaction.QueryRowContext(ctx, `SELECT role FROM retention_operation_exports WHERE operation_id = ? AND export_id = ?`, operationID, id).Scan(&role); err != nil {
			return fmt.Errorf("restore affected export %d was not claimed: %w", id, err)
		}
		_, selected := selectedIDs[id]
		if (role == "selected") != selected {
			return fmt.Errorf("restore selected set differs from durable claim for export %d", id)
		}
	}
	for id := range selectedIDs {
		if _, affected := affectedIDs[id]; !affected {
			return fmt.Errorf("restore selected export %d is outside the affected segments", id)
		}
	}
	replacementMembers := make(map[int64]struct{}, len(affectedIDs)-len(selectedIDs))
	replacedOriginals := make(map[string]struct{}, len(publication.Replacements))
	for _, replacement := range publication.Replacements {
		members, known := originalMembers[replacement.OriginalSegmentID]
		if !known {
			return fmt.Errorf("replacement refers to foreign original segment %s", replacement.OriginalSegmentID)
		}
		if _, duplicate := replacedOriginals[replacement.OriginalSegmentID]; duplicate {
			return fmt.Errorf("restore publication has multiple replacements for %s", replacement.OriginalSegmentID)
		}
		replacedOriginals[replacement.OriginalSegmentID] = struct{}{}
		for _, exported := range replacement.Segment.Exports {
			if _, belongs := members[exported.ID]; !belongs {
				return fmt.Errorf("replacement includes export %d from another segment", exported.ID)
			}
			if _, selected := selectedIDs[exported.ID]; selected {
				return fmt.Errorf("replacement includes selected export %d", exported.ID)
			}
			if _, duplicate := replacementMembers[exported.ID]; duplicate {
				return fmt.Errorf("replacement duplicates export %d", exported.ID)
			}
			replacementMembers[exported.ID] = struct{}{}
		}
	}
	for id := range affectedIDs {
		if _, selected := selectedIDs[id]; selected {
			continue
		}
		if _, replaced := replacementMembers[id]; !replaced {
			return fmt.Errorf("restore replacement omits affected export %d", id)
		}
	}
	for id := range affectedIDs {
		role := "affected"
		if _, ok := selectedIDs[id]; ok {
			role = "selected"
		}
		var claimedRole string
		if err := transaction.QueryRowContext(ctx, `SELECT role FROM retention_export_authorities WHERE export_id = ? AND operation_id = ? AND kind = 'restore'`, id, operationID).Scan(&claimedRole); err != nil {
			return fmt.Errorf("revalidate restore authority for export %d: %w", id, err)
		}
		if claimedRole != role {
			return fmt.Errorf("restore role changed for export %d", id)
		}
	}
	for _, originalID := range publication.OriginalSegments {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM current_archive_memberships WHERE segment_id = ?`, originalID); err != nil {
			return err
		}
	}
	for _, replacement := range publication.Replacements {
		segment := replacement.Segment
		replacementPublishedAt := time.Now().UTC()
		if len(segment.Exports) == 0 {
			return fmt.Errorf("empty replacement for %s", replacement.OriginalSegmentID)
		}
		minAt, maxAt := segment.Exports[0].ReceivedAt, segment.Exports[0].ReceivedAt
		for _, exported := range segment.Exports[1:] {
			if exported.ReceivedAt.Before(minAt) {
				minAt = exported.ReceivedAt
			}
			if exported.ReceivedAt.After(maxAt) {
				maxAt = exported.ReceivedAt
			}
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO archive_segments (
 id, reference_state, file_name, representation_version, payload_integrity, metadata_integrity,
 min_received_at, max_received_at, export_count, original_bytes, stored_bytes, file_sha256,
 membership_sha256, verified_at, created_at
) VALUES (?, 'current', ?, 1, 'intact', 'verifiable', ?, ?, ?, ?, ?, ?, ?, ?, ?)`, segment.SegmentID,
			filepath.Base(segment.FileName), formatTime(minAt), formatTime(maxAt), len(segment.Exports), segment.OriginalBytes,
			segment.StoredBytes, segment.FileSHA256, segment.MembershipSHA256, formatTime(replacementPublishedAt), formatTime(replacementPublishedAt)); err != nil {
			return fmt.Errorf("publish replacement segment: %w", err)
		}
		for ordinal, exported := range segment.Exports {
			if _, selected := selectedIDs[exported.ID]; selected {
				return fmt.Errorf("replacement includes selected export %d", exported.ID)
			}
			if _, affected := affectedIDs[exported.ID]; !affected {
				return fmt.Errorf("replacement includes foreign export %d", exported.ID)
			}
			if _, err := transaction.ExecContext(ctx, `INSERT INTO archive_segment_members (segment_id, export_id, ordinal) VALUES (?, ?, ?)`, segment.SegmentID, exported.ID, ordinal); err != nil {
				return err
			}
			if _, err := transaction.ExecContext(ctx, `INSERT INTO current_archive_memberships (export_id, segment_id, ordinal) VALUES (?, ?, ?)`, exported.ID, segment.SegmentID, ordinal); err != nil {
				return err
			}
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO archive_segment_replacements (original_segment_id, replacement_segment_id) VALUES (?, ?)`, replacement.OriginalSegmentID, segment.SegmentID); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE archive_segments SET reference_state = 'superseded', file_name = NULL WHERE id = ?`, replacement.OriginalSegmentID); err != nil {
			return err
		}
	}
	replaced := make(map[string]struct{}, len(publication.Replacements))
	for _, replacement := range publication.Replacements {
		replaced[replacement.OriginalSegmentID] = struct{}{}
	}
	for _, originalID := range publication.OriginalSegments {
		if _, ok := replaced[originalID]; ok {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE archive_segments SET reference_state = 'restored', file_name = NULL WHERE id = ?`, originalID); err != nil {
			return err
		}
	}
	for _, accepted := range publication.Selected {
		prepared, err := prepareExport(accepted)
		if err != nil {
			return err
		}
		if _, err := store.commitExportTx(ctx, transaction, accepted, prepared); err != nil {
			return fmt.Errorf("restore export %d: %w", accepted.Identity.ID, err)
		}
	}
	if err := store.reconcileRestoredSpanWinners(ctx, transaction, publication.Selected); err != nil {
		return err
	}
	selectedList := make([]int64, 0, len(selectedIDs))
	for id := range selectedIDs {
		selectedList = append(selectedList, id)
	}
	placeholders, args := int64Placeholders(selectedList)
	if _, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET phase = 'content_removed' WHERE id = ?`, operationID); err != nil {
		return err
	}
	// The hold clock starts only after every raw and derived row is prepared.
	// This is the final mutation before the atomic publication commit.
	publishedAt := time.Now().UTC()
	if store.retentionNow != nil {
		publishedAt = store.retentionNow().UTC()
	}
	holdUntil := publication.Hold.Until(publishedAt)
	if _, err := transaction.ExecContext(ctx, `UPDATE retained_exports SET hold_until = ?, prior_segment_id = prior_segment_id WHERE id IN (`+placeholders+`)`, append([]any{formatTime(holdUntil)}, args...)...); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit restore publication: %w", err)
	}
	store.signalProjectionChange()
	return nil
}

func (store *Store) reconcileRestoredSpanWinners(ctx context.Context, transaction *sql.Tx, selected []ingest.AcceptedExport) error {
	type spanKey struct{ traceID, spanID string }
	keys := make(map[spanKey]struct{})
	for _, accepted := range selected {
		for _, span := range accepted.Projection.Spans {
			if canonical.IsSemanticSpan(span) {
				keys[spanKey{traceID: span.TraceID, spanID: span.SpanID}] = struct{}{}
			}
		}
	}
	for key := range keys {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM spans WHERE trace_id = ? AND span_id = ?`, key.traceID, key.spanID); err != nil {
			return err
		}
		var exportID, sequence int64
		var projectionJSON []byte
		if err := transaction.QueryRowContext(ctx, `SELECT candidates.export_id, candidates.projection_sequence, candidates.projection_json
FROM span_projection_candidates candidates JOIN retained_exports retained ON retained.id = candidates.export_id
WHERE candidates.trace_id = ? AND candidates.span_id = ? AND retained.state = 'active'
ORDER BY candidates.export_id DESC LIMIT 1`, key.traceID, key.spanID).Scan(&exportID, &sequence, &projectionJSON); err != nil {
			return fmt.Errorf("select stable restored span winner: %w", err)
		}
		var span canonical.Span
		if err := json.Unmarshal(projectionJSON, &span); err != nil {
			return err
		}
		if err := putSpan(ctx, transaction, exportID, span, sequence, store.activityID("span:"+span.TraceID+":"+span.SpanID)); err != nil {
			return err
		}
	}
	if len(keys) == 0 {
		return nil
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM session_links`); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO session_links (source, parent_session_id, child_session_id, observed_at)
SELECT source, parent_session_id, child_session_id, MAX(observed_at) FROM session_link_evidence GROUP BY source, parent_session_id, child_session_id`); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_calls WHERE source = 'claude'`); err != nil {
		return err
	}
	if err := store.rebuildReplayCostProjections(ctx, transaction); err != nil {
		return err
	}
	for _, table := range []string{"session_memberships", "session_agents", "trace_agents"} {
		if _, err := transaction.ExecContext(ctx, `DELETE FROM `+table); err != nil {
			return err
		}
	}
	if _, err := transaction.ExecContext(ctx, sessionAgentAggregateInsert(``, ``)); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, traceAgentInsert(``, ``)); err != nil {
		return err
	}
	if err := rebuildReplaySessionMemberships(ctx, transaction); err != nil {
		return err
	}
	if err := rebuildReplaySessionAggregates(ctx, transaction); err != nil {
		return err
	}
	if err := rebuildReplayTraceAggregates(ctx, transaction); err != nil {
		return err
	}
	_, err := appendProjectionChange(ctx, transaction, []query.ChangeTarget{query.OverviewTarget(), query.AllSourcesTarget(), query.AllSessionsTarget(), query.AllTracesTarget()})
	return err
}

func (store *Store) RetentionOperations(ctx context.Context) ([]retention.Operation, error) {
	rows, err := store.readDB.QueryContext(ctx, `SELECT o.id, o.kind, o.status, o.phase, o.requested_at, o.evaluated_at, o.completed_at, COALESCE(o.error, ''),
 COALESCE(o.affected_export_count, (SELECT COUNT(*) FROM retention_operation_exports targets WHERE targets.operation_id = o.id)),
 COALESCE(o.affected_segment_count, (SELECT COUNT(DISTINCT members.segment_id) FROM retention_operation_exports targets
  LEFT JOIN archive_segment_members members ON members.export_id = targets.export_id WHERE targets.operation_id = o.id))
FROM retention_operations o
WHERE o.status IN ('pending', 'running') OR o.id IN (
 SELECT id FROM retention_operations WHERE status IN ('completed', 'failed', 'cancelled') ORDER BY completed_at DESC, requested_at DESC LIMIT 100
)
ORDER BY CASE WHEN completed_at IS NULL THEN 0 ELSE 1 END, completed_at DESC, requested_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var operations []retention.Operation
	for rows.Next() {
		var operation retention.Operation
		var kind, status, requestedText string
		var evaluated, completed sql.NullString
		if err := rows.Scan(&operation.ID, &kind, &status, &operation.Phase, &requestedText, &evaluated, &completed, &operation.Error, &operation.AffectedExports, &operation.AffectedSegments); err != nil {
			return nil, err
		}
		operation.Kind, operation.Status = retention.OperationKind(kind), retention.OperationStatus(status)
		operation.RequestedAt, err = time.Parse(time.RFC3339Nano, requestedText)
		if err != nil {
			return nil, err
		}
		if evaluated.Valid {
			value, parseErr := time.Parse(time.RFC3339Nano, evaluated.String)
			if parseErr != nil {
				return nil, parseErr
			}
			operation.EvaluatedAt = &value
		}
		if completed.Valid {
			value, parseErr := time.Parse(time.RFC3339Nano, completed.String)
			if parseErr != nil {
				return nil, parseErr
			}
			operation.CompletedAt = &value
		}
		operations = append(operations, operation)
	}
	return operations, rows.Err()
}

func (store *Store) BeginRetentionCycle(ctx context.Context, evaluatedAt time.Time) (string, error) {
	if err := store.lockWrite(ctx); err != nil {
		return "", err
	}
	defer store.unlockWrite()
	var enabled int
	if err := store.db.QueryRowContext(ctx, `SELECT enabled FROM retention_policy WHERE id = 1`).Scan(&enabled); err != nil {
		return "", fmt.Errorf("read retention policy before cycle start: %w", err)
	}
	if enabled == 0 {
		return "", retention.ErrRetentionDisabled
	}
	id, err := store.newActivityID()
	if err != nil {
		return "", err
	}
	startedAt := time.Now().UTC()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO retention_cycles (id, status, started_at, evaluated_at) VALUES (?, 'running', ?, ?)`, id, formatTime(startedAt), formatTime(evaluatedAt)); err != nil {
		return "", err
	}
	var highWater, cohort int64
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0), COUNT(*) FROM retained_exports`).Scan(&highWater, &cohort); err != nil {
		message := "cohort unavailable: " + err.Error()
		_, _ = store.db.ExecContext(context.Background(), `UPDATE retention_cycles SET status = 'failed', completed_at = ?, error = ?, cohort_unavailable_reason = ? WHERE id = ?`, formatTime(time.Now()), message, message, id)
		return "", fmt.Errorf("evaluate retention cycle cohort: %w", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE retention_cycles SET cohort_size = ?, cohort_max_export_id = ? WHERE id = ? AND status = 'running'`, cohort, highWater, id); err != nil {
		message := "cohort unavailable: " + err.Error()
		_, _ = store.db.ExecContext(context.Background(), `UPDATE retention_cycles SET status = 'failed', completed_at = ?, error = ?, cohort_unavailable_reason = ? WHERE id = ?`, formatTime(time.Now()), message, message, id)
		return "", err
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO retention_cycle_segments (cycle_id, segment_id)
SELECT ?, id FROM archive_segments WHERE reference_state = 'current'`, id); err != nil {
		message := "cohort unavailable: " + err.Error()
		_, _ = store.db.ExecContext(context.Background(), `UPDATE retention_cycles SET status = 'failed', completed_at = ?, error = ?, cohort_unavailable_reason = ? WHERE id = ?`, formatTime(time.Now()), message, message, id)
		return "", err
	}
	return id, nil
}

func (store *Store) MarkRetentionCyclePlanned(ctx context.Context, id string, plannedChildren int) error {
	if plannedChildren < 0 {
		return fmt.Errorf("planned child count must not be negative")
	}
	if err := store.lockWrite(ctx); err != nil {
		return err
	}
	defer store.unlockWrite()
	result, err := store.db.ExecContext(ctx, `UPDATE retention_cycles SET planning_completed = 1, planned_children = ? WHERE id = ? AND status = 'running'`, plannedChildren, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("retention cycle %s is not available to complete planning", id)
	}
	return nil
}

func (store *Store) CompleteRetentionCycle(ctx context.Context, id string, cycleErr error) error {
	if err := store.lockWrite(ctx); err != nil {
		return err
	}
	defer store.unlockWrite()
	var planningCompleted bool
	var plannedChildren int
	if err := store.db.QueryRowContext(ctx, `SELECT planning_completed, planned_children FROM retention_cycles WHERE id = ? AND status = 'running'`, id).Scan(&planningCompleted, &plannedChildren); err != nil {
		return err
	}
	if cycleErr == nil && !planningCompleted {
		cycleErr = errors.New("maintenance cohort evaluation did not complete")
	}
	children, counts, err := cycleChildSnapshot(ctx, store.db, id)
	if err != nil {
		return err
	}
	if cycleErr == nil && len(children) != plannedChildren {
		cycleErr = fmt.Errorf("maintenance planned %d child operations but published %d", plannedChildren, len(children))
	}
	aggregate := retention.AggregateCycle(children)
	if aggregate == retention.CycleRunning {
		if cycleErr != nil {
			_, _ = store.db.ExecContext(ctx, `UPDATE retention_cycles SET error = ? WHERE id = ? AND status = 'running'`, cycleErr.Error(), id)
		}
		return nil
	}
	status, message := string(aggregate), any(nil)
	if cycleErr != nil {
		status, message = string(retention.CycleFailed), cycleErr.Error()
	} else if aggregate == retention.CycleFailed {
		message = "one or more lifecycle operations failed"
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `UPDATE retention_cycles SET status = ?, completed_at = ?, error = ?, pending_children = ?, running_children = ?, completed_children = ?, failed_children = ?, cancelled_children = ? WHERE id = ? AND status = 'running'`, status, formatTime(time.Now()), message, counts.pending, counts.running, counts.completed, counts.failed, counts.cancelled, id); err != nil {
		return err
	}
	if err := pruneTerminalRetentionOperations(ctx, transaction); err != nil {
		return err
	}
	return transaction.Commit()
}

type cycleChildCounts struct {
	pending, running, completed, failed, cancelled int64
}

func cycleChildSnapshot(ctx context.Context, reader sqlReader, cycleID string) ([]retention.OperationStatus, cycleChildCounts, error) {
	rows, err := reader.QueryContext(ctx, `SELECT status FROM retention_operations WHERE cycle_id = ? ORDER BY requested_at, id`, cycleID)
	if err != nil {
		return nil, cycleChildCounts{}, err
	}
	defer rows.Close()
	var statuses []retention.OperationStatus
	var counts cycleChildCounts
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, cycleChildCounts{}, err
		}
		status := retention.OperationStatus(value)
		statuses = append(statuses, status)
		switch status {
		case retention.OperationPending:
			counts.pending++
		case retention.OperationRunning:
			counts.running++
		case retention.OperationCompleted:
			counts.completed++
		case retention.OperationFailed:
			counts.failed++
		case retention.OperationCancelled:
			counts.cancelled++
		}
	}
	return statuses, counts, rows.Err()
}

func (store *Store) RetentionCycles(ctx context.Context) ([]retention.Cycle, error) {
	rows, err := store.readDB.QueryContext(ctx, `SELECT c.id, c.status, c.started_at, c.evaluated_at, COALESCE(c.cohort_size, 0), COALESCE(c.cohort_unavailable_reason, ''), c.completed_at, COALESCE(c.error, ''),
 CASE WHEN c.status = 'running' THEN (SELECT COUNT(*) FROM retention_operations o WHERE o.cycle_id = c.id AND o.status = 'pending') ELSE c.pending_children END,
 CASE WHEN c.status = 'running' THEN (SELECT COUNT(*) FROM retention_operations o WHERE o.cycle_id = c.id AND o.status = 'running') ELSE c.running_children END,
 CASE WHEN c.status = 'running' THEN (SELECT COUNT(*) FROM retention_operations o WHERE o.cycle_id = c.id AND o.status = 'completed') ELSE c.completed_children END,
 CASE WHEN c.status = 'running' THEN (SELECT COUNT(*) FROM retention_operations o WHERE o.cycle_id = c.id AND o.status = 'failed') ELSE c.failed_children END,
 CASE WHEN c.status = 'running' THEN (SELECT COUNT(*) FROM retention_operations o WHERE o.cycle_id = c.id AND o.status = 'cancelled') ELSE c.cancelled_children END
FROM retention_cycles c
WHERE c.status = 'running' OR c.id IN (
 SELECT id FROM retention_cycles WHERE status IN ('completed', 'failed', 'cancelled') ORDER BY completed_at DESC, started_at DESC LIMIT 100
)
ORDER BY CASE WHEN completed_at IS NULL THEN 0 ELSE 1 END, completed_at DESC, started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cycles []retention.Cycle
	for rows.Next() {
		var cycle retention.Cycle
		var status, startedText, evaluatedText string
		var completed sql.NullString
		if err := rows.Scan(&cycle.ID, &status, &startedText, &evaluatedText, &cycle.CohortSize, &cycle.CohortUnavailableReason, &completed, &cycle.Error,
			&cycle.PendingChildren, &cycle.RunningChildren, &cycle.CompletedChildren, &cycle.FailedChildren, &cycle.CancelledChildren); err != nil {
			return nil, err
		}
		cycle.Status = retention.CycleStatus(status)
		cycle.StartedAt, err = time.Parse(time.RFC3339Nano, startedText)
		if err != nil {
			return nil, err
		}
		cycle.EvaluatedAt, err = time.Parse(time.RFC3339Nano, evaluatedText)
		if err != nil {
			return nil, err
		}
		if completed.Valid {
			value, parseErr := time.Parse(time.RFC3339Nano, completed.String)
			if parseErr != nil {
				return nil, parseErr
			}
			cycle.CompletedAt = &value
		}
		cycles = append(cycles, cycle)
	}
	return cycles, rows.Err()
}

func (store *Store) EligibleDeletionSegments(ctx context.Context, policy retention.Policy, evaluatedAt time.Time) ([]retention.Segment, error) {
	return store.eligibleDeletionSegments(ctx, policy, evaluatedAt, "")
}

func (store *Store) EligibleDeletionSegmentsForCycle(ctx context.Context, policy retention.Policy, evaluatedAt time.Time, cycleID string) ([]retention.Segment, error) {
	var running int
	if err := store.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM retention_cycles WHERE id = ? AND status = 'running'`, cycleID).Scan(&running); err != nil {
		return nil, err
	}
	if running != 1 {
		return nil, fmt.Errorf("retention cycle %s is not running", cycleID)
	}
	return store.eligibleDeletionSegments(ctx, policy, evaluatedAt, cycleID)
}

func (store *Store) eligibleDeletionSegments(ctx context.Context, policy retention.Policy, evaluatedAt time.Time, cycleID string) ([]retention.Segment, error) {
	if !policy.Enabled {
		return nil, nil
	}
	cutoff := evaluatedAt.UTC().Add(-policy.DeleteAfter.Duration())
	cycleClause := ""
	arguments := []any{formatTime(cutoff), formatTime(cutoff)}
	if cycleID != "" {
		cycleClause = " AND EXISTS (SELECT 1 FROM retention_cycle_segments cohort WHERE cohort.cycle_id = ? AND cohort.segment_id = s.id)"
		arguments = append(arguments, cycleID)
	}
	rows, err := store.readDB.QueryContext(ctx, `SELECT s.id
FROM archive_segments s
WHERE s.reference_state = 'current' AND s.metadata_integrity = 'verifiable' AND s.max_received_at <= ?
  AND NOT EXISTS (
    SELECT 1 FROM current_archive_memberships m JOIN retained_exports r ON r.id = m.export_id
    WHERE m.segment_id = s.id AND (r.state <> 'archived' OR r.received_at > ?)
  )
  AND NOT EXISTS (
    SELECT 1 FROM current_archive_memberships m JOIN retention_export_authorities a ON a.export_id = m.export_id
    WHERE m.segment_id = s.id
  )`+cycleClause+`
ORDER BY s.id`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("select deletion-eligible segments: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byID := make(map[string]retention.Segment)
	after := ""
	for {
		all, next, err := store.ListArchiveSegments(ctx, after, 100)
		if err != nil {
			return nil, err
		}
		for _, segment := range all {
			byID[segment.ID] = segment
		}
		if next == "" {
			break
		}
		after = next
	}
	result := make([]retention.Segment, 0, len(ids))
	for _, id := range ids {
		if segment, ok := byID[id]; ok {
			result = append(result, segment)
		}
	}
	return result, nil
}

// DeleteArchiveSegment owns the serialized point of no return. file is a
// passive durable file capability; it is invoked while writeGate excludes
// restore claims, policy updates, ingestion publication, and other expiry.
func (store *Store) DeleteArchiveSegment(ctx context.Context, segmentID string, policy retention.Policy, evaluatedAt time.Time, cycleID string, file retention.DeletionHandle) (returnErr error) {
	if !policy.Enabled {
		return nil
	}
	if err := store.lockWrite(ctx); err != nil {
		return fmt.Errorf("wait for archive deletion writer: %w", err)
	}
	locked := true
	defer func() {
		if locked {
			store.unlockWrite()
		}
	}()
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	var enabled int
	var deleteDays, revision int64
	var state, metadata string
	if err := transaction.QueryRowContext(ctx, `SELECT p.enabled, COALESCE(p.delete_days, 0), p.revision, s.reference_state, s.metadata_integrity
FROM retention_policy p CROSS JOIN archive_segments s WHERE p.id = 1 AND s.id = ?`, segmentID).Scan(&enabled, &deleteDays, &revision, &state, &metadata); err != nil {
		return err
	}
	if enabled == 0 || revision != policy.Revision || state != "current" || metadata != "verifiable" {
		return fmt.Errorf("archive deletion authority changed")
	}
	if cycleID != "" {
		var inCohort int
		if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM retention_cycle_segments WHERE cycle_id = ? AND segment_id = ?`, cycleID, segmentID).Scan(&inCohort); err != nil || inCohort != 1 {
			return fmt.Errorf("archive segment was not current when retention cycle %s started", cycleID)
		}
	}
	cutoff := evaluatedAt.UTC().Add(-time.Duration(deleteDays) * retention.Day)
	var ineligible int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM current_archive_memberships m
JOIN retained_exports r ON r.id = m.export_id
WHERE m.segment_id = ? AND (r.state <> 'archived' OR r.received_at > ? OR EXISTS (
 SELECT 1 FROM retention_export_authorities a WHERE a.export_id = r.id
))`, segmentID, formatTime(cutoff)).Scan(&ineligible); err != nil {
		return err
	}
	if ineligible != 0 {
		return fmt.Errorf("archive segment is no longer deletion eligible")
	}
	operationID, err := store.newActivityID()
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO retention_operations (
 id, cycle_id, kind, status, phase, requested_at, evaluated_at, staging_token, decision_policy_revision, decision_cutoff_days, affected_segment_count
) VALUES (?, ?, 'delete', 'running', 'staging', ?, ?, ?, ?, ?, 1)`, operationID, nullableString(cycleID), formatTime(evaluatedAt), formatTime(evaluatedAt), segmentID, policy.Revision, int(policy.DeleteAfter)); err != nil {
		return err
	}
	rows, err := transaction.QueryContext(ctx, `SELECT export_id FROM current_archive_memberships WHERE segment_id = ? ORDER BY ordinal`, segmentID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET affected_export_count = ? WHERE id = ?`, len(ids), operationID); err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO retention_operation_exports (operation_id, export_id, role) VALUES (?, ?, 'affected')`, operationID, id); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO retention_export_authorities (export_id, operation_id, kind, role) VALUES (?, ?, 'delete', 'affected')`, id, operationID); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit deletion staging intent: %w", err)
	}
	defer func() {
		if returnErr == nil {
			return
		}
		recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if !locked {
			if err := store.lockWrite(recoveryCtx); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("reacquire writer for deletion recovery: %w", err))
				return
			}
			locked = true
		}
		var recoveryErr error
		for recoveryCtx.Err() == nil {
			recoveryErr = store.recoverDeletionOperation(recoveryCtx, operationID, segmentID, file)
			if recoveryErr == nil {
				return
			}
			_, _ = store.db.ExecContext(recoveryCtx, `UPDATE retention_operations SET error = ? WHERE id = ? AND status = 'running'`, recoveryErr.Error(), operationID)
			select {
			case <-time.After(25 * time.Millisecond):
			case <-recoveryCtx.Done():
			}
		}
		returnErr = errors.Join(returnErr, fmt.Errorf("converge deletion operation: %w", recoveryErr))
	}()
	stageErr := file.StageAndSync()
	presence, inspectErr := file.Inspect()
	if inspectErr != nil {
		return errors.Join(stageErr, inspectErr)
	}
	if presence == retention.DeletionInstalled {
		failure, beginErr := store.db.BeginTx(ctx, nil)
		if beginErr != nil {
			return errors.Join(stageErr, beginErr)
		}
		defer func() { _ = failure.Rollback() }()
		_, _ = failure.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id = ?`, operationID)
		message := "archive deletion staging did not move the file"
		if stageErr != nil {
			message = stageErr.Error()
		}
		_, _ = failure.ExecContext(ctx, `UPDATE retention_operations SET status = 'failed', phase = 'terminal', completed_at = ?, error = ? WHERE id = ?`, formatTime(time.Now()), message, operationID)
		if err := failure.Commit(); err != nil {
			return errors.Join(stageErr, err)
		}
		if stageErr != nil {
			return stageErr
		}
		return fmt.Errorf("archive deletion staging left installed file")
	}
	if presence != retention.DeletionStaged {
		return errors.Join(stageErr, fmt.Errorf("archive deletion staging has ambiguous missing content"))
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE retention_operations SET phase = 'staged', error = ? WHERE id = ? AND status = 'running' AND phase = 'staging'`, errorText(stageErr), operationID); err != nil {
		return fmt.Errorf("publish staged archive deletion: %w", err)
	}
	// Staging is reversible. Yield the shared boundary so an already-arriving
	// restore can reinstall the file, cancel this operation, and claim the full
	// affected set before physical deletion becomes irreversible.
	store.unlockWrite()
	locked = false
	if err := store.lockWrite(ctx); err != nil {
		return fmt.Errorf("reacquire deletion cutover writer: %w", err)
	}
	locked = true
	var currentStatus, currentPhase string
	if err := store.db.QueryRowContext(ctx, `SELECT status, phase FROM retention_operations WHERE id = ?`, operationID).Scan(&currentStatus, &currentPhase); err != nil {
		return err
	}
	if currentStatus == "cancelled" {
		return nil
	}
	if currentStatus != "running" || currentPhase != "staged" {
		return fmt.Errorf("deletion operation %s changed before cutover", operationID)
	}
	var currentEnabled int
	var currentRevision int64
	if err := store.db.QueryRowContext(ctx, `SELECT enabled, revision FROM retention_policy WHERE id = 1`).Scan(&currentEnabled, &currentRevision); err != nil {
		return err
	}
	var authorityCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM retention_export_authorities WHERE operation_id = ? AND kind = 'delete'`, operationID).Scan(&authorityCount); err != nil {
		return err
	}
	inCycle := 1
	if cycleID != "" {
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM retention_cycle_segments WHERE cycle_id = ? AND segment_id = ?`, cycleID, segmentID).Scan(&inCycle); err != nil {
			return err
		}
	}
	if currentEnabled == 0 || currentRevision != policy.Revision || authorityCount != len(ids) || inCycle != 1 {
		if err := file.RestoreAndSync(); err != nil {
			return fmt.Errorf("restore staged content after deletion revalidation failed: %w", err)
		}
		presence, err := file.Inspect()
		if err != nil || presence != retention.DeletionInstalled {
			return errors.Join(err, fmt.Errorf("cannot release deletion authority without installed archive content"))
		}
		cancel, err := store.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		_, _ = cancel.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id = ?`, operationID)
		_, _ = cancel.ExecContext(ctx, `UPDATE retention_operations SET status = 'cancelled', phase = 'terminal', completed_at = ?, error = 'deletion authority or policy changed before cutover' WHERE id = ?`, formatTime(time.Now()), operationID)
		if err := cancel.Commit(); err != nil {
			return err
		}
		return nil
	}
	// This durable transition is the point of no return. From here recovery
	// must finish deletion and must never return authority to Archived content.
	if _, err := store.db.ExecContext(ctx, `UPDATE retention_operations SET phase = 'delete_committing' WHERE id = ? AND status = 'running' AND phase = 'staged'`, operationID); err != nil {
		return fmt.Errorf("commit deletion point of no return: %w", err)
	}
	removeErr := file.RemoveAndSync()
	presence, inspectErr = file.Inspect()
	if inspectErr != nil {
		return errors.Join(removeErr, inspectErr)
	}
	if presence != retention.DeletionMissing {
		return errors.Join(removeErr, fmt.Errorf("staged archive content remains; deletion will resume during recovery"))
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE retention_operations SET phase = 'content_removed', error = ? WHERE id = ? AND status = 'running' AND phase = 'delete_committing'`, errorText(removeErr), operationID); err != nil {
		return fmt.Errorf("publish removed archive content: %w", err)
	}
	terminal, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin deletion terminal publication: %w", err)
	}
	defer func() { _ = terminal.Rollback() }()
	placeholders, args := int64Placeholders(ids)
	updateArgs := []any{formatTime(time.Now()), policy.Revision, int(policy.DeleteAfter), formatTime(evaluatedAt), operationID}
	updateArgs = append(updateArgs, args...)
	if _, err := terminal.ExecContext(ctx, `UPDATE retained_exports SET state = 'deleted', hold_until = NULL,
 deleted_at = ?, deletion_policy_revision = ?, deletion_cutoff_days = ?, deletion_evaluated_at = ?, deletion_operation_id = ?
WHERE id IN (`+placeholders+`)`, updateArgs...); err != nil {
		return err
	}
	if _, err := terminal.ExecContext(ctx, `DELETE FROM current_archive_memberships WHERE segment_id = ?`, segmentID); err != nil {
		return err
	}
	if _, err := terminal.ExecContext(ctx, `UPDATE archive_segments SET reference_state = 'deleted', file_name = NULL, deletion_completed_at = ? WHERE id = ?`, formatTime(time.Now()), segmentID); err != nil {
		return err
	}
	if _, err := terminal.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id = ?`, operationID); err != nil {
		return err
	}
	if _, err := terminal.ExecContext(ctx, `UPDATE retention_operations SET status = 'completed', phase = 'terminal', completed_at = ?, error = NULL WHERE id = ?`, formatTime(time.Now()), operationID); err != nil {
		return err
	}
	if err := pruneTerminalRetentionOperations(ctx, terminal); err != nil {
		return err
	}
	if err := terminal.Commit(); err != nil {
		return fmt.Errorf("commit deletion terminal publication: %w", err)
	}
	return nil
}

func errorText(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}

func pruneTerminalRetentionOperations(ctx context.Context, transaction *sql.Tx) error {
	_, err := transaction.ExecContext(ctx, `DELETE FROM retention_operations
WHERE status IN ('completed', 'failed', 'cancelled')
AND (cycle_id IS NULL OR cycle_id NOT IN (SELECT id FROM retention_cycles WHERE status = 'running'))
AND id NOT IN (
 SELECT id FROM retention_operations WHERE status IN ('completed', 'failed', 'cancelled')
 ORDER BY completed_at DESC, requested_at DESC LIMIT 100
)`)
	if err != nil {
		return fmt.Errorf("prune terminal retention operations: %w", err)
	}
	return nil
}

func (store *Store) RetentionCapacity(ctx context.Context, observedAt time.Time) (retention.CapacityReport, error) {
	report := retention.CapacityReport{ObservedAt: observedAt.UTC()}
	var pageCount, freePages, pageSize int64
	if err := store.readDB.QueryRowContext(ctx, `SELECT (SELECT page_count FROM pragma_page_count),
 (SELECT freelist_count FROM pragma_freelist_count), (SELECT page_size FROM pragma_page_size)`).Scan(&pageCount, &freePages, &pageSize); err != nil {
		return report, fmt.Errorf("measure SQLite capacity: %w", err)
	}
	report.DatabaseBytes = pageCount * pageSize
	report.DatabaseUnusedBytes = freePages * pageSize
	if allocated, err := allocatedFileBytes(store.path); err == nil {
		report.DatabaseBytes = allocated
		report.TotalAllocatedBytes += allocated
	} else if !errors.Is(err, os.ErrNotExist) {
		report.UnavailableReason = "database allocation: " + err.Error()
	}
	if allocated, err := allocatedFileBytes(store.path + "-wal"); err == nil {
		report.WALBytes = allocated
		report.TotalAllocatedBytes += allocated
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, err
	}
	if allocated, err := allocatedFileBytes(store.path + "-shm"); err == nil {
		report.TotalAllocatedBytes += allocated
	} else if !errors.Is(err, os.ErrNotExist) {
		return report, err
	}
	pageClasses := []struct {
		tables []string
		target *int64
	}{
		{tables: []string{"otlp_exports"}, target: &report.ActiveRawBytes},
		{tables: []string{"observations"}, target: &report.ObservationBytes},
		{tables: []string{"spans", "logs", "metrics", "session_rollups", "session_links", "session_memberships", "session_agents", "session_traces", "trace_rollups", "trace_conversations", "trace_agents", "projection_feed_state", "projection_changes", "activity_changes", "plan_usage_snapshots", "model_rates", "model_calls", "model_call_evidence", "model_call_evidence_aliases", "model_call_attributions", "model_call_activity_links", "model_call_trace_memberships", "model_call_trace_supports", "span_projection_candidates", "session_link_evidence"}, target: &report.QueryProjectionBytes},
	}
	for _, class := range pageClasses {
		placeholders := make([]string, len(class.tables))
		arguments := make([]any, len(class.tables))
		for index, table := range class.tables {
			placeholders[index], arguments[index] = "?", table
		}
		if err := store.readDB.QueryRowContext(ctx, `SELECT COALESCE(SUM(pgsize), 0) FROM dbstat
WHERE name IN (SELECT name FROM sqlite_schema WHERE tbl_name IN (`+strings.Join(placeholders, ",")+`))`, arguments...).Scan(class.target); err != nil {
			return report, fmt.Errorf("measure SQLite page class: %w", err)
		}
	}
	if err := store.readDB.QueryRowContext(ctx, `SELECT COALESCE(SUM(stored_bytes), 0) FROM archive_segments WHERE reference_state = 'current'`).Scan(&report.ArchiveBytes); err != nil {
		return report, err
	}
	currentArchiveFiles := make(map[string]struct{})
	currentRows, err := store.readDB.QueryContext(ctx, `SELECT file_name FROM archive_segments WHERE reference_state = 'current' AND file_name IS NOT NULL`)
	if err != nil {
		return report, err
	}
	for currentRows.Next() {
		var name string
		if err := currentRows.Scan(&name); err != nil {
			_ = currentRows.Close()
			return report, err
		}
		currentArchiveFiles[name] = struct{}{}
	}
	if err := currentRows.Close(); err != nil {
		return report, err
	}
	if err := filepath.WalkDir(store.ArchiveDirectory(), func(path string, entry os.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		allocated, err := allocatedFileBytes(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(entry.Name(), ".deleting") || strings.HasSuffix(entry.Name(), ".candidate") {
			report.StagingAllocatedBytes += allocated
		} else if strings.HasSuffix(entry.Name(), ".tar.zst") {
			if _, current := currentArchiveFiles[entry.Name()]; current {
				report.ArchiveAllocatedBytes += allocated
			} else {
				report.StagingAllocatedBytes += allocated
			}
		}
		report.TotalAllocatedBytes += allocated
		return nil
	}); err != nil && !errors.Is(err, os.ErrNotExist) {
		return report, fmt.Errorf("measure archive files: %w", err)
	}
	free, err := filesystemFreeBytes(filepath.Dir(store.path))
	if err != nil {
		report.UnavailableReason = err.Error()
	} else {
		report.FilesystemAvailable, report.FilesystemFreeBytes = true, free
	}
	var enabled int
	var archiveDays int64
	if err := store.readDB.QueryRowContext(ctx, `SELECT enabled, COALESCE(archive_days, 0) FROM retention_policy WHERE id = 1`).Scan(&enabled, &archiveDays); err != nil {
		return report, err
	}
	if enabled != 0 {
		cutoff := observedAt.UTC().Add(-time.Duration(archiveDays) * retention.Day)
		if err := store.readDB.QueryRowContext(ctx, `SELECT COALESCE(SUM(e.payload_size), 0) FROM otlp_exports e JOIN retained_exports r ON r.id = e.id
WHERE r.state = 'active' AND r.received_at <= ? AND (r.hold_until IS NULL OR r.hold_until <= ?)`, formatTime(cutoff), formatTime(observedAt)).Scan(&report.EstimatedArchivePeakBytes); err != nil {
			return report, err
		}
		report.EstimatedArchivePeakBytes = min(report.EstimatedArchivePeakBytes, retention.MaxSegmentOriginalBytes) * 2
	}
	if err := store.readDB.QueryRowContext(ctx, `SELECT COALESCE(SUM(original_bytes * 2), 0) FROM archive_segments WHERE reference_state = 'current' AND payload_integrity = 'intact'`).Scan(&report.EstimatedRestorePeakBytes); err != nil {
		return report, err
	}
	peak := max(report.EstimatedArchivePeakBytes, report.EstimatedRestorePeakBytes)
	report.Warning = retentionCapacityWarning(peak, report.FilesystemFreeBytes, report.FilesystemAvailable)
	return report, nil
}

func retentionCapacityWarning(peak, available int64, filesystemAvailable bool) string {
	if filesystemAvailable && peak > available {
		return fmt.Sprintf("estimated additional peak allocation %d exceeds filesystem availability %d", peak, available)
	}
	return ""
}

type pendingDeletion struct {
	operationID, segmentID, phase, evaluatedAt string
	policyRevision, cutoffDays                 int64
	cycleID                                    sql.NullString
}

// recoverRetentionLifecycle runs before query services open. Before the point
// of no return a staged file is put back. At and after delete_committing the
// authority is retained until content removal and terminal evidence converge.
func (store *Store) recoverRetentionLifecycle(ctx context.Context) error {
	archiveRecovery, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := archiveRecovery.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id IN (
 SELECT id FROM retention_operations WHERE status = 'running' AND kind = 'archive' AND phase IN ('planned', 'file_ready')
)`); err != nil {
		_ = archiveRecovery.Rollback()
		return fmt.Errorf("release interrupted archive authority: %w", err)
	}
	if _, err := archiveRecovery.ExecContext(ctx, `UPDATE retention_operations SET status = 'failed', phase = 'terminal', completed_at = ?, error = 'archive interrupted before atomic publication'
WHERE status = 'running' AND kind = 'archive' AND phase IN ('planned', 'file_ready')`, formatTime(time.Now())); err != nil {
		_ = archiveRecovery.Rollback()
		return fmt.Errorf("record interrupted archive: %w", err)
	}
	if err := archiveRecovery.Commit(); err != nil {
		return err
	}
	// Restore preparation is intentionally non-durable. A crashed process has
	// published no Active rows, so releasing the claim and recording failure is
	// the only truthful recovery.
	restorePreparationRecovery, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := restorePreparationRecovery.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id IN (
 SELECT id FROM retention_operations WHERE status = 'running' AND kind = 'restore' AND phase IN ('planned', 'file_ready')
)`); err != nil {
		_ = restorePreparationRecovery.Rollback()
		return fmt.Errorf("release interrupted restore authority: %w", err)
	}
	if _, err := restorePreparationRecovery.ExecContext(ctx, `UPDATE retention_operations SET status = 'failed', phase = 'terminal', completed_at = ?, error = 'restore interrupted before atomic publication'
WHERE status = 'running' AND kind = 'restore' AND phase IN ('planned', 'file_ready')`, formatTime(time.Now())); err != nil {
		_ = restorePreparationRecovery.Rollback()
		return fmt.Errorf("record interrupted restore: %w", err)
	}
	if err := restorePreparationRecovery.Commit(); err != nil {
		return err
	}
	operations, err := store.runningDeletionOperations(ctx)
	if err != nil {
		return err
	}
	if err := store.completeRecoverableDeletionCycles(ctx); err != nil {
		return err
	}
	for _, operation := range operations {
		file, err := store.deletionFile(operation.segmentID)
		if err != nil {
			return err
		}
		if err := store.recoverDeletionOperationWithState(ctx, operation, file); err != nil {
			return err
		}
	}
	if err := store.removeUnreferencedArchiveFiles(ctx); err != nil {
		return err
	}
	restoreCleanupRecovery, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := restoreCleanupRecovery.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id IN (
 SELECT id FROM retention_operations WHERE status = 'running' AND kind = 'restore' AND phase = 'content_removed'
)`); err != nil {
		_ = restoreCleanupRecovery.Rollback()
		return fmt.Errorf("release recovered restore authority: %w", err)
	}
	if _, err := restoreCleanupRecovery.ExecContext(ctx, `UPDATE retention_operations SET status = 'completed', phase = 'terminal', completed_at = ?, error = NULL
WHERE status = 'running' AND kind = 'restore' AND phase = 'content_removed'`, formatTime(time.Now())); err != nil {
		_ = restoreCleanupRecovery.Rollback()
		return fmt.Errorf("complete recovered restore cleanup: %w", err)
	}
	if err := restoreCleanupRecovery.Commit(); err != nil {
		return err
	}
	if err := store.completeRecoveredCycles(ctx); err != nil {
		return err
	}
	return nil
}

// ResumeRetentionDeletions retries durable deletion work left running by an
// earlier maintenance attempt. It also rediscovers planned parents whose
// children became terminal before the parent aggregation could commit.
func (store *Store) ResumeRetentionDeletions(ctx context.Context) error {
	if err := store.lockWrite(ctx); err != nil {
		return err
	}
	defer store.unlockWrite()
	operations, err := store.runningDeletionOperations(ctx)
	if err != nil {
		return err
	}
	if err := store.completeRecoverableDeletionCycles(ctx); err != nil {
		return err
	}
	for _, operation := range operations {
		file, err := store.deletionFile(operation.segmentID)
		if err != nil {
			return err
		}
		if err := store.recoverDeletionOperationWithState(ctx, operation, file); err != nil {
			_, _ = store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE retention_operations SET error = ? WHERE id = ? AND status = 'running'`, err.Error(), operation.operationID)
			return err
		}
		if operation.cycleID.Valid {
			if err := store.completeRecoveredCycleByID(ctx, operation.cycleID.String); err != nil {
				return err
			}
		}
	}
	return nil
}

func (store *Store) completeRecoverableDeletionCycles(ctx context.Context) error {
	rows, err := store.db.QueryContext(ctx, `SELECT c.id
FROM retention_cycles c
WHERE c.status = 'running'
  AND c.planning_completed = 1
  AND c.planned_children = (SELECT COUNT(*) FROM retention_operations o WHERE o.cycle_id = c.id)
  AND EXISTS (SELECT 1 FROM retention_operations o WHERE o.cycle_id = c.id AND o.kind = 'delete')
  AND NOT EXISTS (SELECT 1 FROM retention_operations o WHERE o.cycle_id = c.id AND o.status IN ('pending', 'running'))
ORDER BY c.started_at, c.id`)
	if err != nil {
		return err
	}
	var cycleIDs []string
	for rows.Next() {
		var cycleID string
		if err := rows.Scan(&cycleID); err != nil {
			_ = rows.Close()
			return err
		}
		cycleIDs = append(cycleIDs, cycleID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, cycleID := range cycleIDs {
		if err := store.completeRecoveredCycleByID(ctx, cycleID); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) runningDeletionOperations(ctx context.Context) ([]pendingDeletion, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT id, staging_token, phase, evaluated_at,
 cycle_id,
 COALESCE(decision_policy_revision, 0), COALESCE(decision_cutoff_days, 0)
FROM retention_operations WHERE status = 'running' AND kind = 'delete' ORDER BY requested_at`)
	if err != nil {
		return nil, fmt.Errorf("inspect retention recovery: %w", err)
	}
	defer rows.Close()
	var operations []pendingDeletion
	for rows.Next() {
		var item pendingDeletion
		if err := rows.Scan(&item.operationID, &item.segmentID, &item.phase, &item.evaluatedAt, &item.cycleID, &item.policyRevision, &item.cutoffDays); err != nil {
			return nil, err
		}
		operations = append(operations, item)
	}
	return operations, rows.Err()
}

type pendingRetentionCycle struct {
	id         string
	highWater  sql.NullInt64
	planned    bool
	children   int
	priorError sql.NullString
}

func (store *Store) completeRecoveredCycles(ctx context.Context) error {
	rows, err := store.db.QueryContext(ctx, `SELECT id, cohort_max_export_id, planning_completed, planned_children, error
FROM retention_cycles WHERE status = 'running' ORDER BY started_at`)
	if err != nil {
		return err
	}
	var cycles []pendingRetentionCycle
	for rows.Next() {
		var cycle pendingRetentionCycle
		if err := rows.Scan(&cycle.id, &cycle.highWater, &cycle.planned, &cycle.children, &cycle.priorError); err != nil {
			_ = rows.Close()
			return err
		}
		cycles = append(cycles, cycle)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, cycle := range cycles {
		if err := store.completeRecoveredCycle(ctx, cycle); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) completeRecoveredCycleByID(ctx context.Context, cycleID string) error {
	var cycle pendingRetentionCycle
	cycle.id = cycleID
	err := store.db.QueryRowContext(ctx, `SELECT cohort_max_export_id, planning_completed, planned_children, error
FROM retention_cycles WHERE id = ? AND status = 'running'`, cycleID).Scan(&cycle.highWater, &cycle.planned, &cycle.children, &cycle.priorError)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return store.completeRecoveredCycle(ctx, cycle)
}

func (store *Store) completeRecoveredCycle(ctx context.Context, cycle pendingRetentionCycle) error {
	children, counts, err := cycleChildSnapshot(ctx, store.db, cycle.id)
	if err != nil {
		return err
	}
	status := retention.AggregateCycle(children)
	message := any(nil)
	if !cycle.highWater.Valid {
		status = retention.CycleFailed
		message = "maintenance interrupted before cohort evaluation"
	} else if !cycle.planned {
		status = retention.CycleFailed
		message = "maintenance interrupted before cohort evaluation completed"
	} else if len(children) != cycle.children {
		status = retention.CycleFailed
		message = fmt.Sprintf("maintenance planned %d child operations but published %d before interruption", cycle.children, len(children))
	} else if status == retention.CycleRunning {
		return nil
	} else if cycle.priorError.Valid && cycle.priorError.String != "" {
		status = retention.CycleFailed
		message = cycle.priorError.String
	} else if status == retention.CycleFailed {
		message = "one or more lifecycle operations failed"
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `UPDATE retention_cycles SET status = ?, completed_at = ?, error = ?, pending_children = ?, running_children = ?, completed_children = ?, failed_children = ?, cancelled_children = ? WHERE id = ? AND status = 'running'`, status, formatTime(time.Now()), message, counts.pending, counts.running, counts.completed, counts.failed, counts.cancelled, cycle.id); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) recoverDeletionOperation(ctx context.Context, operationID, segmentID string, file retention.DeletionHandle) error {
	var operation pendingDeletion
	operation.operationID = operationID
	operation.segmentID = segmentID
	var status string
	if err := store.db.QueryRowContext(ctx, `SELECT status, phase, evaluated_at,
 COALESCE(decision_policy_revision, 0), COALESCE(decision_cutoff_days, 0)
FROM retention_operations WHERE id = ? AND kind = 'delete'`, operationID).Scan(
		&status, &operation.phase, &operation.evaluatedAt, &operation.policyRevision, &operation.cutoffDays,
	); err != nil {
		return err
	}
	if status != "running" {
		return nil
	}
	return store.recoverDeletionOperationWithState(ctx, operation, file)
}

func (store *Store) recoverDeletionOperationWithState(ctx context.Context, operation pendingDeletion, file retention.DeletionHandle) error {
	presence, err := file.Inspect()
	if err != nil {
		return err
	}
	switch operation.phase {
	case "staging", "staged":
		if presence == retention.DeletionStaged {
			if err := file.RestoreAndSync(); err != nil {
				return fmt.Errorf("restore staged archive during recovery: %w", err)
			}
			presence, err = file.Inspect()
			if err != nil {
				return err
			}
		}
		if presence != retention.DeletionInstalled {
			return fmt.Errorf("recover pre-cutover deletion %s: archive content is missing", operation.operationID)
		}
		transaction, err := store.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = transaction.Rollback() }()
		if _, err := transaction.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id = ?`, operation.operationID); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET status = 'cancelled', phase = 'terminal', completed_at = ?, error = 'recovered before deletion cutover' WHERE id = ? AND status = 'running'`, formatTime(time.Now()), operation.operationID); err != nil {
			return err
		}
		return transaction.Commit()
	case "delete_committing", "content_removed":
		if operation.policyRevision <= 0 || operation.cutoffDays <= 0 {
			return fmt.Errorf("recover deletion %s: decision evidence is missing", operation.operationID)
		}
		if presence == retention.DeletionInstalled {
			stageErr := file.StageAndSync()
			presence, err = file.Inspect()
			if err != nil || presence != retention.DeletionStaged {
				return errors.Join(stageErr, err, fmt.Errorf("resume deletion staging did not converge"))
			}
		}
		if presence == retention.DeletionStaged {
			removeErr := file.RemoveAndSync()
			presence, err = file.Inspect()
			if err != nil || presence != retention.DeletionMissing {
				return errors.Join(removeErr, err, fmt.Errorf("resume deletion left staged content"))
			}
		}
		if presence != retention.DeletionMissing {
			return fmt.Errorf("resume deletion has unsupported file presence %s", presence)
		}
		if _, err := store.db.ExecContext(ctx, `UPDATE retention_operations SET phase = 'content_removed' WHERE id = ? AND status = 'running'`, operation.operationID); err != nil {
			return err
		}
		return store.completeRecoveredDeletion(ctx, operation.operationID, operation.segmentID, operation.evaluatedAt, operation.policyRevision, operation.cutoffDays)
	default:
		return fmt.Errorf("recover deletion %s: unsupported phase %s", operation.operationID, operation.phase)
	}
}

// RecoverRetentionLifecycleOwned converges lifecycle state while the caller
// owns the database replacement lock. It is used by compaction before taking
// its source snapshot.
func RecoverRetentionLifecycleOwned(ctx context.Context, path string) error {
	database, err := sql.Open("sqlite", sqliteDSN(path, false))
	if err != nil {
		return err
	}
	defer database.Close()
	var exists int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = 'retention_operations'`).Scan(&exists); err != nil || exists == 0 {
		return err
	}
	store := &Store{path: path, db: database, deletionFile: archiveDeletionResolver(path)}
	return store.recoverRetentionLifecycle(ctx)
}

func (store *Store) completeRecoveredDeletion(ctx context.Context, operationID, segmentID, evaluatedAt string, policyRevision, cutoffDays int64) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	completedAt := formatTime(time.Now())
	if _, err := transaction.ExecContext(ctx, `UPDATE retained_exports SET state = 'deleted', hold_until = NULL, deleted_at = ?,
 deletion_policy_revision = ?, deletion_cutoff_days = ?, deletion_evaluated_at = ?, deletion_operation_id = ?
WHERE id IN (SELECT export_id FROM retention_operation_exports WHERE operation_id = ?)`, completedAt, policyRevision, cutoffDays, evaluatedAt, operationID, operationID); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM current_archive_memberships WHERE segment_id = ?`, segmentID); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE archive_segments SET reference_state = 'deleted', file_name = NULL, deletion_completed_at = ? WHERE id = ?`, completedAt, segmentID); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM retention_export_authorities WHERE operation_id = ?`, operationID); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE retention_operations SET status = 'completed', phase = 'terminal', completed_at = ?, error = NULL WHERE id = ?`, completedAt, operationID); err != nil {
		return err
	}
	return transaction.Commit()
}

func (store *Store) removeUnreferencedArchiveFiles(ctx context.Context) error {
	entries, err := os.ReadDir(store.ArchiveDirectory())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect archive recovery directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".segment-") && strings.HasSuffix(name, ".candidate") {
			if err := os.Remove(filepath.Join(store.ArchiveDirectory(), name)); err != nil {
				return err
			}
			continue
		}
		if !strings.HasSuffix(name, ".tar.zst") {
			continue
		}
		var references int
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM archive_segments WHERE reference_state = 'current' AND file_name = ?`, name).Scan(&references); err != nil {
			return err
		}
		if references == 0 {
			if err := os.Remove(filepath.Join(store.ArchiveDirectory(), name)); err != nil {
				return err
			}
		}
	}
	return nil
}
