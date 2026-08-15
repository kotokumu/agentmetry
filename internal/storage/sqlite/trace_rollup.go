package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/theoden9014/agentmetry/internal/canonical"
)

func (store *Store) initializeTraceRollups(ctx context.Context) error {
	var count int64
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM trace_rollups`).Scan(&count); err != nil {
		return fmt.Errorf("inspect trace rollups: %w", err)
	}
	if count > 0 {
		return nil
	}
	rows, err := store.db.QueryContext(ctx, `SELECT trace_id FROM spans WHERE trace_id <> '' UNION SELECT trace_id FROM logs WHERE trace_id <> ''`)
	if err != nil {
		return fmt.Errorf("discover trace rollups: %w", err)
	}
	traceIDs := make([]string, 0, 128)
	for rows.Next() {
		var traceID string
		if err := rows.Scan(&traceID); err != nil {
			_ = rows.Close()
			return err
		}
		traceIDs = append(traceIDs, traceID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin trace rollup initialization: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	for _, traceID := range traceIDs {
		if err := rebuildTraceRollupTx(ctx, transaction, traceID); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit trace rollup initialization: %w", err)
	}
	return nil
}

func updateAffectedTraceRollups(ctx context.Context, transaction *sql.Tx, batch canonical.Batch, previous map[storedSpanKey]storedSpanScope, sequence int64) error {
	traceIDs := make(map[string]struct{})
	for _, span := range batch.Spans {
		if canonical.IsSemanticSpan(span) && span.TraceID != "" {
			traceIDs[span.TraceID] = struct{}{}
		}
	}
	for _, log := range batch.Logs {
		if log.TraceID != "" {
			traceIDs[log.TraceID] = struct{}{}
		}
	}
	if len(traceIDs) == 0 {
		return nil
	}
	if !traceRollupCanApplyIncrementally(batch, previous) {
		for traceID := range traceIDs {
			if err := rebuildTraceRollupTx(ctx, transaction, traceID); err != nil {
				return err
			}
		}
		return nil
	}
	for _, old := range previous {
		if old.activityKind == string(canonical.ActivityUnknown) || old.trace == "" {
			continue
		}
		root, missing := 0, 0
		if old.parentSpanID == "" {
			root = 1
		}
		if old.missingParent {
			missing = 1
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE trace_rollups SET activity_count = activity_count - 1,
  root_span_count = root_span_count - ?, missing_parent_count = missing_parent_count - ? WHERE trace_id = ?`, root, missing, old.trace); err != nil {
			return fmt.Errorf("subtract revised trace span: %w", err)
		}
	}
	const increment = `INSERT INTO trace_rollups (
  trace_id, started_at, ended_at, status_rank, activity_count, root_span_count, missing_parent_count
)
SELECT trace_id, MIN(started_at), MAX(ended_at), MAX(status_rank), COUNT(*), SUM(root_span), SUM(missing_parent)
FROM (
  SELECT spans.trace_id, spans.started_at, spans.ended_at,
    CASE lower(spans.status) WHEN 'error' THEN 2 WHEN 'ok' THEN 1 ELSE 0 END AS status_rank,
    CASE WHEN spans.parent_span_id = '' THEN 1 ELSE 0 END AS root_span,
    CASE WHEN spans.parent_span_id <> '' AND NOT EXISTS (
      SELECT 1 FROM spans AS parent WHERE parent.trace_id = spans.trace_id AND parent.span_id = spans.parent_span_id
    ) THEN 1 ELSE 0 END AS missing_parent
  FROM spans WHERE spans.projection_sequence = ? AND spans.trace_id <> '' AND spans.activity_kind <> 'unknown'
  UNION ALL
  SELECT trace_id, observed_at, observed_at, 0, 0, 0
  FROM logs WHERE projection_sequence = ? AND trace_id <> ''
) GROUP BY trace_id
ON CONFLICT(trace_id) DO UPDATE SET
  started_at = MIN(trace_rollups.started_at, excluded.started_at),
  ended_at = MAX(trace_rollups.ended_at, excluded.ended_at),
  status_rank = MAX(trace_rollups.status_rank, excluded.status_rank),
  activity_count = trace_rollups.activity_count + excluded.activity_count,
  root_span_count = trace_rollups.root_span_count + excluded.root_span_count,
  missing_parent_count = trace_rollups.missing_parent_count + excluded.missing_parent_count`
	if _, err := transaction.ExecContext(ctx, increment, sequence, sequence); err != nil {
		return fmt.Errorf("increment trace rollups: %w", err)
	}
	for _, span := range batch.Spans {
		if !canonical.IsSemanticSpan(span) || previous[storedSpanKey{traceID: span.TraceID, spanID: span.SpanID}].present {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE trace_rollups SET missing_parent_count = MAX(0, missing_parent_count - (
  SELECT COUNT(*) FROM spans WHERE trace_id = ? AND parent_span_id = ? AND projection_sequence <> ?
)) WHERE trace_id = ?`, span.TraceID, span.SpanID, sequence, span.TraceID); err != nil {
			return fmt.Errorf("resolve trace missing parents: %w", err)
		}
	}
	if err := upsertTraceMembers(ctx, transaction, sequence); err != nil {
		return err
	}
	return nil
}

func traceRollupCanApplyIncrementally(batch canonical.Batch, previous map[storedSpanKey]storedSpanScope) bool {
	for _, span := range batch.Spans {
		if !canonical.IsSemanticSpan(span) {
			continue
		}
		old, exists := previous[storedSpanKey{traceID: span.TraceID, spanID: span.SpanID}]
		if !exists {
			continue
		}
		if old.activityKind != string(span.Kind) || old.source != normalizeSource(span.Source) || old.session != span.Agent.RunID ||
			old.agentID != span.Agent.AgentID || old.parentSpanID != span.ParentSpanID ||
			formatTime(span.StartedAt) > old.startedAt ||
			formatTime(span.EndedAt) < old.endedAt || traceStatusRank(span.Status) < traceStatusRank(old.status) {
			return false
		}
	}
	return true
}

func traceStatusRank(status string) int {
	switch strings.ToLower(status) {
	case "error":
		return 2
	case "ok":
		return 1
	default:
		return 0
	}
}

func rebuildTraceRollupTx(ctx context.Context, transaction *sql.Tx, traceID string) error {
	for _, statement := range []string{
		`DELETE FROM trace_rollups WHERE trace_id = ?`,
		`DELETE FROM trace_conversations WHERE trace_id = ?`,
		`DELETE FROM trace_agents WHERE trace_id = ?`,
	} {
		if _, err := transaction.ExecContext(ctx, statement, traceID); err != nil {
			return fmt.Errorf("clear trace rollup: %w", err)
		}
	}
	_, err := transaction.ExecContext(ctx, `INSERT INTO trace_rollups (
  trace_id, started_at, ended_at, status_rank, activity_count, root_span_count, missing_parent_count
)
SELECT ?, MIN(started_at), MAX(ended_at), MAX(status_rank), COUNT(*), SUM(root_span), SUM(missing_parent)
FROM (
  SELECT spans.started_at, spans.ended_at,
    CASE lower(spans.status) WHEN 'error' THEN 2 WHEN 'ok' THEN 1 ELSE 0 END AS status_rank,
    CASE WHEN spans.parent_span_id = '' THEN 1 ELSE 0 END AS root_span,
    CASE WHEN spans.parent_span_id <> '' AND NOT EXISTS (
      SELECT 1 FROM spans AS parent WHERE parent.trace_id = spans.trace_id AND parent.span_id = spans.parent_span_id
    ) THEN 1 ELSE 0 END AS missing_parent
  FROM spans WHERE spans.trace_id = ? AND spans.activity_kind <> 'unknown'
  UNION ALL
  SELECT observed_at, observed_at, 0, 0, 0 FROM logs WHERE trace_id = ?
) HAVING COUNT(*) > 0`, traceID, traceID, traceID)
	if err != nil {
		return fmt.Errorf("rebuild trace rollup: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO trace_conversations (trace_id, source, run_id)
SELECT trace_id, source, run_id FROM spans WHERE trace_id = ? AND run_id <> ''
UNION SELECT trace_id, source, run_id FROM logs WHERE trace_id = ? AND run_id <> ''`, traceID, traceID); err != nil {
		return fmt.Errorf("rebuild trace conversations: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, traceAgentInsert(`AND trace_id = ?`, `AND trace_id = ?`), traceID, traceID); err != nil {
		return fmt.Errorf("rebuild trace agents: %w", err)
	}
	return nil
}

func upsertTraceMembers(ctx context.Context, transaction *sql.Tx, sequence int64) error {
	if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO trace_conversations (trace_id, source, run_id)
SELECT trace_id, source, run_id FROM spans WHERE projection_sequence = ? AND trace_id <> '' AND run_id <> ''
UNION SELECT trace_id, source, run_id FROM logs WHERE projection_sequence = ? AND trace_id <> '' AND run_id <> ''`, sequence, sequence); err != nil {
		return fmt.Errorf("increment trace conversations: %w", err)
	}
	statement := traceAgentInsert(`AND projection_sequence = ?`, `AND projection_sequence = ?`) + `
ON CONFLICT(trace_id, source, run_id, agent_id) DO UPDATE SET
  agent_definition = CASE WHEN excluded.agent_definition <> '' THEN excluded.agent_definition ELSE trace_agents.agent_definition END,
  agent_type = CASE WHEN excluded.agent_type <> '' THEN excluded.agent_type ELSE trace_agents.agent_type END,
  parent_agent_id = CASE WHEN excluded.parent_agent_id <> '' THEN excluded.parent_agent_id ELSE trace_agents.parent_agent_id END,
  model = CASE WHEN excluded.model <> '' THEN excluded.model ELSE trace_agents.model END`
	if _, err := transaction.ExecContext(ctx, statement, sequence, sequence); err != nil {
		return fmt.Errorf("increment trace agents: %w", err)
	}
	return nil
}

func traceAgentInsert(spanFilter, logFilter string) string {
	return `INSERT INTO trace_agents (trace_id, source, run_id, agent_id, agent_definition, agent_type, parent_agent_id, model)
SELECT trace_id, source, run_id, agent_id,
  MAX(agent_definition), MAX(agent_type), MAX(parent_agent_id), MAX(model)
FROM (
  SELECT trace_id, source, run_id, agent_id, agent_definition, agent_type, parent_agent_id, model
  FROM spans WHERE trace_id <> '' AND agent_id <> '' ` + spanFilter + `
  UNION ALL
  SELECT trace_id, source, run_id, agent_id, agent_definition, agent_type, parent_agent_id, model
  FROM logs WHERE trace_id <> '' AND agent_id <> '' ` + logFilter + `
) GROUP BY trace_id, source, run_id, agent_id`
}
