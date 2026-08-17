package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kotokumu/agentmetry/internal/query"
)

// GetConversation returns one source-qualified conversation independently of
// dashboard time filters while keeping the public activity page bounded.
func (store *Store) GetConversation(ctx context.Context, filter query.ConversationFilter) (query.Session, error) {
	transaction, err := store.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return query.Session{}, fmt.Errorf("begin conversation snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	sourceID := filter.Identity.SourceID()
	requested := sessionRef{sourceID: sourceID, sessionID: filter.Identity.ConversationID()}
	graph, err := loadSessionGroupWithReader(ctx, transaction, requested)
	if err != nil {
		return query.Session{}, err
	}
	root := graph.root(requested)
	members := graph.members(root)
	memberIDs := make([]string, len(members))
	for index, member := range members {
		memberIDs[index] = member.sessionID
	}

	session, summaryErr := store.loadSessionSummary(ctx, transaction, root, graph)
	if summaryErr != nil && !errors.Is(summaryErr, query.ErrConversationNotFound) {
		return query.Session{}, summaryErr
	}
	targetRequested := filter.Anchor.Present()
	anchorTraceID, anchorSpanID := "", ""
	if targetRequested {
		anchorTraceID = filter.Anchor.TraceID().String()
		anchorSpanID = filter.Anchor.SpanID().String()
	}
	since := formatTime(time.Time{})
	spanWhere, spanArgs := activityWhereSessions("ended_at", since, sourceID, memberIDs, "")
	logWhere, logArgs := activityWhereSessions("observed_at", since, sourceID, memberIDs, "")
	spanWhere, spanArgs = restrictMeaningfulActivity(spanWhere, spanArgs, anchorTraceID, anchorSpanID)
	logWhere, logArgs = restrictMeaningfulActivity(logWhere, logArgs, anchorTraceID, anchorSpanID)
	var total int64
	countArgs := append(spanArgs, logArgs...)
	if err := transaction.QueryRowContext(ctx, fmt.Sprintf(`SELECT
  (SELECT COUNT(*) FROM spans WHERE %s) +
  (SELECT COUNT(*) FROM logs WHERE %s)`, spanWhere, logWhere), countArgs...).Scan(&total); err != nil {
		return query.Session{}, fmt.Errorf("count conversation activities: %w", err)
	}
	if total == 0 {
		return query.Session{}, query.ErrConversationNotFound
	}

	offset := filter.Page.Offset()
	if targetRequested {
		anchorIndex, err := conversationAnchorIndex(ctx, transaction, spanWhere, spanArgs, logWhere, logArgs, anchorTraceID, anchorSpanID)
		if err != nil {
			return query.Session{}, err
		}
		if filter.PageMode == query.ConversationPageAroundAnchor {
			offset = filter.Page.OffsetAround(anchorIndex)
		}
	}
	offset = boundedOffset(int(total), offset)
	activities, err := store.activitiesWindowWithReaderSessionSelection(
		ctx, transaction, since, filter.Page.Size(), offset, sourceID, memberIDs, true, "", anchorTraceID, anchorSpanID,
	)
	if err != nil {
		return query.Session{}, err
	}
	for index := range activities {
		activities[index] = graph.normalizeActivityAgent(activities[index])
	}

	if errors.Is(summaryErr, query.ErrConversationNotFound) {
		session = query.Session{
			ID: root.sessionID, SourceID: sourceID,
			Sources: []query.TelemetrySource{store.describeSource(sourceID)},
			Agents:  make([]query.AgentSession, 0), Activities: make([]query.Activity, 0),
		}
		if len(activities) > 0 {
			session.StartedAt = activities[len(activities)-1].ObservedAt
			session.EndedAt = activities[0].ObservedAt
		}
	}
	session.ActivityCount = total
	session.ActivityOffset = offset
	session.Activities = activities
	session.HasEarlier = offset > 0
	session.HasMore = int64(offset+len(activities)) < total
	if err := transaction.Commit(); err != nil {
		return query.Session{}, fmt.Errorf("commit conversation snapshot: %w", err)
	}
	return session, nil
}

func conversationAnchorIndex(ctx context.Context, reader sqlReader, spanWhere string, spanArgs []any, logWhere string, logArgs []any, traceID, spanID string) (int, error) {
	statement := fmt.Sprintf(`WITH activity AS (
  SELECT ended_at AS observed_at, 'span:' || trace_id || ':' || span_id AS activity_key, trace_id, span_id
  FROM spans WHERE %s
  UNION ALL
  SELECT observed_at, 'log:' || id, trace_id, span_id
  FROM logs WHERE %s
), anchor AS (
  SELECT observed_at, activity_key FROM activity
  WHERE trace_id = ? AND span_id = ?
  ORDER BY observed_at DESC, activity_key ASC LIMIT 1
)
SELECT (SELECT COUNT(*) FROM anchor),
  (SELECT COUNT(*) FROM activity, anchor
   WHERE activity.observed_at > anchor.observed_at
      OR (activity.observed_at = anchor.observed_at AND activity.activity_key < anchor.activity_key))`, spanWhere, logWhere)
	args := append(spanArgs, logArgs...)
	args = append(args, traceID, spanID)
	var found, before int64
	if err := reader.QueryRowContext(ctx, statement, args...).Scan(&found, &before); err != nil {
		return 0, fmt.Errorf("query conversation activity anchor: %w", err)
	}
	if found == 0 {
		return 0, query.ErrConversationTargetNotFound
	}
	return int(before), nil
}
