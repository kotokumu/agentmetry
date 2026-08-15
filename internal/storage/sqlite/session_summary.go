package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
)

// loadSessionSummary reads only aggregate columns and agent metadata. It does
// not materialize operation content; operation evidence is owned by
// ListSessionActivities.
func (store *Store) loadSessionSummary(ctx context.Context, reader sqlReader, sourceID, sessionID string) (query.Session, error) {
	var count int64
	var startedAt, endedAt string
	var input, output, cacheRead, cacheWrite, reasoning int64
	var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported int
	var costReported bool
	var cost float64
	if err := reader.QueryRowContext(ctx, `SELECT activity_count, started_at, ended_at,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_reported, output_reported, cache_read_reported, cache_write_reported, reasoning_reported,
  cost_reported, cost_usd
FROM session_rollups WHERE source = ? AND run_id = ?`, sourceID, sessionID).Scan(
		&count, &startedAt, &endedAt, &input, &output, &cacheRead, &cacheWrite, &reasoning,
		&inputReported, &outputReported, &cacheReadReported, &cacheWriteReported, &reasoningReported,
		&costReported, &cost,
	); err != nil {
		if err == sql.ErrNoRows {
			return query.Session{}, query.ErrConversationNotFound
		}
		return query.Session{}, fmt.Errorf("query session aggregate: %w", err)
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
	if costReported {
		session.CostUSD = &cost
	}

	agentRows, err := reader.QueryContext(ctx, `SELECT agent_id, agent_definition, agent_type,
  parent_agent_id, model, activity_count, input_tokens, output_tokens, cache_read_tokens,
  cache_write_tokens, reasoning_tokens, input_reported, output_reported, cache_read_reported,
  cache_write_reported, reasoning_reported
FROM session_agents WHERE source = ? AND run_id = ? ORDER BY agent_id`, sourceID, sessionID)
	if err != nil {
		return query.Session{}, fmt.Errorf("query session agents: %w", err)
	}
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
	if err := agentRows.Close(); err != nil {
		return query.Session{}, fmt.Errorf("close session agents: %w", err)
	}
	parents, err := store.inferAgentParents(ctx, reader, sourceID, sessionID, session.Agents)
	if err != nil {
		return query.Session{}, err
	}
	for index := range session.Agents {
		if session.Agents[index].ParentAgentID == "" {
			session.Agents[index].ParentAgentID = parents[session.Agents[index].AgentID]
		}
	}
	session.AgentCount = int64(len(session.Agents))

	traceRows, err := reader.QueryContext(ctx, `SELECT trace_id FROM session_traces WHERE source = ? AND run_id = ? ORDER BY trace_id`, sourceID, sessionID)
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
	if err := traceRows.Close(); err != nil {
		return query.Session{}, fmt.Errorf("close session traces: %w", err)
	}
	return session, nil
}

// inferAgentParents reconstructs agent delegation when a producer omits
// gen_ai.agent.parent.id. Span parentage still records which agent's span
// contained the child agent's model/tool span, so the query projection can
// expose a useful topology without loading the whole session. Only agents that
// still lack an explicit parent are inspected, with bounded evidence and
// ancestry depth. Both lookups use composite/primary-key indexes.
func (store *Store) inferAgentParents(ctx context.Context, reader sqlReader, sourceID, sessionID string, agents []query.AgentSession) (map[string]string, error) {
	const evidenceLimit = 64
	parents := make(map[string]string)
	for _, agent := range agents {
		if agent.AgentID == "main" || agent.ParentAgentID != "" {
			continue
		}
		rows, err := reader.QueryContext(ctx, `SELECT trace_id, parent_span_id FROM spans
WHERE source = ? AND run_id = ? AND agent_id = ? AND parent_span_id <> ''
ORDER BY trace_id, span_id LIMIT ?`, sourceID, sessionID, agent.AgentID, evidenceLimit)
		if err != nil {
			return nil, fmt.Errorf("query bounded agent parent evidence: %w", err)
		}
		type candidate struct{ traceID, parentSpanID string }
		candidates := make([]candidate, 0, evidenceLimit)
		for rows.Next() {
			var value candidate
			if err := rows.Scan(&value.traceID, &value.parentSpanID); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan agent parent evidence: %w", err)
			}
			candidates = append(candidates, value)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterate agent parent evidence: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close agent parent evidence: %w", err)
		}
		for _, candidate := range candidates {
			parentID, err := nearestAncestorAgent(ctx, reader, sourceID, sessionID, candidate.traceID, candidate.parentSpanID, agent.AgentID)
			if err != nil {
				return nil, err
			}
			if parentID != "" {
				parents[agent.AgentID] = parentID
				break
			}
		}
	}
	return parents, nil
}

func nearestAncestorAgent(ctx context.Context, reader sqlReader, sourceID, sessionID, traceID, parentSpanID, childAgentID string) (string, error) {
	visited := make(map[string]struct{}, 8)
	for depth := 0; parentSpanID != "" && depth < 64; depth++ {
		if _, exists := visited[parentSpanID]; exists {
			return "", nil
		}
		visited[parentSpanID] = struct{}{}
		var nextParentSpanID, parentAgentID string
		err := reader.QueryRowContext(ctx, `SELECT parent_span_id, agent_id FROM spans
WHERE trace_id = ? AND span_id = ? AND source = ? AND run_id = ?`, traceID, parentSpanID, sourceID, sessionID).Scan(&nextParentSpanID, &parentAgentID)
		if err == sql.ErrNoRows {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("query agent ancestor: %w", err)
		}
		parentAgentID = sessionAgentID(parentAgentID)
		if parentAgentID != childAgentID {
			return parentAgentID, nil
		}
		parentSpanID = nextParentSpanID
	}
	return "", nil
}

func sessionAgentID(agentID string) string {
	if agentID == "" {
		return "main"
	}
	return agentID
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
