package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
)

// loadSessionSummary reads only aggregate columns and agent metadata. It does
// not materialize operation content; operation evidence is owned by
// ListSessionActivities.
func (store *Store) loadSessionSummary(ctx context.Context, sourceID, sessionID string) (query.Session, error) {
	var count int64
	var startedAt, endedAt string
	var input, output, cacheRead, cacheWrite, reasoning int64
	var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported int
	var costReported bool
	var cost float64
	if err := store.readDB.QueryRowContext(ctx, `SELECT activity_count, started_at, ended_at,
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

	agentRows, err := store.readDB.QueryContext(ctx, `SELECT agent_id, agent_definition, agent_type,
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
	parents, err := store.inferAgentParents(ctx, sourceID, sessionID)
	if err != nil {
		return query.Session{}, err
	}
	for index := range session.Agents {
		if session.Agents[index].ParentAgentID == "" {
			session.Agents[index].ParentAgentID = parents[session.Agents[index].AgentID]
		}
	}
	session.AgentCount = int64(len(session.Agents))

	traceRows, err := store.readDB.QueryContext(ctx, `SELECT trace_id FROM session_traces WHERE source = ? AND run_id = ? ORDER BY trace_id`, sourceID, sessionID)
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

type sessionSpanEvidence struct {
	traceID       string
	spanID        string
	parentSpanID  string
	agentID       string
	parentAgentID string
}

// inferAgentParents reconstructs agent delegation when a producer omits
// gen_ai.agent.parent.id. Span parentage still records which agent's span
// contained the child agent's model/tool span, so the query projection can
// expose a useful topology without materializing operation bodies.
func (store *Store) inferAgentParents(ctx context.Context, sourceID, sessionID string) (map[string]string, error) {
	rows, err := store.readDB.QueryContext(ctx, `SELECT trace_id, span_id, parent_span_id, agent_id, parent_agent_id
FROM spans WHERE source = ? AND run_id = ? ORDER BY trace_id, span_id`, sourceID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query agent span relationships: %w", err)
	}
	defer rows.Close()

	spans := make(map[string]sessionSpanEvidence)
	explicitParents := make(map[string]string)
	for rows.Next() {
		var evidence sessionSpanEvidence
		if err := rows.Scan(&evidence.traceID, &evidence.spanID, &evidence.parentSpanID, &evidence.agentID, &evidence.parentAgentID); err != nil {
			return nil, fmt.Errorf("scan agent span relationship: %w", err)
		}
		if evidence.traceID == "" || evidence.spanID == "" {
			continue
		}
		spans[spanKey(evidence.traceID, evidence.spanID)] = evidence
		child := sessionAgentID(evidence.agentID)
		if evidence.parentAgentID != "" && evidence.parentAgentID != child {
			explicitParents[child] = evidence.parentAgentID
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent span relationships: %w", err)
	}

	agentIDs := make([]string, 0, len(spans))
	for _, evidence := range spans {
		agentID := sessionAgentID(evidence.agentID)
		if agentID != "main" {
			agentIDs = append(agentIDs, agentID)
		}
	}
	sort.Strings(agentIDs)
	parents := make(map[string]string, len(explicitParents))
	for agentID, parentID := range explicitParents {
		parents[agentID] = parentID
	}
	for _, agentID := range agentIDs {
		if parents[agentID] != "" {
			continue
		}
		for _, evidence := range spans {
			if sessionAgentID(evidence.agentID) != agentID {
				continue
			}
			parentID := nearestAncestorAgent(spans, evidence.traceID, evidence.parentSpanID, agentID)
			if parentID != "" {
				parents[agentID] = parentID
				break
			}
		}
	}
	return parents, nil
}

func nearestAncestorAgent(spans map[string]sessionSpanEvidence, traceID, parentSpanID, childAgentID string) string {
	visited := make(map[string]struct{})
	for parentSpanID != "" {
		key := spanKey(traceID, parentSpanID)
		if _, ok := visited[key]; ok {
			return ""
		}
		visited[key] = struct{}{}
		parent, ok := spans[key]
		if !ok {
			return ""
		}
		parentAgentID := sessionAgentID(parent.agentID)
		if parentAgentID != childAgentID {
			return parentAgentID
		}
		parentSpanID = parent.parentSpanID
	}
	return ""
}

func spanKey(traceID, spanID string) string { return traceID + "\x00" + spanID }

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
