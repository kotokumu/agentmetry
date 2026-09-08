package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/kotokumu/agentmetry/internal/query"
)

func (store *Store) ListTraces(ctx context.Context, filter query.TraceListFilter) (query.TracePage, error) {
	if err := validateTraceConditions(filter.Conditions); err != nil {
		return query.TracePage{}, err
	}
	transaction, err := store.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return query.TracePage{}, fmt.Errorf("begin trace list snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	minDuration := float64(0)
	minDurationEnabled := int64(0)
	if filter.Conditions.MinDurationMS != nil {
		minDuration = *filter.Conditions.MinDurationMS
		minDurationEnabled = 1
	}
	statusRank := traceFailureRank(filter.Conditions.FailureObservation)
	queryText := `SELECT trace_id, started_at, ended_at, status_rank, activity_count, root_span_count, missing_parent_count
FROM trace_rollups
WHERE (? = '' OR started_at >= ?)
  AND (? = '' OR EXISTS (SELECT 1 FROM trace_conversations c WHERE c.trace_id = trace_rollups.trace_id AND c.source = ?))
  AND (? = -1 OR status_rank = ?)
  AND (? = 0 OR (started_at <> '' AND ended_at <> '' AND (julianday(ended_at) - julianday(started_at)) * 86400000 >= ?))
ORDER BY started_at DESC, trace_id ASC
LIMIT ? OFFSET ?`
	since := ""
	if !filter.Since.IsZero() {
		since = formatTime(filter.Since)
	}
	offset := filter.Page.Offset()
	limit := filter.Page.Size()
	args := []any{since, since, filter.SourceID, filter.SourceID, statusRank, statusRank, minDurationEnabled, minDuration, limit + 1, offset}
	rows, err := transaction.QueryContext(ctx, queryText, args...)
	if err != nil {
		return query.TracePage{}, fmt.Errorf("query trace catalog: %w", err)
	}
	defer rows.Close()

	entries := make([]query.TraceListEntry, 0, limit)
	hasMore := false
	for rows.Next() {
		var traceID, started, ended string
		var statusRank, activityCount, rootCount, missingParentCount int64
		if err := rows.Scan(&traceID, &started, &ended, &statusRank, &activityCount, &rootCount, &missingParentCount); err != nil {
			return query.TracePage{}, fmt.Errorf("scan trace catalog: %w", err)
		}
		if len(entries) == limit {
			hasMore = true
			break
		}
		entry := query.TraceListEntry{
			TraceID: traceID, Status: traceStatus(statusRank), ActivityCount: activityCount,
			RootSpanCount: rootCount, MissingParentCount: missingParentCount,
		}
		if started != "" {
			value, parseErr := time.Parse(time.RFC3339Nano, started)
			if parseErr == nil {
				entry.StartedAt = &value
			}
		}
		if ended != "" {
			value, parseErr := time.Parse(time.RFC3339Nano, ended)
			if parseErr == nil {
				entry.EndedAt = &value
			}
		}
		if entry.StartedAt != nil && entry.EndedAt != nil && !entry.EndedAt.Before(*entry.StartedAt) {
			duration := entry.EndedAt.Sub(*entry.StartedAt).Seconds() * 1000
			entry.DurationMS = &duration
		}
		entry.Conversations, err = loadTraceConversations(ctx, transaction, traceID)
		if err != nil {
			return query.TracePage{}, err
		}
		entry.CostSummary, err = traceCostSummary(ctx, transaction, traceID)
		if err != nil {
			return query.TracePage{}, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return query.TracePage{}, fmt.Errorf("iterate trace catalog: %w", err)
	}
	page := query.TracePage{Traces: entries, NextOffset: filter.Page.NextOffset(len(entries)), HasMore: hasMore}
	if filter.Conditions.FailureObservation != query.TraceFailureUnspecified || filter.Conditions.MinDurationMS != nil {
		conditions := filter.Conditions
		page.AppliedConditions = &conditions
	}
	if err := transaction.Commit(); err != nil {
		return query.TracePage{}, fmt.Errorf("commit trace list snapshot: %w", err)
	}
	return page, nil
}

func validateTraceConditions(conditions query.TraceConditions) error {
	switch conditions.FailureObservation {
	case "", query.TraceFailureUnspecified, query.TraceFailureObserved, query.TraceFailureNotObserved, query.TraceFailureNotReported:
	default:
		return fmt.Errorf("unsupported trace failure observation %q", conditions.FailureObservation)
	}
	if conditions.MinDurationMS != nil && (*conditions.MinDurationMS < 0 || math.IsNaN(*conditions.MinDurationMS) || math.IsInf(*conditions.MinDurationMS, 0)) {
		return fmt.Errorf("min duration must be finite and non-negative")
	}
	return nil
}

func traceFailureRank(observation query.TraceFailureObservation) int64 {
	switch observation {
	case query.TraceFailureObserved:
		return 2
	case query.TraceFailureNotObserved:
		return 1
	case query.TraceFailureNotReported:
		return 0
	default:
		return -1
	}
}

func traceStatus(rank int64) query.TraceStatus {
	switch rank {
	case 2:
		return query.TraceStatusError
	case 1:
		return query.TraceStatusOK
	default:
		return query.TraceStatusUnknown
	}
}

func loadTraceConversations(ctx context.Context, reader sqlReader, traceID string) ([]query.ConversationRef, error) {
	rows, err := reader.QueryContext(ctx, `SELECT source, run_id FROM trace_conversations WHERE trace_id = ? ORDER BY source, run_id`, traceID)
	if err != nil {
		return nil, fmt.Errorf("query trace conversations: %w", err)
	}
	defer rows.Close()
	var result []query.ConversationRef
	for rows.Next() {
		var source, runID string
		if err := rows.Scan(&source, &runID); err != nil {
			return nil, fmt.Errorf("scan trace conversation: %w", err)
		}
		result = append(result, query.ConversationRef{SourceID: source, ID: runID})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trace conversations: %w", err)
	}
	return result, nil
}
