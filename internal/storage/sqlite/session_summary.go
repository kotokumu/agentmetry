package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
)

// loadSessionSummary reads only aggregate columns and agent metadata. It does
// not materialize operation content; operation evidence is owned by
// ListSessionActivities.
func (store *Store) loadSessionSummary(ctx context.Context, reader sqlReader, root sessionRef, graph sessionGraph) (query.Session, error) {
	members := graph.members(root)
	predicate, memberArgs := sessionMembershipPredicate("run_id", members)
	aggregate := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM(activity_count), 0),
  COALESCE(MIN(started_at), ''), COALESCE(MAX(ended_at), ''),
  COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
  COALESCE(SUM(reasoning_tokens), 0),
	COALESCE(MAX(input_reported), 0), COALESCE(MAX(output_reported), 0),
	COALESCE(MAX(cache_read_reported), 0), COALESCE(MAX(cache_write_reported), 0),
	COALESCE(MAX(reasoning_reported), 0), COALESCE(MAX(cost_reported), 0),
	COALESCE(SUM(cost_usd), 0)
FROM session_rollups WHERE source = ? AND %s`, predicate)
	aggregateArgs := append([]any{root.sourceID}, memberArgs...)
	var rollupCount int64
	var count int64
	var startedAt, endedAt string
	var input, output, cacheRead, cacheWrite, reasoning int64
	var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported int
	var costReported bool
	var cost float64
	if err := reader.QueryRowContext(ctx, aggregate, aggregateArgs...).Scan(
		&rollupCount, &count, &startedAt, &endedAt, &input, &output, &cacheRead, &cacheWrite, &reasoning,
		&inputReported, &outputReported, &cacheReadReported, &cacheWriteReported, &reasoningReported,
		&costReported, &cost,
	); err != nil {
		return query.Session{}, fmt.Errorf("query session aggregate: %w", err)
	}
	if rollupCount == 0 {
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
		ID: root.sessionID, SourceID: root.sourceID, Sources: []query.TelemetrySource{store.describeSource(root.sourceID)},
		StartedAt: started, EndedAt: ended, ActivityCount: count,
		Tokens: aggregateTokens(input, output, cacheRead, cacheWrite, reasoning, inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported),
		Agents: make([]query.AgentSession, 0), Activities: make([]query.Activity, 0),
	}
	session.AgentCount = 0
	if costReported {
		session.CostUSD = &cost
	}

	agentQuery := fmt.Sprintf(`SELECT run_id, agent_id, agent_definition, agent_type,
  parent_agent_id, model, activity_count, input_tokens, output_tokens, cache_read_tokens,
  cache_write_tokens, reasoning_tokens, input_reported, output_reported, cache_read_reported,
  cache_write_reported, reasoning_reported
FROM session_agents WHERE source = ? AND %s ORDER BY run_id, agent_id`, predicate)
	agentArgs := append([]any{root.sourceID}, memberArgs...)
	agentRows, err := reader.QueryContext(ctx, agentQuery, agentArgs...)
	if err != nil {
		return query.Session{}, fmt.Errorf("query session agents: %w", err)
	}
	agents := make(map[string]*query.AgentSession)
	type agentOrigin struct{ runID, nativeAgentID string }
	origins := make(map[string]agentOrigin)
	nativeByRun := make(map[string][]query.AgentSession)
	grouped := len(members) > 1
	for agentRows.Next() {
		var agent query.AgentSession
		var runID, nativeAgentID string
		var input, output, cacheRead, cacheWrite, reasoning int64
		var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported int
		if err := agentRows.Scan(&runID, &nativeAgentID, &agent.AgentDefinition, &agent.AgentType, &agent.ParentAgentID, &agent.Model, &agent.ActivityCount,
			&input, &output, &cacheRead, &cacheWrite, &reasoning, &inputReported, &outputReported, &cacheReadReported, &cacheWriteReported, &reasoningReported); err != nil {
			return query.Session{}, fmt.Errorf("scan session agent: %w", err)
		}
		nativeAgent := agent
		nativeAgent.AgentID = sessionAgentID(nativeAgentID)
		nativeByRun[runID] = append(nativeByRun[runID], nativeAgent)
		ref := sessionRef{sourceID: root.sourceID, sessionID: runID}
		agent.AgentID = graph.effectiveAgentID(ref, nativeAgentID)
		if isPrimarySessionAgent(ref, nativeAgentID) && agent.AgentType == "" && (!grouped || runID == root.sessionID) {
			agent.AgentType = "root"
		}
		agent.Tokens = aggregateTokens(input, output, cacheRead, cacheWrite, reasoning, inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported)
		key := agent.AgentID
		if existing := agents[key]; existing != nil {
			existing.ActivityCount += agent.ActivityCount
			addTokens(&existing.Tokens, agent.Tokens)
			if existing.AgentDefinition == "" {
				existing.AgentDefinition = agent.AgentDefinition
			}
			if existing.AgentType == "" {
				existing.AgentType = agent.AgentType
			}
			if existing.ParentAgentID == "" {
				existing.ParentAgentID = agent.ParentAgentID
			}
			if existing.Model == "" {
				existing.Model = agent.Model
			}
			continue
		}
		agentCopy := agent
		agents[key] = &agentCopy
		origins[key] = agentOrigin{runID: runID, nativeAgentID: nativeAgentID}
	}
	if err := agentRows.Err(); err != nil {
		return query.Session{}, fmt.Errorf("iterate session agents: %w", err)
	}
	if err := agentRows.Close(); err != nil {
		return query.Session{}, fmt.Errorf("close session agents: %w", err)
	}
	parentsByRun := make(map[string]map[string]string)
	for key, agent := range agents {
		origin := origins[key]
		ref := sessionRef{sourceID: root.sourceID, sessionID: origin.runID}
		primary := isPrimarySessionAgent(ref, origin.nativeAgentID)
		if agent.ParentAgentID == "" && !primary {
			parents := parentsByRun[origin.runID]
			if parents == nil {
				var err error
				parents, err = store.inferAgentParents(ctx, reader, root.sourceID, origin.runID, nativeByRun[origin.runID])
				if err != nil {
					return query.Session{}, err
				}
				parentsByRun[origin.runID] = parents
			}
			agent.ParentAgentID = parents[sessionAgentID(origin.nativeAgentID)]
		}
		if agent.ParentAgentID != "" {
			agent.ParentAgentID = graph.effectiveParentAgentID(ref, agent.ParentAgentID)
		} else if grouped && primary {
			if parent, exists := graph.parent(ref); exists {
				agent.ParentAgentID = parent.sessionID
			}
		} else if grouped {
			agent.ParentAgentID = ref.sessionID
		}
		session.Agents = append(session.Agents, *agent)
	}
	sort.Slice(session.Agents, func(i, j int) bool { return session.Agents[i].AgentID < session.Agents[j].AgentID })
	session.AgentCount = int64(len(session.Agents))

	traceQuery := fmt.Sprintf(`SELECT DISTINCT trace_id FROM session_traces WHERE source = ? AND %s ORDER BY trace_id`, predicate)
	traceArgs := append([]any{root.sourceID}, memberArgs...)
	traceRows, err := reader.QueryContext(ctx, traceQuery, traceArgs...)
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

func sessionMembershipPredicate(column string, members []sessionRef) (string, []any) {
	identities := make([]string, len(members))
	for index, member := range members {
		identities[index] = member.sessionID
	}
	payload, _ := json.Marshal(identities)
	return column + " IN (SELECT value FROM json_each(?))", []any{string(payload)}
}

// inferAgentParents reconstructs agent delegation when a producer omits
// gen_ai.agent.parent.id. Span parentage still records which agent's span
// contained the child agent's model/tool span, so the query projection can
// expose a useful topology without loading the whole session. Only agents that
// still lack an explicit parent are inspected, with bounded evidence and
// ancestry depth. Both lookups use composite/primary-key indexes.
func (store *Store) inferAgentParents(ctx context.Context, reader sqlReader, sourceID, sessionID string, agents []query.AgentSession) (map[string]string, error) {
	const evidenceLimitPerAgent = 8
	remainingEvidence := 256
	parents := make(map[string]string)
	for _, agent := range agents {
		if agent.AgentID == "main" || agent.ParentAgentID != "" || remainingEvidence == 0 {
			continue
		}
		limit := min(evidenceLimitPerAgent, remainingEvidence)
		// Reserve the attempt before querying. Agents without parent evidence
		// must still consume the global budget; otherwise a session with many
		// evidence-free agents can issue one query per agent.
		remainingEvidence -= limit
		rows, err := reader.QueryContext(ctx, `SELECT trace_id, parent_span_id FROM spans
WHERE source = ? AND run_id = ? AND agent_id = ? AND parent_span_id > ''
LIMIT ?`, sourceID, sessionID, agent.AgentID, limit)
		if err != nil {
			return nil, fmt.Errorf("query bounded agent parent evidence: %w", err)
		}
		type candidate struct{ traceID, parentSpanID string }
		candidates := make([]candidate, 0, limit)
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
