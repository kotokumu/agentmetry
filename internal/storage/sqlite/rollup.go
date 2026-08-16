package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/theoden9014/agentmetry/internal/canonical"
)

type sessionKey struct {
	source string
	runID  string
}

func (store *Store) initializeSessionCostPresence(ctx context.Context) error {
	var pending int64
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_rollups WHERE cost_reported = -1`).Scan(&pending); err != nil {
		return fmt.Errorf("inspect session cost presence migration: %w", err)
	}
	if pending == 0 {
		return nil
	}
	_, err := store.db.ExecContext(ctx, `UPDATE session_rollups AS rollup SET cost_reported = CASE WHEN
  EXISTS (SELECT 1 FROM spans WHERE spans.source = rollup.source AND spans.run_id = rollup.run_id AND spans.cost_usd IS NOT NULL)
  OR EXISTS (SELECT 1 FROM logs WHERE logs.source = rollup.source AND logs.run_id = rollup.run_id AND logs.cost_usd IS NOT NULL)
THEN 1 ELSE 0 END WHERE cost_reported = -1`)
	if err != nil {
		return fmt.Errorf("initialize session cost presence: %w", err)
	}
	return nil
}

func (store *Store) initializeSessionAgents(ctx context.Context) error {
	var rollups, members int64
	if err := store.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM session_rollups), (SELECT COUNT(*) FROM session_agents)`).Scan(&rollups, &members); err != nil {
		return fmt.Errorf("inspect session agent membership: %w", err)
	}
	if rollups == 0 || members > 0 {
		return nil
	}
	_, err := store.db.ExecContext(ctx, sessionAgentAggregateInsert(``, ``))
	if err != nil {
		return fmt.Errorf("initialize session agent membership: %w", err)
	}
	return nil
}

func (store *Store) initializeSessionTraces(ctx context.Context) error {
	var count int64
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_traces`).Scan(&count); err != nil {
		return fmt.Errorf("inspect session trace membership: %w", err)
	}
	if count > 0 {
		return nil
	}
	_, err := store.db.ExecContext(ctx, `INSERT OR IGNORE INTO session_traces (source, run_id, trace_id)
SELECT source, run_id, trace_id FROM spans WHERE run_id <> '' AND trace_id <> '' AND activity_kind <> 'unknown'
UNION SELECT source, run_id, trace_id FROM logs WHERE run_id <> '' AND trace_id <> '' AND activity_kind <> 'unknown'`)
	if err != nil {
		return fmt.Errorf("initialize session trace membership: %w", err)
	}
	return nil
}

func (store *Store) rebuildSessionRollups(ctx context.Context) error {
	var count int64
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM session_rollups").Scan(&count); err != nil {
		return fmt.Errorf("count session rollups: %w", err)
	}
	if count > 0 {
		var incomplete int64
		if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM session_rollups WHERE (trace_count IS NULL OR log_count IS NULL) OR (trace_count = 0 AND log_count = 0)`).Scan(&incomplete); err != nil {
			return fmt.Errorf("check session rollup completeness: %w", err)
		}
		if incomplete == 0 {
			return nil
		}
	}
	rows, err := store.db.QueryContext(ctx, `SELECT source, run_id FROM spans WHERE run_id <> ''
UNION
SELECT source, run_id FROM logs WHERE run_id <> ''`)
	if err != nil {
		return fmt.Errorf("discover session rollups: %w", err)
	}
	defer rows.Close()
	keys := make([]sessionKey, 0, 128)
	for rows.Next() {
		var key sessionKey
		if err := rows.Scan(&key.source, &key.runID); err != nil {
			return fmt.Errorf("scan session rollup key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate session rollup keys: %w", err)
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session rollup rebuild: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	for _, key := range keys {
		if err := rebuildSessionRollupTx(ctx, transaction, key.source, key.runID); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit session rollup rebuild: %w", err)
	}
	return nil
}

func rebuildAffectedSessionRollups(ctx context.Context, transaction *sql.Tx, batch canonical.Batch, previous map[sessionKey]struct{}) error {
	keys := make(map[sessionKey]struct{})
	for key := range previous {
		keys[key] = struct{}{}
	}
	for _, span := range batch.Spans {
		if span.Agent.RunID != "" {
			keys[sessionKey{source: normalizeSource(span.Source), runID: span.Agent.RunID}] = struct{}{}
		}
	}
	for _, log := range batch.Logs {
		if log.Agent.RunID != "" {
			keys[sessionKey{source: normalizeSource(log.Source), runID: log.Agent.RunID}] = struct{}{}
		}
	}
	for key := range keys {
		if err := rebuildSessionAgentsTx(ctx, transaction, key); err != nil {
			return err
		}
		if err := rebuildSessionTracesTx(ctx, transaction, key); err != nil {
			return err
		}
		if err := rebuildSessionRollupTx(ctx, transaction, key.source, key.runID); err != nil {
			return err
		}
	}
	return nil
}

func rebuildSessionTracesTx(ctx context.Context, transaction *sql.Tx, key sessionKey) error {
	if _, err := transaction.ExecContext(ctx, `DELETE FROM session_traces WHERE source = ? AND run_id = ?`, key.source, key.runID); err != nil {
		return fmt.Errorf("delete session trace membership: %w", err)
	}
	_, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO session_traces (source, run_id, trace_id)
SELECT source, run_id, trace_id FROM spans WHERE source = ? AND run_id = ? AND trace_id <> '' AND activity_kind <> 'unknown'
UNION SELECT source, run_id, trace_id FROM logs WHERE source = ? AND run_id = ? AND trace_id <> '' AND activity_kind <> 'unknown'`, key.source, key.runID, key.source, key.runID)
	if err != nil {
		return fmt.Errorf("rebuild session trace membership: %w", err)
	}
	return nil
}

func rebuildSessionAgentsTx(ctx context.Context, transaction *sql.Tx, key sessionKey) error {
	if _, err := transaction.ExecContext(ctx, `DELETE FROM session_agents WHERE source = ? AND run_id = ?`, key.source, key.runID); err != nil {
		return fmt.Errorf("delete session agent membership: %w", err)
	}
	_, err := transaction.ExecContext(ctx, sessionAgentAggregateInsert(`AND source = ? AND run_id = ?`, `AND source = ? AND run_id = ?`), key.source, key.runID, key.source, key.runID)
	if err != nil {
		return fmt.Errorf("rebuild session agent membership: %w", err)
	}
	return nil
}

func sessionAgentAggregateInsert(spanFilter, logFilter string) string {
	return `INSERT INTO session_agents (
  source, run_id, agent_id, agent_definition, agent_type, parent_agent_id, model,
  activity_count, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
  reasoning_tokens, input_reported, output_reported, cache_read_reported,
  cache_write_reported, reasoning_reported
)
SELECT source, run_id, CASE WHEN agent_id = '' OR agent_id = 'main' OR agent_id = run_id THEN 'main' ELSE agent_id END,
  MAX(agent_definition), MAX(agent_type), MAX(parent_agent_id), MAX(model), COUNT(*),
  COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0), COALESCE(SUM(reasoning_tokens), 0),
  MAX(CASE WHEN input_tokens_reported <> 0 OR input_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN output_tokens_reported <> 0 OR output_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_read_tokens_reported <> 0 OR cache_read_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_write_tokens_reported <> 0 OR cache_write_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN reasoning_tokens_reported <> 0 OR reasoning_tokens <> 0 THEN 1 ELSE 0 END)
FROM (
  SELECT source, run_id, agent_id, agent_definition, agent_type, parent_agent_id, model,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported
  FROM spans WHERE run_id <> '' AND activity_kind <> 'unknown' ` + spanFilter + `
  UNION ALL
  SELECT source, run_id, agent_id, agent_definition, agent_type, parent_agent_id, model,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported
  FROM logs WHERE run_id <> '' AND activity_kind <> 'unknown' ` + logFilter + `
) GROUP BY source, run_id, CASE WHEN agent_id = '' OR agent_id = 'main' OR agent_id = run_id THEN 'main' ELSE agent_id END`
}

func updateAffectedSessionRollups(ctx context.Context, transaction *sql.Tx, batch canonical.Batch, previousSessions map[sessionKey]struct{}, previousSpans map[storedSpanKey]storedSpanScope, sequence int64, incremental bool) error {
	if !incremental {
		return rebuildAffectedSessionRollups(ctx, transaction, batch, previousSessions)
	}
	for _, old := range previousSpans {
		if old.activityKind == string(canonical.ActivityUnknown) || old.session == "" {
			continue
		}
		cost := float64(0)
		if old.cost.Valid {
			cost = old.cost.Float64
		}
		_, err := transaction.ExecContext(ctx, `UPDATE session_rollups SET
  activity_count = activity_count - 1, trace_count = trace_count - 1,
  input_tokens = input_tokens - ?, output_tokens = output_tokens - ?,
  cache_read_tokens = cache_read_tokens - ?, cache_write_tokens = cache_write_tokens - ?,
  reasoning_tokens = reasoning_tokens - ?, cost_usd = cost_usd - ?
WHERE source = ? AND run_id = ?`, old.input, old.output, old.cacheRead, old.cacheWrite, old.reasoning, cost, old.source, old.session)
		if err != nil {
			return fmt.Errorf("subtract revised span rollup: %w", err)
		}
		_, err = transaction.ExecContext(ctx, `UPDATE session_agents SET
  activity_count = activity_count - 1,
  input_tokens = input_tokens - ?, output_tokens = output_tokens - ?,
  cache_read_tokens = cache_read_tokens - ?, cache_write_tokens = cache_write_tokens - ?,
  reasoning_tokens = reasoning_tokens - ?
WHERE source = ? AND run_id = ? AND agent_id = ?`, old.input, old.output, old.cacheRead, old.cacheWrite, old.reasoning, old.source, old.session, normalizedAgentID(old.agentID, old.session))
		if err != nil {
			return fmt.Errorf("subtract revised span agent rollup: %w", err)
		}
	}
	const statement = `INSERT INTO session_rollups (
  source, run_id, started_at, ended_at, activity_count, trace_count, log_count, agent_count,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_reported, output_reported, cache_read_reported, cache_write_reported, reasoning_reported, cost_usd, cost_reported
)
SELECT source, run_id, MIN(observed_at), MAX(observed_at), COUNT(*),
  SUM(CASE WHEN signal = 'trace' THEN 1 ELSE 0 END), SUM(CASE WHEN signal = 'log' THEN 1 ELSE 0 END), 0,
  COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0), COALESCE(SUM(reasoning_tokens), 0),
  MAX(CASE WHEN input_tokens_reported <> 0 OR input_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN output_tokens_reported <> 0 OR output_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_read_tokens_reported <> 0 OR cache_read_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_write_tokens_reported <> 0 OR cache_write_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN reasoning_tokens_reported <> 0 OR reasoning_tokens <> 0 THEN 1 ELSE 0 END),
  COALESCE(SUM(cost_usd), 0), MAX(CASE WHEN cost_usd IS NOT NULL THEN 1 ELSE 0 END)
FROM (
  SELECT 'trace' AS signal, source, run_id, ended_at AS observed_at, input_tokens, output_tokens,
    cache_read_tokens, cache_write_tokens, reasoning_tokens, input_tokens_reported, output_tokens_reported,
    cache_read_tokens_reported, cache_write_tokens_reported, reasoning_tokens_reported, cost_usd
  FROM spans WHERE projection_sequence = ? AND run_id <> '' AND activity_kind <> 'unknown'
  UNION ALL
  SELECT 'log', source, run_id, observed_at, input_tokens, output_tokens,
    cache_read_tokens, cache_write_tokens, reasoning_tokens, input_tokens_reported, output_tokens_reported,
    cache_read_tokens_reported, cache_write_tokens_reported, reasoning_tokens_reported, cost_usd
  FROM logs WHERE projection_sequence = ? AND run_id <> '' AND activity_kind <> 'unknown'
) GROUP BY source, run_id
ON CONFLICT(source, run_id) DO UPDATE SET
  started_at = MIN(session_rollups.started_at, excluded.started_at), ended_at = MAX(session_rollups.ended_at, excluded.ended_at),
  activity_count = session_rollups.activity_count + excluded.activity_count,
  trace_count = session_rollups.trace_count + excluded.trace_count, log_count = session_rollups.log_count + excluded.log_count,
  input_tokens = session_rollups.input_tokens + excluded.input_tokens, output_tokens = session_rollups.output_tokens + excluded.output_tokens,
  cache_read_tokens = session_rollups.cache_read_tokens + excluded.cache_read_tokens,
  cache_write_tokens = session_rollups.cache_write_tokens + excluded.cache_write_tokens,
  reasoning_tokens = session_rollups.reasoning_tokens + excluded.reasoning_tokens,
  input_reported = MAX(session_rollups.input_reported, excluded.input_reported),
  output_reported = MAX(session_rollups.output_reported, excluded.output_reported),
  cache_read_reported = MAX(session_rollups.cache_read_reported, excluded.cache_read_reported),
  cache_write_reported = MAX(session_rollups.cache_write_reported, excluded.cache_write_reported),
  reasoning_reported = MAX(session_rollups.reasoning_reported, excluded.reasoning_reported),
  cost_usd = session_rollups.cost_usd + excluded.cost_usd,
  cost_reported = MAX(session_rollups.cost_reported, excluded.cost_reported)`
	if _, err := transaction.ExecContext(ctx, statement, sequence, sequence); err != nil {
		return fmt.Errorf("increment session rollups: %w", err)
	}
	// A monotonic span revision can move the earliest observed timestamp
	// forward. MIN cannot subtract the previous extremum, so repair only the
	// revised session scopes using the source/run/time indexes.
	revisedSessions := make(map[sessionKey]struct{})
	for _, span := range batch.Spans {
		old, exists := previousSpans[storedSpanKey{traceID: span.TraceID, spanID: span.SpanID}]
		if exists && old.session != "" && old.endedAt != formatTime(span.EndedAt) {
			revisedSessions[sessionKey{source: old.source, runID: old.session}] = struct{}{}
		}
	}
	for key := range revisedSessions {
		if _, err := transaction.ExecContext(ctx, `UPDATE session_rollups SET started_at = (
  SELECT MIN(observed_at) FROM (
    SELECT MIN(ended_at) AS observed_at FROM spans
      WHERE source = ? AND run_id = ? AND activity_kind <> 'unknown'
    UNION ALL
    SELECT MIN(observed_at) FROM logs
      WHERE source = ? AND run_id = ? AND activity_kind <> 'unknown'
  )
) WHERE source = ? AND run_id = ?`, key.source, key.runID, key.source, key.runID, key.source, key.runID); err != nil {
			return fmt.Errorf("repair revised session start time: %w", err)
		}
	}
	agentStatement := sessionAgentAggregateInsert(`AND projection_sequence = ?`, `AND projection_sequence = ?`) + `
ON CONFLICT(source, run_id, agent_id) DO UPDATE SET
  agent_definition = CASE WHEN excluded.agent_definition <> '' THEN excluded.agent_definition ELSE session_agents.agent_definition END,
  agent_type = CASE WHEN excluded.agent_type <> '' THEN excluded.agent_type ELSE session_agents.agent_type END,
  parent_agent_id = CASE WHEN excluded.parent_agent_id <> '' THEN excluded.parent_agent_id ELSE session_agents.parent_agent_id END,
  model = CASE WHEN excluded.model <> '' THEN excluded.model ELSE session_agents.model END,
  activity_count = session_agents.activity_count + excluded.activity_count,
  input_tokens = session_agents.input_tokens + excluded.input_tokens,
  output_tokens = session_agents.output_tokens + excluded.output_tokens,
  cache_read_tokens = session_agents.cache_read_tokens + excluded.cache_read_tokens,
  cache_write_tokens = session_agents.cache_write_tokens + excluded.cache_write_tokens,
  reasoning_tokens = session_agents.reasoning_tokens + excluded.reasoning_tokens,
  input_reported = MAX(session_agents.input_reported, excluded.input_reported),
  output_reported = MAX(session_agents.output_reported, excluded.output_reported),
  cache_read_reported = MAX(session_agents.cache_read_reported, excluded.cache_read_reported),
  cache_write_reported = MAX(session_agents.cache_write_reported, excluded.cache_write_reported),
  reasoning_reported = MAX(session_agents.reasoning_reported, excluded.reasoning_reported)`
	if _, err := transaction.ExecContext(ctx, agentStatement, sequence, sequence); err != nil {
		return fmt.Errorf("increment session agent rollups: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO session_traces (source, run_id, trace_id)
SELECT source, run_id, trace_id FROM spans WHERE projection_sequence = ? AND run_id <> '' AND trace_id <> '' AND activity_kind <> 'unknown'
UNION SELECT source, run_id, trace_id FROM logs WHERE projection_sequence = ? AND run_id <> '' AND trace_id <> '' AND activity_kind <> 'unknown'`, sequence, sequence); err != nil {
		return fmt.Errorf("increment session trace membership: %w", err)
	}
	keys := make(map[sessionKey]struct{})
	for _, span := range batch.Spans {
		if canonical.IsSemanticSpan(span) && span.Agent.RunID != "" {
			keys[sessionKey{normalizeSource(span.Source), span.Agent.RunID}] = struct{}{}
		}
	}
	for _, log := range batch.Logs {
		if log.Kind != canonical.ActivityUnknown && log.Agent.RunID != "" {
			keys[sessionKey{normalizeSource(log.Source), log.Agent.RunID}] = struct{}{}
		}
	}
	for key := range keys {
		if _, err := transaction.ExecContext(ctx, `UPDATE session_rollups SET agent_count = (SELECT COUNT(*) FROM session_agents WHERE source = ? AND run_id = ?) WHERE source = ? AND run_id = ?`, key.source, key.runID, key.source, key.runID); err != nil {
			return fmt.Errorf("update session agent count: %w", err)
		}
	}
	return nil
}

func normalizedAgentID(value, runID string) string {
	if value == "" || value == "main" || value == runID {
		return "main"
	}
	return value
}

func rebuildSessionRollupTx(ctx context.Context, transaction *sql.Tx, sourceID, runID string) error {
	if _, err := transaction.ExecContext(ctx, "DELETE FROM session_rollups WHERE source = ? AND run_id = ?", sourceID, runID); err != nil {
		return fmt.Errorf("delete session rollup: %w", err)
	}
	const statement = `INSERT INTO session_rollups (
  source, run_id, started_at, ended_at, activity_count, trace_count, log_count, agent_count,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_reported, output_reported, cache_read_reported, cache_write_reported, reasoning_reported, cost_usd, cost_reported
)
SELECT source, run_id, MIN(observed_at), MAX(observed_at), COUNT(*),
  SUM(CASE WHEN signal = 'trace' THEN 1 ELSE 0 END),
  SUM(CASE WHEN signal = 'log' THEN 1 ELSE 0 END),
  COUNT(DISTINCT CASE WHEN agent_id = '' OR agent_id = 'main' OR agent_id = run_id THEN 'main' ELSE agent_id END),
  COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0), COALESCE(SUM(reasoning_tokens), 0),
  MAX(CASE WHEN input_tokens_reported <> 0 OR input_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN output_tokens_reported <> 0 OR output_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_read_tokens_reported <> 0 OR cache_read_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_write_tokens_reported <> 0 OR cache_write_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN reasoning_tokens_reported <> 0 OR reasoning_tokens <> 0 THEN 1 ELSE 0 END),
  COALESCE(SUM(cost_usd), 0), MAX(CASE WHEN cost_usd IS NOT NULL THEN 1 ELSE 0 END)
FROM (
  SELECT 'trace' AS signal, source, run_id, ended_at AS observed_at, agent_id, cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported
  FROM spans WHERE source = ? AND run_id = ? AND activity_kind <> 'unknown'
  UNION ALL
  SELECT 'log' AS signal, source, run_id, observed_at, agent_id, cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported
  FROM logs WHERE source = ? AND run_id = ? AND activity_kind <> 'unknown'
) AS activity
GROUP BY source, run_id`
	result, err := transaction.ExecContext(ctx, statement, sourceID, runID, sourceID, runID)
	if err != nil {
		return fmt.Errorf("insert session rollup: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read session rollup result: %w", err)
	} else if rows > 1 {
		return fmt.Errorf("session rollup produced %d rows", rows)
	}
	return nil
}
