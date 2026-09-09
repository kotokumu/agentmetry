package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// rebuildCodexCorroboratingSupports derives trace membership from retained
// Codex evidence. Exact usage IDs are only accepted while they identify one
// billable call in the native session; later ambiguity retracts the support.
func rebuildCodexCorroboratingSupports(ctx context.Context, transaction *sql.Tx, sequence int64, sessionID string) error {
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_call_activity_links
WHERE evidence_role = 'corroborating' AND call_id IN (
  SELECT call_id FROM model_calls WHERE source = 'codex' AND native_session_id = ?
)`, sessionID); err != nil {
		return fmt.Errorf("clear Codex corroborating activity links: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_call_trace_supports
WHERE support_kind = 'corroborating' AND call_id IN (
  SELECT call_id FROM model_calls WHERE source = 'codex' AND native_session_id = ?
)`, sessionID); err != nil {
		return fmt.Errorf("clear Codex corroborating trace supports: %w", err)
	}
	const candidates = `WITH identities AS (
  SELECT l.run_id, l.usage_id, MIN(links.call_id) AS call_id
  FROM logs l
  JOIN model_call_activity_links links ON links.activity_id = l.activity_id
  WHERE l.source = 'codex' AND l.run_id = ? AND l.usage_id <> ''
  GROUP BY l.run_id, l.usage_id HAVING COUNT(DISTINCT links.call_id) = 1
), unique_candidates AS (
  SELECT s.activity_id, MIN(s.trace_id) AS trace_id, MIN(identities.call_id) AS call_id
  FROM spans s JOIN identities USING (run_id, usage_id)
  WHERE s.source = 'codex'
    AND COALESCE(json_extract(s.attributes_json, '$."gen_ai.usage.role"'), '') = 'corroborating'
  GROUP BY s.activity_id
)
`
	if _, err := transaction.ExecContext(ctx, candidates+`INSERT INTO model_call_activity_links (activity_id, call_id, evidence_role)
SELECT activity_id, call_id, 'corroborating' FROM unique_candidates`, sessionID); err != nil {
		return fmt.Errorf("project Codex corroborating links: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO model_call_trace_supports (call_id, trace_id, activity_id, support_kind)
SELECT links.call_id, spans.trace_id, spans.activity_id, 'corroborating'
FROM spans JOIN model_call_activity_links links USING (activity_id)
WHERE spans.source = 'codex' AND spans.run_id = ? AND spans.trace_id <> ''
  AND links.evidence_role = 'corroborating'`, sessionID); err != nil {
		return fmt.Errorf("project Codex corroborating trace supports: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_call_trace_memberships
WHERE call_id IN (SELECT call_id FROM model_calls WHERE source = 'codex' AND native_session_id = ?)`, sessionID); err != nil {
		return fmt.Errorf("replace Codex trace memberships: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO model_call_trace_memberships (call_id, trace_id)
SELECT DISTINCT call_id, trace_id FROM model_call_trace_supports
WHERE call_id IN (SELECT call_id FROM model_calls WHERE source = 'codex' AND native_session_id = ?)`, sessionID); err != nil {
		return fmt.Errorf("derive Codex trace memberships: %w", err)
	}
	return publishCorroboratingSpanChanges(ctx, transaction, sequence, "codex", sessionID)
}

// rebuildAllCodexCorroboratingSupports derives Codex trace membership from a
// complete replay snapshot. Unlike live ingestion, replay has no affected-run
// boundary, so set-wide statements avoid repeating the same scans per session.
func rebuildAllCodexCorroboratingSupports(ctx context.Context, transaction *sql.Tx) error {
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_call_activity_links
WHERE evidence_role = 'corroborating' AND call_id IN (
  SELECT call_id FROM model_calls WHERE source = 'codex'
)`); err != nil {
		return fmt.Errorf("clear replay Codex corroborating activity links: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_call_trace_supports
WHERE support_kind = 'corroborating' AND call_id IN (
  SELECT call_id FROM model_calls WHERE source = 'codex'
)`); err != nil {
		return fmt.Errorf("clear replay Codex corroborating trace supports: %w", err)
	}
	const candidates = `WITH identities AS (
  SELECT l.run_id, l.usage_id, MIN(links.call_id) AS call_id
  FROM model_calls calls INDEXED BY model_calls_session_filter_idx
  CROSS JOIN model_call_activity_links links INDEXED BY model_call_activity_call_idx
  CROSS JOIN logs l INDEXED BY logs_activity_id_idx
  WHERE calls.source = 'codex' AND links.call_id = calls.call_id
    AND l.activity_id = links.activity_id AND l.source = 'codex'
    AND l.run_id = calls.native_session_id AND l.usage_id <> ''
  GROUP BY l.run_id, l.usage_id HAVING COUNT(DISTINCT links.call_id) = 1
)
`
	if _, err := transaction.ExecContext(ctx, candidates+`INSERT INTO model_call_activity_links (activity_id, call_id, evidence_role)
SELECT spans.activity_id, identities.call_id, 'corroborating'
FROM identities
CROSS JOIN spans INDEXED BY spans_source_run_usage_idx
WHERE spans.source = 'codex' AND spans.run_id = identities.run_id
  AND spans.usage_id = identities.usage_id
  AND COALESCE(json_extract(spans.attributes_json, '$."gen_ai.usage.role"'), '') = 'corroborating'`); err != nil {
		return fmt.Errorf("project replay Codex corroborating links: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO model_call_trace_supports (call_id, trace_id, activity_id, support_kind)
SELECT links.call_id, spans.trace_id, spans.activity_id, 'corroborating'
FROM model_calls calls INDEXED BY model_calls_session_filter_idx
CROSS JOIN model_call_activity_links links INDEXED BY model_call_activity_call_idx
CROSS JOIN spans INDEXED BY spans_activity_id_idx
WHERE calls.source = 'codex' AND links.call_id = calls.call_id
  AND spans.activity_id = links.activity_id AND spans.source = 'codex'
  AND spans.trace_id <> '' AND links.evidence_role = 'corroborating'`); err != nil {
		return fmt.Errorf("project replay Codex corroborating trace supports: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_call_trace_memberships
WHERE call_id IN (SELECT call_id FROM model_calls WHERE source = 'codex')`); err != nil {
		return fmt.Errorf("replace replay Codex trace memberships: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO model_call_trace_memberships (call_id, trace_id)
SELECT DISTINCT supports.call_id, supports.trace_id
FROM model_call_trace_supports supports
JOIN model_calls calls ON calls.call_id = supports.call_id
WHERE calls.source = 'codex'`); err != nil {
		return fmt.Errorf("derive replay Codex trace memberships: %w", err)
	}
	return nil
}

func publishCorroboratingSpanChanges(ctx context.Context, transaction *sql.Tx, sequence int64, source, sessionID string) error {
	rows, err := transaction.QueryContext(ctx, `SELECT activity_id, run_id, trace_id FROM spans
WHERE source = ? AND run_id = ?
  AND COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), '') = 'corroborating'`, source, sessionID)
	if err != nil {
		return fmt.Errorf("load %s corroborating activity changes: %w", source, err)
	}
	defer rows.Close()
	for rows.Next() {
		var activityID, sessionID, traceID string
		if err := rows.Scan(&activityID, &sessionID, &traceID); err != nil {
			return err
		}
		if sessionID != "" {
			if err := appendActivityChange(ctx, transaction, sequence, 0, "session", source, sessionID, activityID, "upsert"); err != nil {
				return err
			}
		}
		if traceID != "" {
			if err := appendActivityChange(ctx, transaction, sequence, 0, "trace", "", traceID, activityID, "upsert"); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}
