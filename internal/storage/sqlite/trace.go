package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
)

func (store *Store) GetTrace(ctx context.Context, filter query.TraceFilter) (query.Trace, error) {
	traceID, err := query.ParseTraceID(filter.TraceID)
	if err != nil {
		return query.Trace{}, err
	}
	const statement = `SELECT source, signal, trace_id, span_id, parent_span_id, name,
  activity_kind, tool_name, target_agent_id, target_agent_type, content,
  agent_id, agent_definition, agent_type, parent_agent_id, run_id, model,
  started_at, ended_at, observed_at, status, cost_usd,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
  cache_write_tokens_reported, reasoning_tokens_reported, usage_role, usage_id
FROM (
  SELECT source, 'trace' AS signal, trace_id, span_id, parent_span_id, name,
    activity_kind, tool_name, target_agent_id, target_agent_type, content,
    agent_id, agent_definition, agent_type, parent_agent_id, run_id, model,
    started_at, ended_at, ended_at AS observed_at, status, cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), '') AS usage_role,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.id"'), '') AS usage_id
  FROM spans WHERE trace_id = ?
  UNION ALL
  SELECT source, 'log', trace_id, span_id, '', name,
    activity_kind, tool_name, target_agent_id, target_agent_type, body,
    agent_id, agent_definition, agent_type, parent_agent_id, run_id, model,
    observed_at, observed_at, observed_at, '', cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), ''),
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.id"'), '')
  FROM logs WHERE trace_id = ?
)
ORDER BY started_at ASC, observed_at ASC, signal DESC`
	rows, err := store.db.QueryContext(ctx, statement, traceID, traceID)
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
	if len(activities) == 0 {
		return query.Trace{}, query.ErrTraceNotFound
	}
	activities = enrichAgentEvidence(activities)
	usageContributions := selectUsageContributions(activities)
	for index := range activities {
		activities[index].ContributesToTotal = usageContributions[index]
	}
	return buildTrace(traceID, activities), nil
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
