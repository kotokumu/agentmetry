package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/theoden9014/agentmetry/internal/query"
)

func (store *Store) SyncSessionActivities(ctx context.Context, filter query.SessionActivitySyncFilter) (query.ActivitySyncPage, error) {
	return store.syncActivities(ctx, "session", filter.Identity.SourceID(), filter.Identity.ConversationID(), filter.ActivitySyncFilter)
}

func (store *Store) SyncTraceActivities(ctx context.Context, filter query.TraceActivitySyncFilter) (query.ActivitySyncPage, error) {
	return store.syncActivities(ctx, "trace", "", filter.TraceID.String(), filter.ActivitySyncFilter)
}

func (store *Store) syncActivities(ctx context.Context, scopeKind, source, scopeID string, filter query.ActivitySyncFilter) (query.ActivitySyncPage, error) {
	transaction, err := store.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return query.ActivitySyncPage{}, fmt.Errorf("begin activity sync snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	current, earliest, err := projectionBoundsTx(ctx, transaction)
	if err != nil {
		return query.ActivitySyncPage{}, err
	}
	through := filter.Through
	if through.Generation == "" {
		through = current
	}
	if err := query.ValidateProjectionPosition(current, filter.After); err != nil {
		return query.ActivitySyncPage{}, err
	}
	if err := query.ValidateProjectionPosition(current, through); err != nil {
		return query.ActivitySyncPage{}, err
	}
	if through.Sequence < filter.After.Sequence {
		return query.ActivitySyncPage{}, query.ErrProjectionCursorInvalid
	}
	if earliest.Valid && filter.After.Sequence < earliest.Int64-1 {
		return query.ActivitySyncPage{}, query.ErrProjectionCursorExpired
	}

	pageSize, offset := filter.Page.Size(), filter.Page.Offset()
	rows, err := transaction.QueryContext(ctx, `WITH ranked AS (
  SELECT id, sequence, activity_id, operation,
    ROW_NUMBER() OVER (PARTITION BY activity_id ORDER BY sequence DESC, id DESC) AS rank
  FROM activity_changes
  WHERE scope_kind = ? AND source = ? AND scope_id = ? AND sequence > ? AND sequence <= ?
)
SELECT activity_id, operation FROM ranked WHERE rank = 1
ORDER BY sequence ASC, id ASC LIMIT ? OFFSET ?`, scopeKind, source, scopeID, filter.After.Sequence, through.Sequence, pageSize+1, offset)
	if err != nil {
		return query.ActivitySyncPage{}, fmt.Errorf("query activity mutations: %w", err)
	}
	defer rows.Close()
	mutations := make([]query.ActivityMutation, 0, pageSize+1)
	for rows.Next() {
		var activityID, operation string
		if err := rows.Scan(&activityID, &operation); err != nil {
			return query.ActivitySyncPage{}, err
		}
		mutations = append(mutations, query.ActivityMutation{ActivityID: activityID, Operation: query.ActivityMutationOperation(operation)})
	}
	if err := rows.Err(); err != nil {
		return query.ActivitySyncPage{}, err
	}
	if err := rows.Close(); err != nil {
		return query.ActivitySyncPage{}, err
	}
	result := query.ActivitySyncPage{Mutations: mutations, Through: through, Offset: offset}
	if len(result.Mutations) > pageSize {
		result.Mutations = result.Mutations[:pageSize]
		result.HasMore = true
		result.NextOffset = offset + pageSize
	}
	upsertIDs := make([]string, 0, len(result.Mutations))
	for _, mutation := range result.Mutations {
		if mutation.Operation == query.ActivityMutationUpsert {
			upsertIDs = append(upsertIDs, mutation.ActivityID)
		}
	}
	loaded, err := store.activitiesByID(ctx, transaction, upsertIDs)
	if err != nil {
		return query.ActivitySyncPage{}, err
	}
	for index := range result.Mutations {
		mutation := &result.Mutations[index]
		if mutation.Operation != query.ActivityMutationUpsert {
			continue
		}
		activity, exists := loaded[mutation.ActivityID]
		if !exists || !activityBelongsToScope(activity, scopeKind, source, scopeID) {
			mutation.Operation = query.ActivityMutationRemove
			continue
		}
		mutation.Activity = &activity
	}
	if err := transaction.Commit(); err != nil {
		return query.ActivitySyncPage{}, fmt.Errorf("commit activity sync snapshot: %w", err)
	}
	return result, nil
}

func activityBelongsToScope(activity query.Activity, scopeKind, source, scopeID string) bool {
	if scopeKind == "session" {
		return activity.Source == source && activity.RunID == scopeID
	}
	return activity.TraceID == scopeID
}

func projectionBoundsTx(ctx context.Context, transaction *sql.Tx) (query.ProjectionPosition, sql.NullInt64, error) {
	var current query.ProjectionPosition
	var earliest sql.NullInt64
	err := transaction.QueryRowContext(ctx, `SELECT generation,
  (SELECT MIN(sequence) FROM projection_changes),
  COALESCE((SELECT MAX(sequence) FROM projection_changes), 0)
FROM projection_feed_state WHERE id = 1`).Scan(&current.Generation, &earliest, &current.Sequence)
	if err != nil {
		return query.ProjectionPosition{}, sql.NullInt64{}, fmt.Errorf("read projection bounds: %w", err)
	}
	return current, earliest, nil
}

const activityByIDStatement = `SELECT stored_activity_id, activity_key, source, signal, trace_id, span_id, parent_span_id, name,
  activity_kind, tool_name, target_agent_id, target_agent_type, content,
  agent_id, agent_definition, agent_type, parent_agent_id, run_id, model,
  started_at, ended_at, observed_at, status, cost_usd,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
  cache_write_tokens_reported, reasoning_tokens_reported, usage_role, prompt_id, usage_id
FROM (
  SELECT activity_id AS stored_activity_id, 'span:' || trace_id || ':' || span_id AS activity_key, source, 'trace' AS signal,
    trace_id, span_id, parent_span_id, name, activity_kind, tool_name, target_agent_id,
    target_agent_type, content, agent_id, agent_definition, agent_type, parent_agent_id,
    run_id, model, started_at, ended_at, ended_at AS observed_at, status, cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), '') AS usage_role,
    COALESCE(json_extract(attributes_json, '$."gen_ai.turn.id"'), '') AS prompt_id,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.id"'), '') AS usage_id
  FROM spans WHERE activity_id = ?
  UNION ALL
  SELECT activity_id, 'log:' || id, source, 'log', trace_id, span_id, '', name, activity_kind,
    tool_name, target_agent_id, target_agent_type, body, agent_id, agent_definition,
    agent_type, parent_agent_id, run_id, model, observed_at, observed_at, observed_at,
    '', cost_usd, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
    reasoning_tokens, input_tokens_reported, output_tokens_reported,
    cache_read_tokens_reported, cache_write_tokens_reported, reasoning_tokens_reported,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), ''),
    COALESCE(json_extract(attributes_json, '$."gen_ai.turn.id"'), ''),
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.id"'), '')
  FROM logs WHERE activity_id = ?
)`

func (store *Store) activitiesByID(ctx context.Context, transaction *sql.Tx, activityIDs []string) (map[string]query.Activity, error) {
	result := make(map[string]query.Activity, len(activityIDs))
	if len(activityIDs) == 0 {
		return result, nil
	}
	payload, err := json.Marshal(activityIDs)
	if err != nil {
		return nil, fmt.Errorf("encode activity identities: %w", err)
	}
	statement := strings.ReplaceAll(activityByIDStatement, "activity_id = ?", "activity_id IN (SELECT value FROM json_each(?))")
	rows, err := transaction.QueryContext(ctx, statement, string(payload), string(payload))
	if err != nil {
		return nil, fmt.Errorf("load activity mutations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		activity, err := store.scanActivity(rows)
		if err != nil {
			return nil, err
		}
		result[activity.ID] = activity
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
