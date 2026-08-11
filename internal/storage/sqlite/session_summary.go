package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
)

// loadSessionSummary reads only aggregate columns and agent metadata. It does
// not materialize operation content; operation evidence is owned by
// ListSessionActivities.
func (store *Store) loadSessionSummary(ctx context.Context, sourceID, sessionID string) (query.Session, error) {
	const branches = `
  SELECT source, trace_id, ended_at AS observed_at, agent_id, agent_definition, agent_type, parent_agent_id, model,
    cost_usd, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported
  FROM spans
  WHERE ended_at >= ? AND source = ? AND run_id = ? AND activity_kind <> 'unknown'
  UNION ALL
  SELECT source, trace_id, observed_at, agent_id, agent_definition, agent_type, parent_agent_id, model,
    cost_usd, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported
  FROM logs
  WHERE observed_at >= ? AND source = ? AND run_id = ? AND activity_kind <> 'unknown'`

	const aggregate = `WITH activity AS (
%s
)
SELECT COUNT(*), COALESCE(MIN(observed_at), ''), COALESCE(MAX(observed_at), ''),
  COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
  COALESCE(SUM(reasoning_tokens), 0),
  MAX(CASE WHEN input_tokens_reported <> 0 OR input_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN output_tokens_reported <> 0 OR output_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_read_tokens_reported <> 0 OR cache_read_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_write_tokens_reported <> 0 OR cache_write_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN reasoning_tokens_reported <> 0 OR reasoning_tokens <> 0 THEN 1 ELSE 0 END),
  COUNT(cost_usd), COALESCE(SUM(cost_usd), 0)
FROM activity`

	args := sessionArgs(sourceID, sessionID)
	var count int64
	var startedAt, endedAt string
	var input, output, cacheRead, cacheWrite, reasoning int64
	var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported int
	var costCount int64
	var cost float64
	if err := store.db.QueryRowContext(ctx, fmt.Sprintf(aggregate, branches), args...).Scan(
		&count, &startedAt, &endedAt, &input, &output, &cacheRead, &cacheWrite, &reasoning,
		&inputReported, &outputReported, &cacheReadReported, &cacheWriteReported, &reasoningReported,
		&costCount, &cost,
	); err != nil {
		return query.Session{}, fmt.Errorf("query session aggregate: %w", err)
	}
	if count == 0 {
		return query.Session{}, query.ErrConversationNotFound
	}
	started, err := parseStorageTime(startedAt)
	if err != nil {
		return query.Session{}, err
	}
	ended, err := parseStorageTime(endedAt)
	if err != nil {
		return query.Session{}, err
	}
	session := query.Session{
		ID: sessionID, SourceID: sourceID, Sources: []query.TelemetrySource{store.describeSource(sourceID)},
		StartedAt: started, EndedAt: ended, ActivityCount: count,
		Tokens: aggregateTokens(input, output, cacheRead, cacheWrite, reasoning, inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported),
		Agents: make([]query.AgentSession, 0), Activities: make([]query.Activity, 0),
	}
	session.AgentCount = 0
	if costCount > 0 {
		session.CostUSD = &cost
	}

	agentQuery := fmt.Sprintf(`WITH activity AS (
%s
)
SELECT CASE WHEN agent_id = '' THEN 'main' ELSE agent_id END,
  MAX(agent_definition), MAX(agent_type), MAX(parent_agent_id), MAX(model), COUNT(*),
  COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0), COALESCE(SUM(reasoning_tokens), 0),
  MAX(CASE WHEN input_tokens_reported <> 0 OR input_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN output_tokens_reported <> 0 OR output_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_read_tokens_reported <> 0 OR cache_read_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN cache_write_tokens_reported <> 0 OR cache_write_tokens <> 0 THEN 1 ELSE 0 END),
  MAX(CASE WHEN reasoning_tokens_reported <> 0 OR reasoning_tokens <> 0 THEN 1 ELSE 0 END)
FROM activity
GROUP BY CASE WHEN agent_id = '' THEN 'main' ELSE agent_id END
ORDER BY 1`, branches)
	agentRows, err := store.db.QueryContext(ctx, agentQuery, args...)
	if err != nil {
		return query.Session{}, fmt.Errorf("query session agents: %w", err)
	}
	defer agentRows.Close()
	for agentRows.Next() {
		var agent query.AgentSession
		var input, output, cacheRead, cacheWrite, reasoning int64
		var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported int
		if err := agentRows.Scan(&agent.AgentID, &agent.AgentDefinition, &agent.AgentType, &agent.ParentAgentID, &agent.Model, &agent.ActivityCount,
			&input, &output, &cacheRead, &cacheWrite, &reasoning, &inputReported, &outputReported, &cacheReadReported, &cacheWriteReported, &reasoningReported); err != nil {
			return query.Session{}, fmt.Errorf("scan session agent: %w", err)
		}
		if agent.AgentID == "main" && agent.AgentType == "" {
			agent.AgentType = "root"
		}
		agent.Tokens = aggregateTokens(input, output, cacheRead, cacheWrite, reasoning, inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported)
		session.Agents = append(session.Agents, agent)
	}
	if err := agentRows.Err(); err != nil {
		return query.Session{}, fmt.Errorf("iterate session agents: %w", err)
	}
	session.AgentCount = int64(len(session.Agents))

	traceRows, err := store.db.QueryContext(ctx, fmt.Sprintf(`WITH activity AS (
%s
)
SELECT DISTINCT trace_id FROM activity WHERE trace_id <> '' ORDER BY trace_id`, branches), args...)
	if err != nil {
		return query.Session{}, fmt.Errorf("query session traces: %w", err)
	}
	defer traceRows.Close()
	for traceRows.Next() {
		var traceID string
		if err := traceRows.Scan(&traceID); err != nil {
			return query.Session{}, fmt.Errorf("scan session trace: %w", err)
		}
		session.TraceIDs = append(session.TraceIDs, traceID)
	}
	if err := traceRows.Err(); err != nil {
		return query.Session{}, fmt.Errorf("iterate session traces: %w", err)
	}
	return session, nil
}

func sessionArgs(sourceID, sessionID string) []any {
	since := formatTime(time.Unix(0, 0))
	return []any{since, sourceID, sessionID, since, sourceID, sessionID}
}

func aggregateTokens(input, output, cacheRead, cacheWrite, reasoning int64, inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported int) canonical.TokenUsage {
	return canonical.TokenUsage{
		Input: input, Output: output, CacheRead: cacheRead, CacheWrite: cacheWrite, Reasoning: reasoning,
		Presence: canonical.TokenPresence{
			Input: inputReported != 0 && input == 0, Output: outputReported != 0 && output == 0,
			CacheRead: cacheReadReported != 0 && cacheRead == 0, CacheWrite: cacheWriteReported != 0 && cacheWrite == 0,
			Reasoning: reasoningReported != 0 && reasoning == 0,
		},
	}
}
