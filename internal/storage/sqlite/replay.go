package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
)

// commitReplayExportTx stores authoritative journal and projection facts. It
// maintains order-sensitive agent metadata, but defers cost correlation and
// session/trace rollups until FinalizeReplay. Replay candidates are private, so
// no consumer can observe this intentionally incomplete state.
func (store *Store) commitReplayExportTx(ctx context.Context, transaction *sql.Tx, accepted ingest.AcceptedExport, prepared preparedExport) error {
	exportID, err := insertExport(ctx, transaction, accepted, prepared)
	if err != nil {
		return err
	}
	for _, item := range accepted.Observations {
		if err := insertObservation(ctx, transaction, exportID, item); err != nil {
			return err
		}
	}
	if accepted.NormalizationError != "" {
		return nil
	}
	previousSpans, err := loadPreviousSpanScopes(ctx, transaction, accepted.Projection)
	if err != nil {
		return err
	}
	previousSessions := make(map[sessionKey]struct{})
	for _, scope := range previousSpans {
		if scope.session != "" {
			previousSessions[sessionKey{source: scope.source, runID: scope.session}] = struct{}{}
		}
	}
	var logActivityIDs []string
	sequences := projectionSequences{row: exportID}
	if err := store.commitProjection(ctx, transaction, accepted.Projection, sequences, previousSpans, &logActivityIDs); err != nil {
		return err
	}
	if err := store.persistReplayModelCallFacts(ctx, transaction, exportID, accepted, prepared, logActivityIDs, exportID); err != nil {
		return err
	}
	attribution, err := reconcileClaudeModelCallAgents(ctx, transaction, accepted.Projection, previousSpans, sequences)
	if err != nil {
		return err
	}
	if err := updateReplaySessionAgents(ctx, transaction, accepted.Projection, previousSessions, previousSpans, exportID, rollupCanApplyIncrementally(accepted.Projection, previousSpans) && !attribution.rebuildSessions); err != nil {
		return err
	}
	if err := updateReplayTraceAgents(ctx, transaction, accepted.Projection, previousSpans, exportID); err != nil {
		return err
	}
	for traceID := range attribution.traceIDs {
		if err := rebuildTraceAgentsTx(ctx, transaction, traceID); err != nil {
			return err
		}
	}
	return nil
}

func updateReplaySessionAgents(ctx context.Context, transaction *sql.Tx, batch canonical.Batch, previousSessions map[sessionKey]struct{}, previousSpans map[storedSpanKey]storedSpanScope, sequence int64, incremental bool) error {
	if !incremental {
		keys := make(map[sessionKey]struct{})
		for key := range previousSessions {
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
		}
		return nil
	}
	for _, old := range previousSpans {
		if old.activityKind == string(canonical.ActivityUnknown) || old.session == "" {
			continue
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE session_agents SET
  activity_count = activity_count - 1,
  input_tokens = input_tokens - ?, output_tokens = output_tokens - ?,
  cache_read_tokens = cache_read_tokens - ?, cache_write_tokens = cache_write_tokens - ?,
  reasoning_tokens = reasoning_tokens - ?
WHERE source = ? AND run_id = ? AND agent_id = ?`, old.input, old.output, old.cacheRead, old.cacheWrite, old.reasoning, old.source, old.session, normalizedAgentID(old.agentID, old.session)); err != nil {
			return fmt.Errorf("subtract replay span agent aggregate: %w", err)
		}
	}
	statement := sessionAgentAggregateInsert(`AND projection_sequence = ?`, `AND projection_sequence = ?`) + `
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
	if _, err := transaction.ExecContext(ctx, statement, sequence, sequence); err != nil {
		return fmt.Errorf("increment replay session agent aggregate: %w", err)
	}
	return nil
}

func updateReplayTraceAgents(ctx context.Context, transaction *sql.Tx, batch canonical.Batch, previous map[storedSpanKey]storedSpanScope, sequence int64) error {
	if !traceRollupCanApplyIncrementally(batch, previous) {
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
		for traceID := range traceIDs {
			if err := rebuildTraceAgentsTx(ctx, transaction, traceID); err != nil {
				return err
			}
		}
		return nil
	}
	statement := traceAgentInsert(`AND projection_sequence = ?`, `AND projection_sequence = ?`) + `
ON CONFLICT(trace_id, source, run_id, agent_id) DO UPDATE SET
  agent_definition = CASE WHEN excluded.agent_definition <> '' THEN excluded.agent_definition ELSE trace_agents.agent_definition END,
  agent_type = CASE WHEN excluded.agent_type <> '' THEN excluded.agent_type ELSE trace_agents.agent_type END,
  parent_agent_id = CASE WHEN excluded.parent_agent_id <> '' THEN excluded.parent_agent_id ELSE trace_agents.parent_agent_id END,
  model = CASE WHEN excluded.model <> '' THEN excluded.model ELSE trace_agents.model END`
	if _, err := transaction.ExecContext(ctx, statement, sequence, sequence); err != nil {
		return fmt.Errorf("increment replay trace agent aggregate: %w", err)
	}
	return nil
}

func rebuildTraceAgentsTx(ctx context.Context, transaction *sql.Tx, traceID string) error {
	if _, err := transaction.ExecContext(ctx, `DELETE FROM trace_agents WHERE trace_id = ?`, traceID); err != nil {
		return fmt.Errorf("clear replay trace agents: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, traceAgentInsert(`AND trace_id = ?`, `AND trace_id = ?`), traceID, traceID); err != nil {
		return fmt.Errorf("rebuild replay trace agents: %w", err)
	}
	return nil
}

// FinalizeReplay derives every query-facing projection once from the complete
// replayed fact set. It is intentionally a one-way transition: live ingestion
// must never resume through a replay-only candidate.
func (candidate *ReplayCandidate) FinalizeReplay(ctx context.Context) error {
	store := candidate.store
	store.writeMu.Lock()
	defer store.writeMu.Unlock()
	if candidate.finalized {
		return fmt.Errorf("finalize replay: candidate is already finalized")
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replay finalization: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	for _, step := range []struct {
		name string
		run  func(context.Context, *sql.Tx) error
	}{
		{name: "cost projections", run: store.rebuildReplayCostProjections},
		{name: "session memberships", run: rebuildReplaySessionMemberships},
		{name: "session aggregates", run: rebuildReplaySessionAggregates},
		{name: "trace aggregates", run: rebuildReplayTraceAggregates},
		{name: "temporary ordering", run: resetReplayProjectionSequences},
	} {
		if err := step.run(ctx, transaction); err != nil {
			return fmt.Errorf("finalize replay %s: %w", step.name, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit replay finalization: %w", err)
	}
	candidate.finalized = true
	return nil
}

func (store *Store) rebuildReplayCostProjections(ctx context.Context, transaction *sql.Tx) error {
	claudeSessions, err := queryStrings(ctx, transaction, `SELECT DISTINCT native_session_id
FROM model_call_evidence WHERE source = 'claude' ORDER BY native_session_id`)
	if err != nil {
		return err
	}
	for _, sessionID := range claudeSessions {
		if err := rebuildClaudeCostProjection(ctx, transaction, 0, sessionID); err != nil {
			return err
		}
	}
	if err := repriceCodexCalls(ctx, transaction, 0); err != nil {
		return err
	}
	codexSessions, err := queryStrings(ctx, transaction, `SELECT DISTINCT native_session_id
FROM model_calls WHERE source = 'codex' ORDER BY native_session_id`)
	if err != nil {
		return err
	}
	for _, sessionID := range codexSessions {
		if err := rebuildCodexCorroboratingSupports(ctx, transaction, 0, sessionID); err != nil {
			return err
		}
	}
	return nil
}

func rebuildReplaySessionMemberships(ctx context.Context, transaction *sql.Tx) error {
	sources, err := queryStrings(ctx, transaction, `SELECT DISTINCT source FROM session_links ORDER BY source`)
	if err != nil {
		return err
	}
	for _, sourceID := range sources {
		if err := rebuildSessionMemberships(ctx, transaction, sourceID); err != nil {
			return err
		}
	}
	return nil
}

func rebuildReplaySessionAggregates(ctx context.Context, transaction *sql.Tx) error {
	for _, statement := range []string{
		`DELETE FROM session_rollups`,
		`DELETE FROM session_traces`,
	} {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("clear replay session aggregate: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO session_traces (source, run_id, trace_id)
SELECT source, run_id, trace_id FROM spans WHERE run_id <> '' AND trace_id <> '' AND activity_kind <> 'unknown'
UNION SELECT source, run_id, trace_id FROM logs WHERE run_id <> '' AND trace_id <> '' AND activity_kind <> 'unknown'`); err != nil {
		return fmt.Errorf("rebuild replay session traces: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, sessionRollupAggregateInsert(``, ``)); err != nil {
		return fmt.Errorf("rebuild replay session rollups: %w", err)
	}
	return nil
}

func rebuildReplayTraceAggregates(ctx context.Context, transaction *sql.Tx) error {
	for _, statement := range []string{
		`DELETE FROM trace_rollups`,
		`DELETE FROM trace_conversations`,
	} {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("clear replay trace aggregate: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, traceRollupAggregateInsert(``, ``)); err != nil {
		return fmt.Errorf("rebuild replay trace rollups: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO trace_conversations (trace_id, source, run_id)
SELECT trace_id, source, run_id FROM spans WHERE trace_id <> '' AND run_id <> ''
UNION SELECT trace_id, source, run_id FROM logs WHERE trace_id <> '' AND run_id <> ''`); err != nil {
		return fmt.Errorf("rebuild replay trace conversations: %w", err)
	}
	return nil
}

func resetReplayProjectionSequences(ctx context.Context, transaction *sql.Tx) error {
	for _, table := range []string{"spans", "logs", "metrics", "model_calls"} {
		if _, err := transaction.ExecContext(ctx, `UPDATE `+table+` SET projection_sequence = 0 WHERE projection_sequence <> 0`); err != nil {
			return fmt.Errorf("reset %s projection sequence: %w", table, err)
		}
	}
	return nil
}

func queryStrings(ctx context.Context, reader sqlReader, statement string, args ...any) ([]string, error) {
	rows, err := reader.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
