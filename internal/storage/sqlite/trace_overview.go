package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
)

type storedTraceOverviewActivity struct {
	activity   query.TraceOverviewActivity
	attributes map[string]any
}

func (store *Store) traceOverviewActivities(ctx context.Context, reader sqlReader, traceID string, limit int, includeOutcomeAttributes bool) ([]storedTraceOverviewActivity, error) {
	rows, err := reader.QueryContext(ctx, `SELECT activity_id, source, signal, span_id, parent_span_id, name, activity_kind,
  status, started_at, ended_at, missing_parent, attributes_json FROM (
  SELECT activity_id, source, 'trace' AS signal, span_id, parent_span_id, name, activity_kind, status,
    started_at, ended_at, CASE WHEN parent_span_id <> '' AND NOT EXISTS (
      SELECT 1 FROM spans parent WHERE parent.trace_id = child.trace_id AND parent.span_id = child.parent_span_id
    ) THEN 1 ELSE 0 END AS missing_parent,
    CASE WHEN ? THEN attributes_json ELSE '{}' END AS attributes_json,
    ended_at AS observed_at, 'span:' || trace_id || ':' || span_id AS activity_key
  FROM spans child WHERE trace_id = ?
  UNION ALL
  SELECT activity_id, source, 'log', span_id, '', name, activity_kind, '', observed_at, observed_at, 0,
    CASE WHEN ? THEN attributes_json ELSE '{}' END, observed_at, 'log:' || id
  FROM logs WHERE trace_id = ?
) ORDER BY started_at ASC, observed_at ASC, signal DESC, activity_key ASC LIMIT ?`, includeOutcomeAttributes, traceID, includeOutcomeAttributes, traceID, limit)
	if err != nil {
		return nil, fmt.Errorf("query trace overview: %w", err)
	}
	defer rows.Close()
	capacity := query.TraceOverviewLimit
	if limit >= 0 {
		capacity = min(limit, query.TraceOverviewLimit)
	}
	result := make([]storedTraceOverviewActivity, 0, capacity)
	for rows.Next() {
		var item storedTraceOverviewActivity
		var attributesJSON, signal, started, ended string
		var storedID sql.NullString
		if err := rows.Scan(&storedID, &item.activity.Source, &signal, &item.activity.SpanID, &item.activity.ParentSpanID,
			&item.activity.Name, &item.activity.Kind, &item.activity.Status, &started, &ended, &item.activity.MissingParent, &attributesJSON); err != nil {
			return nil, fmt.Errorf("scan trace overview: %w", err)
		}
		item.activity.ID = storedID.String
		item.activity.Signal = canonical.Signal(signal)
		var err error
		item.activity.StartedAt, err = parseStorageTime(started)
		if err != nil {
			return nil, err
		}
		item.activity.EndedAt, err = parseStorageTime(ended)
		if err != nil {
			return nil, err
		}
		if includeOutcomeAttributes {
			activity := query.Activity{Name: item.activity.Name, Status: item.activity.Status}
			if err := decodeAttributes(attributesJSON, &activity.Attributes); err != nil {
				return nil, err
			}
			if query.ActivityHasObservedFailure(activity) {
				item.activity.Status = string(query.TraceStatusError)
			}
		}
		switch strings.ToLower(item.activity.Status) {
		case "error", "failed", "failure":
			item.activity.Status = string(query.TraceStatusError)
		case "ok", "success", "succeeded":
			item.activity.Status = string(query.TraceStatusOK)
		default:
			item.activity.Status = string(query.TraceStatusUnknown)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trace overview: %w", err)
	}
	return result, nil
}

func decodeAttributes(encoded string, target *map[string]any) error {
	if err := json.Unmarshal([]byte(encoded), target); err != nil {
		return fmt.Errorf("decode trace outcome attributes: %w", err)
	}
	return nil
}

func (store *Store) GetTraceOverview(ctx context.Context, traceID query.TraceID) (query.TraceOverview, error) {
	transaction, err := store.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return query.TraceOverview{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	summary, err := store.loadTraceSummary(ctx, transaction, traceID.String())
	if err != nil {
		return query.TraceOverview{}, err
	}
	if summary.ActivityCount == 0 {
		return query.TraceOverview{}, query.ErrTraceNotFound
	}
	stored, err := store.traceOverviewActivities(ctx, transaction, traceID.String(), query.TraceOverviewLimit, true)
	if err != nil {
		return query.TraceOverview{}, err
	}
	activities := make([]query.TraceOverviewActivity, len(stored))
	for index := range stored {
		activities[index] = stored[index].activity
	}
	result := query.TraceOverview{TraceID: traceID.String(), StartedAt: summary.StartedAt, EndedAt: summary.EndedAt,
		TotalActivities: summary.ActivityCount, ReturnedActivities: int64(len(activities)), Coverage: query.TraceOverviewCoverageComplete, Activities: activities}
	if result.ReturnedActivities < result.TotalActivities {
		result.Coverage = query.TraceOverviewCoveragePartial
	}
	if err := transaction.Commit(); err != nil {
		return query.TraceOverview{}, err
	}
	return result, nil
}

func (store *Store) GetTraceWindow(ctx context.Context, filter query.TraceWindowFilter) (query.TraceWindowResult, error) {
	if err := query.ValidateTraceWindow(filter.Window); err != nil {
		return query.TraceWindowResult{}, err
	}
	transaction, err := store.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return query.TraceWindowResult{}, err
	}
	defer func() { _ = transaction.Rollback() }()
	summary, err := store.loadTraceSummary(ctx, transaction, filter.TraceID.String())
	if err != nil {
		return query.TraceWindowResult{}, err
	}
	if summary.ActivityCount == 0 {
		return query.TraceWindowResult{}, query.ErrTraceNotFound
	}
	stored, err := store.traceOverviewActivities(ctx, transaction, filter.TraceID.String(), -1, filter.Window.ErrorsOnly)
	if err != nil {
		return query.TraceWindowResult{}, err
	}
	matching := make([]query.TraceOverviewActivity, 0, len(stored))
	for _, item := range stored {
		if query.TraceWindowIncludes(filter.Window, item.activity) {
			matching = append(matching, item.activity)
		}
	}
	start := min(filter.Page.Offset(), len(matching))
	end := min(start+filter.Page.Size(), len(matching))
	selected := matching[start:end]
	ids := make([]string, len(selected))
	for index := range selected {
		ids[index] = selected[index].ID
	}
	byID, err := store.activitiesByID(ctx, transaction, ids)
	if err != nil {
		return query.TraceWindowResult{}, err
	}
	activities := make([]query.Activity, 0, len(ids))
	for index, id := range ids {
		activity, found := byID[id]
		if !found {
			return query.TraceWindowResult{}, fmt.Errorf("trace activity %q not found", id)
		}
		if filter.Window.ErrorsOnly {
			activity.Status = selected[index].Status
		}
		if activity.Signal == canonical.SignalTrace {
			missingParent := selected[index].MissingParent
			activity.MissingParent = &missingParent
		}
		activities = append(activities, activity)
	}
	activities = enrichActivityRelationships(enrichAgentEvidence(activities))
	contributions := selectUsageContributions(activities)
	for i := range activities {
		activities[i].ContributesToTotal = contributions[i]
	}
	trace := query.Trace{TraceID: filter.TraceID.String(), StartedAt: summary.StartedAt, EndedAt: summary.EndedAt, Status: summary.Status,
		RootSpanCount: summary.RootSpanCount, MissingParentCount: summary.MissingParentCount, Conversations: summary.Conversations,
		Agents: summary.Agents, Activities: activities, ActivityOffset: start, ActivityCount: summary.ActivityCount, HasMore: end < len(matching)}
	if err := transaction.Commit(); err != nil {
		return query.TraceWindowResult{}, err
	}
	return query.TraceWindowResult{Trace: trace, MatchingActivities: int64(len(matching))}, nil
}
