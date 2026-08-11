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

func rebuildAffectedSessionRollups(ctx context.Context, transaction *sql.Tx, batch canonical.Batch) error {
	keys := make(map[sessionKey]struct{})
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
		if err := rebuildSessionRollupTx(ctx, transaction, key.source, key.runID); err != nil {
			return err
		}
	}
	return nil
}

func rebuildSessionRollupTx(ctx context.Context, transaction *sql.Tx, sourceID, runID string) error {
	if _, err := transaction.ExecContext(ctx, "DELETE FROM session_rollups WHERE source = ? AND run_id = ?", sourceID, runID); err != nil {
		return fmt.Errorf("delete session rollup: %w", err)
	}
	const statement = `INSERT INTO session_rollups (
  source, run_id, started_at, ended_at, activity_count, trace_count, log_count, agent_count,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_reported, output_reported, cache_read_reported, cache_write_reported, reasoning_reported, cost_usd
)
SELECT source, run_id, MIN(observed_at), MAX(observed_at), COUNT(*),
  SUM(CASE WHEN signal = 'trace' THEN 1 ELSE 0 END),
  SUM(CASE WHEN signal = 'log' THEN 1 ELSE 0 END),
  COUNT(DISTINCT CASE WHEN agent_id <> '' THEN agent_id ELSE 'main' END),
  COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0), COALESCE(SUM(reasoning_tokens), 0),
  MAX(CASE WHEN input_tokens_reported <> 0 OR input_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN output_tokens_reported <> 0 OR output_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_read_tokens_reported <> 0 OR cache_read_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_write_tokens_reported <> 0 OR cache_write_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN reasoning_tokens_reported <> 0 OR reasoning_tokens <> 0 THEN 1 ELSE 0 END),
  COALESCE(SUM(cost_usd), 0)
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
