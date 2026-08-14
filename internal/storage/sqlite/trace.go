package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
)

func (store *Store) GetTrace(ctx context.Context, filter query.TraceFilter) (query.Trace, error) {
	traceID := filter.TraceID.String()
	summary, err := store.loadTraceSummary(ctx, traceID)
	if err != nil {
		return query.Trace{}, err
	}
	if summary.ActivityCount == 0 {
		return query.Trace{}, query.ErrTraceNotFound
	}
	offset := filter.Page.Offset()
	limit := filter.Page.Size()
	branchLimit := filter.Page.WindowEnd(offset)
	const statement = `SELECT source, signal, trace_id, span_id, parent_span_id, name,
  activity_kind, tool_name, target_agent_id, target_agent_type, content,
  agent_id, agent_definition, agent_type, parent_agent_id, run_id, model,
  started_at, ended_at, observed_at, status, cost_usd,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
  cache_write_tokens_reported, reasoning_tokens_reported, usage_role, prompt_id, usage_id
FROM (
  SELECT source, 'trace' AS signal, trace_id, span_id, parent_span_id, name,
    activity_kind, tool_name, target_agent_id, target_agent_type, content,
    agent_id, agent_definition, agent_type, parent_agent_id, run_id, model,
    started_at, ended_at, ended_at AS observed_at, status, cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), '') AS usage_role,
    COALESCE(json_extract(attributes_json, '$."gen_ai.turn.id"'), '') AS prompt_id,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.id"'), '') AS usage_id
  FROM (SELECT source, trace_id, span_id, parent_span_id, name,
    activity_kind, tool_name, target_agent_id, target_agent_type, content,
    agent_id, agent_definition, agent_type, parent_agent_id, run_id, model,
    started_at, ended_at, status, cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported, attributes_json
  FROM spans WHERE trace_id = ?
  ORDER BY started_at ASC, ended_at ASC
  LIMIT ?) AS spans
  UNION ALL
  SELECT source, 'log', trace_id, span_id, '', name,
    activity_kind, tool_name, target_agent_id, target_agent_type, body,
    agent_id, agent_definition, agent_type, parent_agent_id, run_id, model,
    observed_at, observed_at, observed_at, '', cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), ''),
    COALESCE(json_extract(attributes_json, '$."gen_ai.turn.id"'), ''),
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.id"'), '')
  FROM (SELECT source, trace_id, span_id, name,
    activity_kind, tool_name, target_agent_id, target_agent_type, body,
    agent_id, agent_definition, agent_type, parent_agent_id, run_id, model,
    observed_at, cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported, attributes_json
  FROM logs WHERE trace_id = ?
  ORDER BY observed_at ASC
  LIMIT ?) AS logs
)

ORDER BY started_at ASC, observed_at ASC, signal DESC
LIMIT ? OFFSET ?`
	rows, err := store.db.QueryContext(ctx, statement, traceID, branchLimit, traceID, branchLimit, limit, offset)
	if err != nil {
		return query.Trace{}, fmt.Errorf("query trace: %w", err)
	}
	defer rows.Close()

	activities := make([]query.Activity, 0)
	for rows.Next() {
		activity, err := store.scanActivity(rows)
		if err != nil {
			return query.Trace{}, fmt.Errorf("scan trace activity: %w", err)
		}
		activities = append(activities, activity)
	}
	if err := rows.Err(); err != nil && err != sql.ErrNoRows {
		return query.Trace{}, fmt.Errorf("iterate trace activities: %w", err)
	}
	activities = enrichActivityRelationships(enrichAgentEvidence(activities))
	usageContributions := selectUsageContributions(activities)
	for index := range activities {
		activities[index].ContributesToTotal = usageContributions[index]
	}
	trace := query.Trace{
		TraceID:            traceID,
		StartedAt:          summary.StartedAt,
		EndedAt:            summary.EndedAt,
		Status:             summary.Status,
		RootSpanCount:      summary.RootSpanCount,
		MissingParentCount: summary.MissingParentCount,
		Conversations:      summary.Conversations,
		Agents:             summary.Agents,
		Activities:         activities,
		ActivityOffset:     offset,
		ActivityCount:      summary.ActivityCount,
		HasMore:            int64(offset+len(activities)) < summary.ActivityCount,
	}
	return trace, nil
}

type traceSummary struct {
	StartedAt          time.Time
	EndedAt            time.Time
	Status             query.TraceStatus
	ActivityCount      int64
	RootSpanCount      int64
	MissingParentCount int64
	Conversations      []query.ConversationRef
	Agents             []query.TraceAgent
}

func (store *Store) loadTraceSummary(ctx context.Context, traceID string) (traceSummary, error) {
	result := traceSummary{Status: query.TraceStatusUnknown, Conversations: make([]query.ConversationRef, 0), Agents: make([]query.TraceAgent, 0)}
	var started, ended string
	var statusRank int64
	var conversationsJSON, agentsJSON string
	err := store.db.QueryRowContext(ctx, `
WITH trace_events AS MATERIALIZED (
  SELECT source, run_id, agent_id, agent_definition, agent_type, parent_agent_id, model,
    started_at, ended_at,
    CASE lower(status) WHEN 'error' THEN 2 WHEN 'ok' THEN 1 ELSE 0 END AS status_rank,
    CASE WHEN parent_span_id = '' THEN 1 ELSE 0 END AS root_span_count,
    CASE WHEN parent_span_id <> '' AND NOT EXISTS (
      SELECT 1 FROM spans AS parent
      WHERE parent.trace_id = spans.trace_id AND parent.span_id = spans.parent_span_id
    ) THEN 1 ELSE 0 END AS missing_parent_count
  FROM spans WHERE trace_id = ?
  UNION ALL
  SELECT source, run_id, agent_id, agent_definition, agent_type, parent_agent_id, model,
    observed_at, observed_at, 0, 0, 0
  FROM logs WHERE trace_id = ?
), agent_groups AS (
  SELECT source, run_id, agent_id, MAX(agent_definition) AS agent_definition,
    MAX(agent_type) AS agent_type, MAX(parent_agent_id) AS parent_agent_id, MAX(model) AS model
  FROM trace_events WHERE agent_id <> ''
  GROUP BY source, run_id, agent_id
)
SELECT COUNT(*), COALESCE(MIN(started_at), ''), COALESCE(MAX(ended_at), ''),
  COALESCE(MAX(status_rank), 0), COALESCE(SUM(root_span_count), 0), COALESCE(SUM(missing_parent_count), 0),
  COALESCE((SELECT json_group_array(json_object('sourceId', source, 'id', run_id))
    FROM (SELECT DISTINCT source, run_id FROM trace_events WHERE run_id <> '' ORDER BY source, run_id)), '[]'),
  COALESCE((SELECT json_group_array(json_object('sourceId', source, 'conversationId', run_id, 'agentId', agent_id,
      'agentDefinition', agent_definition, 'agentType', agent_type, 'parentAgentId', parent_agent_id, 'model', model))
    FROM (SELECT source, run_id, agent_id, agent_definition, agent_type, parent_agent_id, model
      FROM agent_groups ORDER BY source, run_id, agent_id)), '[]')
FROM trace_events`, traceID, traceID).Scan(
		&result.ActivityCount, &started, &ended, &statusRank, &result.RootSpanCount,
		&result.MissingParentCount, &conversationsJSON, &agentsJSON,
	)
	if err != nil {
		return traceSummary{}, fmt.Errorf("query trace summary: %w", err)
	}
	if result.ActivityCount == 0 {
		return result, nil
	}
	result.StartedAt, err = parseStorageTime(started)
	if err != nil {
		return traceSummary{}, err
	}
	result.EndedAt, err = parseStorageTime(ended)
	if err != nil {
		return traceSummary{}, err
	}
	switch statusRank {
	case 2:
		result.Status = query.TraceStatusError
	case 1:
		result.Status = query.TraceStatusOK
	}
	if err := json.Unmarshal([]byte(conversationsJSON), &result.Conversations); err != nil {
		return traceSummary{}, fmt.Errorf("decode trace conversations: %w", err)
	}
	if err := json.Unmarshal([]byte(agentsJSON), &result.Agents); err != nil {
		return traceSummary{}, fmt.Errorf("decode trace agents: %w", err)
	}
	return result, nil
}

func buildTrace(traceID string, activities []query.Activity) query.Trace {
	trace := query.Trace{
		TraceID: traceID, Status: query.TraceStatusUnknown, Activities: activities,
		Conversations: make([]query.ConversationRef, 0), Agents: make([]query.TraceAgent, 0),
	}
	spanIDs := make(map[string]struct{})
	for _, activity := range activities {
		if trace.StartedAt.IsZero() || activity.StartedAt.Before(trace.StartedAt) {
			trace.StartedAt = activity.StartedAt
		}
		if trace.EndedAt.IsZero() || activity.EndedAt.After(trace.EndedAt) {
			trace.EndedAt = activity.EndedAt
		}
		if activity.Signal == canonical.SignalTrace && activity.SpanID != "" {
			spanIDs[activity.SpanID] = struct{}{}
		}
		switch strings.ToLower(activity.Status) {
		case "error":
			trace.Status = query.TraceStatusError
		case "ok":
			if trace.Status == query.TraceStatusUnknown {
				trace.Status = query.TraceStatusOK
			}
		}
	}

	conversationSet := make(map[query.ConversationRef]struct{})
	type agentKey struct{ sourceID, conversationID, agentID string }
	agentSet := make(map[agentKey]query.TraceAgent)
	for _, activity := range activities {
		if activity.Signal == canonical.SignalTrace {
			if activity.ParentSpanID == "" {
				trace.RootSpanCount++
			} else if _, exists := spanIDs[activity.ParentSpanID]; !exists {
				trace.MissingParentCount++
			}
		}
		if activity.RunID != "" {
			conversationSet[query.ConversationRef{SourceID: activity.Source, ID: activity.RunID}] = struct{}{}
		}
		if activity.AgentID != "" {
			key := agentKey{sourceID: activity.Source, conversationID: activity.RunID, agentID: activity.AgentID}
			agentSet[key] = mergeTraceAgent(agentSet[key], query.TraceAgent{
				SourceID: activity.Source, ConversationID: activity.RunID, AgentID: activity.AgentID,
				AgentDefinition: activity.AgentDefinition, AgentType: activity.AgentType,
				ParentAgentID: activity.ParentAgentID, Model: activity.Model,
			})
		}
	}
	for conversation := range conversationSet {
		trace.Conversations = append(trace.Conversations, conversation)
	}
	sort.Slice(trace.Conversations, func(i, j int) bool {
		if trace.Conversations[i].SourceID != trace.Conversations[j].SourceID {
			return trace.Conversations[i].SourceID < trace.Conversations[j].SourceID
		}
		return trace.Conversations[i].ID < trace.Conversations[j].ID
	})
	for _, agent := range agentSet {
		trace.Agents = append(trace.Agents, agent)
	}
	sort.Slice(trace.Agents, func(i, j int) bool {
		left, right := trace.Agents[i], trace.Agents[j]
		if left.SourceID != right.SourceID {
			return left.SourceID < right.SourceID
		}
		if left.ConversationID != right.ConversationID {
			return left.ConversationID < right.ConversationID
		}
		return left.AgentID < right.AgentID
	})
	return trace
}

func mergeTraceAgent(current, observed query.TraceAgent) query.TraceAgent {
	if current.SourceID == "" {
		current.SourceID = observed.SourceID
	}
	if current.ConversationID == "" {
		current.ConversationID = observed.ConversationID
	}
	if current.AgentID == "" {
		current.AgentID = observed.AgentID
	}
	if observed.AgentDefinition != "" {
		current.AgentDefinition = observed.AgentDefinition
	}
	if observed.AgentType != "" {
		current.AgentType = observed.AgentType
	}
	if observed.ParentAgentID != "" {
		current.ParentAgentID = observed.ParentAgentID
	}
	if observed.Model != "" {
		current.Model = observed.Model
	}
	return current
}
