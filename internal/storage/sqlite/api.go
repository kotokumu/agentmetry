package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
)

func (store *Store) GetDashboard(ctx context.Context, filter query.DashboardFilter) (query.Overview, error) {
	since := formatTime(filter.Since)
	var dashboard query.Overview
	if err := store.readDB.QueryRowContext(ctx, `WITH grouped AS (
  SELECT r.source, COALESCE(m.root_session_id, r.run_id) AS root_id, MAX(r.ended_at) AS ended_at,
    SUM(r.trace_count) AS trace_count, SUM(r.log_count) AS log_count
  FROM session_rollups r
  LEFT JOIN session_memberships m ON m.source = r.source AND m.session_id = r.run_id
  WHERE (? = '' OR r.source = ?)
  GROUP BY r.source, COALESCE(m.root_session_id, r.run_id)
)
SELECT COALESCE(SUM(trace_count), 0), COALESCE(SUM(log_count), 0)
FROM grouped WHERE ended_at >= ?`, filter.SourceID, filter.SourceID, since).Scan(
		&dashboard.SignalCounts.Traces, &dashboard.SignalCounts.Logs,
	); err != nil {
		return query.Overview{}, fmt.Errorf("query dashboard signal counts: %w", err)
	}
	if err := store.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM metrics WHERE observed_at >= ? AND (? = '' OR source = ?)`, since, filter.SourceID, filter.SourceID).Scan(&dashboard.SignalCounts.Metrics); err != nil {
		return query.Overview{}, fmt.Errorf("query dashboard metric count: %w", err)
	}

	var err error
	dashboard.Sources, err = store.dashboardSources(ctx, since, filter.SourceID)
	if err != nil {
		return query.Overview{}, err
	}
	dashboard.RunCount, dashboard.AgentCount, dashboard.Tokens, err = store.dashboardAggregates(ctx, since, filter.SourceID, filter.Search)
	if err != nil {
		return query.Overview{}, err
	}
	dashboard.RecentActivity, err = store.activities(ctx, since, 50, filter.SourceID, "")
	if err != nil {
		return query.Overview{}, err
	}
	graph, err := store.loadSessionGraph(ctx, filter.SourceID)
	if err != nil {
		return query.Overview{}, err
	}
	for index := range dashboard.RecentActivity {
		dashboard.RecentActivity[index] = graph.normalizeActivityAgent(dashboard.RecentActivity[index])
	}
	dashboard.PlanUsage, err = store.LatestPlanUsage(ctx)
	if err != nil {
		return query.Overview{}, err
	}
	return dashboard, nil
}

func (store *Store) ListSessions(ctx context.Context, filter query.SessionListFilter) (query.SessionPage, error) {
	var matchedRoots map[sessionRef]struct{}
	if strings.TrimSpace(filter.Search) != "" {
		graph, err := store.loadSessionGraph(ctx, filter.SourceID)
		if err != nil {
			return query.SessionPage{}, err
		}
		matchedRoots, err = store.searchSessionRoots(ctx, filter, graph)
		if err != nil {
			return query.SessionPage{}, err
		}
	}
	return store.listSessionsFromRollups(ctx, filter, matchedRoots)
}

func (store *Store) searchSessionRoots(ctx context.Context, filter query.SessionListFilter, graph sessionGraph) (map[sessionRef]struct{}, error) {
	branches, args := summaryBranches(formatTime(time.Unix(0, 0)), filter.SourceID, "", filter.Search)
	rows, err := store.readDB.QueryContext(ctx, fmt.Sprintf(`WITH activity AS (
%s
)
SELECT DISTINCT source, run_id FROM activity`, branches), args...)
	if err != nil {
		return nil, fmt.Errorf("query matching sessions: %w", err)
	}
	defer rows.Close()
	matching := make(map[sessionRef]struct{})
	for rows.Next() {
		var ref sessionRef
		if err := rows.Scan(&ref.sourceID, &ref.sessionID); err != nil {
			return nil, fmt.Errorf("scan matching session: %w", err)
		}
		matching[graph.root(ref)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matching sessions: %w", err)
	}
	activeRows, err := store.readDB.QueryContext(ctx, `SELECT r.source, COALESCE(m.root_session_id, r.run_id) AS root_id
FROM session_rollups r
LEFT JOIN session_memberships m ON m.source = r.source AND m.session_id = r.run_id
WHERE (? = '' OR r.source = ?)
GROUP BY r.source, COALESCE(m.root_session_id, r.run_id)
HAVING MAX(r.ended_at) >= ?`, filter.SourceID, filter.SourceID, formatTime(filter.Since))
	if err != nil {
		return nil, fmt.Errorf("query active session roots: %w", err)
	}
	defer activeRows.Close()
	matched := make(map[sessionRef]struct{})
	for activeRows.Next() {
		var ref sessionRef
		if err := activeRows.Scan(&ref.sourceID, &ref.sessionID); err != nil {
			return nil, fmt.Errorf("scan active session root: %w", err)
		}
		if _, exists := matching[ref]; exists {
			matched[ref] = struct{}{}
		}
	}
	if err := activeRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active session roots: %w", err)
	}
	return matched, nil
}

func (store *Store) listSessionsFromRollups(ctx context.Context, filter query.SessionListFilter, matchedRoots map[sessionRef]struct{}) (query.SessionPage, error) {
	pageSize := filter.Page.Size()
	offset := filter.Page.Offset()
	requestedSession := filter.SessionID
	matchedJSON := ""
	if matchedRoots != nil {
		keys := make([]string, 0, len(matchedRoots))
		for ref := range matchedRoots {
			keys = append(keys, ref.sourceID+"\x00"+ref.sessionID)
		}
		payload, _ := json.Marshal(keys)
		matchedJSON = string(payload)
	}
	rows, err := store.readDB.QueryContext(ctx, `WITH grouped AS (
  SELECT r.source, COALESCE(m.root_session_id, r.run_id) AS root_id,
    MIN(r.started_at) AS started_at, MAX(r.ended_at) AS ended_at,
    SUM(r.activity_count) AS activity_count, SUM(r.agent_count) AS agent_count
  FROM session_rollups r
  LEFT JOIN session_memberships m ON m.source = r.source AND m.session_id = r.run_id
  WHERE (? = '' OR r.source = ?)
  GROUP BY r.source, COALESCE(m.root_session_id, r.run_id)
)
SELECT source, root_id, started_at, ended_at, activity_count, agent_count
FROM grouped
WHERE ended_at >= ?
  AND (? = '' OR root_id = ? OR root_id = COALESCE(
    (SELECT root_session_id FROM session_memberships sm WHERE sm.source = grouped.source AND sm.session_id = ?), ?))
  AND (? = '' OR source || char(0) || root_id IN (SELECT value FROM json_each(?)))
ORDER BY ended_at DESC, source ASC, root_id ASC
LIMIT ? OFFSET ?`, filter.SourceID, filter.SourceID, formatTime(filter.Since), requestedSession, requestedSession, requestedSession, requestedSession,
		matchedJSON, matchedJSON, pageSize+1, offset)
	if err != nil {
		return query.SessionPage{}, fmt.Errorf("query session rollups: %w", err)
	}
	defer rows.Close()
	sessions := make([]query.Session, 0, pageSize+1)
	for rows.Next() {
		var sourceID, sessionID string
		var startedAt, endedAt string
		var activityCount, agentCount int64
		if err := rows.Scan(&sourceID, &sessionID, &startedAt, &endedAt, &activityCount, &agentCount); err != nil {
			return query.SessionPage{}, fmt.Errorf("scan session rollup: %w", err)
		}
		started, err := parseStorageTime(startedAt)
		if err != nil {
			return query.SessionPage{}, err
		}
		ended, err := parseStorageTime(endedAt)
		if err != nil {
			return query.SessionPage{}, err
		}
		sessions = append(sessions, query.Session{
			ID: sessionID, SourceID: sourceID, Sources: []query.TelemetrySource{store.describeSource(sourceID)},
			StartedAt: started, EndedAt: ended, ActivityCount: activityCount, AgentCount: agentCount,
			Agents: make([]query.AgentSession, 0), Activities: make([]query.Activity, 0),
		})
	}
	if err := rows.Err(); err != nil {
		return query.SessionPage{}, fmt.Errorf("iterate session rollups: %w", err)
	}
	page := query.SessionPage{Sessions: sessions[:min(len(sessions), pageSize)]}
	if len(sessions) > pageSize {
		page.HasMore = true
		page.NextOffset = filter.Page.NextOffset(len(page.Sessions))
	}
	return page, nil
}

func (store *Store) GetSessionSummary(ctx context.Context, identity query.ConversationIdentity) (query.Session, error) {
	transaction, err := store.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return query.Session{}, fmt.Errorf("begin session summary snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	ref := sessionRef{sourceID: identity.SourceID(), sessionID: identity.ConversationID()}
	graph, err := loadSessionGroupWithReader(ctx, transaction, ref)
	if err != nil {
		return query.Session{}, err
	}
	session, err := store.loadSessionSummary(ctx, transaction, graph.root(ref), graph)
	if err != nil {
		return query.Session{}, err
	}
	if err := transaction.Commit(); err != nil {
		return query.Session{}, fmt.Errorf("commit session summary snapshot: %w", err)
	}
	return session, nil
}

func (store *Store) ListSessionActivities(ctx context.Context, filter query.ActivityPageFilter) (query.ActivityPage, error) {
	transaction, err := store.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return query.ActivityPage{}, fmt.Errorf("begin session activity snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	sourceID := filter.Identity.SourceID()
	conversationID := filter.Identity.ConversationID()
	ref := sessionRef{sourceID: sourceID, sessionID: conversationID}
	graph, err := loadSessionGroupWithReader(ctx, transaction, ref)
	if err != nil {
		return query.ActivityPage{}, err
	}
	root := graph.root(ref)
	members := graph.members(root)
	memberIDs := make([]string, len(members))
	for index, member := range members {
		memberIDs[index] = member.sessionID
	}
	since := formatTime(time.Unix(0, 0))
	spanWhere, spanArgs := activityWhereSessions("ended_at", since, sourceID, memberIDs, filter.AgentID)
	logWhere, logArgs := activityWhereSessions("observed_at", since, sourceID, memberIDs, filter.AgentID)
	spanWhere += " AND activity_kind <> 'unknown'"
	logWhere += " AND activity_kind <> 'unknown'"
	var total int64
	countArgs := append(spanArgs, logArgs...)
	if err := transaction.QueryRowContext(ctx, fmt.Sprintf(`SELECT
  (SELECT COUNT(*) FROM spans WHERE %s) +
  (SELECT COUNT(*) FROM logs WHERE %s)`, spanWhere, logWhere), countArgs...).Scan(&total); err != nil {
		return query.ActivityPage{}, fmt.Errorf("count grouped session activities: %w", err)
	}
	if total == 0 {
		return query.ActivityPage{}, query.ErrConversationNotFound
	}
	offset := filter.Page.Offset()
	if offset == 0 && filter.Anchor.Present() {
		offset, err = store.groupAnchorOffset(ctx, transaction, sourceID, memberIDs, filter.AgentID,
			filter.Anchor.TraceID().String(), filter.Anchor.SpanID().String(), filter.Page)
		if err != nil {
			return query.ActivityPage{}, err
		}
	}
	offset = boundedOffset(int(total), offset)
	activities, err := store.activitiesWindowWithReaderSessions(ctx, transaction, since, filter.Page.Size(), offset, sourceID, memberIDs, true, filter.AgentID)
	if err != nil {
		return query.ActivityPage{}, err
	}
	for index := range activities {
		activities[index] = graph.normalizeActivityAgent(activities[index])
	}
	result := query.ActivityPage{
		Activities: activities, Total: total, Offset: offset,
		HasEarlier: offset > 0, HasMore: int64(offset+len(activities)) < total,
	}
	if err := transaction.Commit(); err != nil {
		return query.ActivityPage{}, fmt.Errorf("commit session activity snapshot: %w", err)
	}
	return result, nil
}

func (store *Store) GetSessionRework(ctx context.Context, identity query.ConversationIdentity) (query.SessionRework, error) {
	transaction, err := store.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return query.SessionRework{}, fmt.Errorf("begin session rework snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	ref := sessionRef{sourceID: identity.SourceID(), sessionID: identity.ConversationID()}
	graph, err := loadSessionGroupWithReader(ctx, transaction, ref)
	if err != nil {
		return query.SessionRework{}, err
	}
	root := graph.root(ref)
	summary, err := store.loadSessionSummary(ctx, transaction, root, graph)
	if err != nil {
		return query.SessionRework{}, err
	}
	activities, err := store.loadSessionReworkActivities(ctx, transaction, root, graph)
	if err != nil {
		return query.SessionRework{}, err
	}
	if err := transaction.Commit(); err != nil {
		return query.SessionRework{}, fmt.Errorf("commit session rework snapshot: %w", err)
	}
	return query.SessionRework{
		SourceID: root.sourceID,
		RunID:    root.sessionID,
		Report:   query.AnalyzeRework(summary, activities),
	}, nil
}

func (store *Store) loadSessionReworkActivities(ctx context.Context, reader sqlReader, root sessionRef, graph sessionGraph) ([]query.Activity, error) {
	members := graph.members(root)
	memberIDs := make([]string, len(members))
	for index, member := range members {
		memberIDs[index] = member.sessionID
	}
	activities, err := store.activitiesWindowWithReaderSessions(
		ctx, reader, formatTime(time.Unix(0, 0)), -1, 0, root.sourceID, memberIDs, true, "",
	)
	if err != nil {
		return nil, err
	}
	if len(activities) == 0 {
		return nil, query.ErrConversationNotFound
	}
	for index := range activities {
		activities[index] = graph.normalizeActivityAgent(activities[index])
	}
	usageContributions := selectUsageContributions(activities)
	for index := range activities {
		activities[index].ContributesToTotal = usageContributions[index]
	}
	return activities, nil
}

func (store *Store) groupAnchorOffset(ctx context.Context, reader sqlReader, sourceID string, memberIDs []string, agentID, traceID, spanID string, page query.Page) (int, error) {
	since := formatTime(time.Unix(0, 0))
	spanWhere, spanArgs := activityWhereSessions("ended_at", since, sourceID, memberIDs, agentID)
	logWhere, logArgs := activityWhereSessions("observed_at", since, sourceID, memberIDs, agentID)
	spanWhere += " AND activity_kind <> 'unknown'"
	logWhere += " AND activity_kind <> 'unknown'"
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
		return 0, fmt.Errorf("query grouped activity anchor: %w", err)
	}
	if found == 0 {
		return 0, query.ErrConversationTargetNotFound
	}
	return page.OffsetAround(int(before)), nil
}

func (store *Store) dashboardSources(ctx context.Context, since, sourceID string) ([]query.TelemetrySource, error) {
	rows, err := store.readDB.QueryContext(ctx, `SELECT source FROM (
  SELECT DISTINCT source FROM spans WHERE ended_at >= ? AND (? = '' OR source = ?)
  UNION
  SELECT DISTINCT source FROM logs WHERE observed_at >= ? AND (? = '' OR source = ?)
  UNION
  SELECT DISTINCT source FROM metrics WHERE observed_at >= ? AND (? = '' OR source = ?)
) ORDER BY source`, since, sourceID, sourceID, since, sourceID, sourceID, since, sourceID, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query dashboard sources: %w", err)
	}
	defer rows.Close()
	var result []query.TelemetrySource
	for rows.Next() {
		var sourceID string
		if err := rows.Scan(&sourceID); err != nil {
			return nil, err
		}
		result = append(result, store.describeSource(sourceID))
	}
	return result, rows.Err()
}

func (store *Store) dashboardAggregates(ctx context.Context, since, sourceID, search string) (runCount, agentCount int64, tokens canonical.TokenUsage, err error) {
	matchedJSON := ""
	if strings.TrimSpace(search) != "" {
		graph, graphErr := store.loadSessionGraph(ctx, sourceID)
		if graphErr != nil {
			return 0, 0, canonical.TokenUsage{}, graphErr
		}
		parsedSince, parseErr := parseStorageTime(since)
		if parseErr != nil {
			return 0, 0, canonical.TokenUsage{}, parseErr
		}
		matched, searchErr := store.searchSessionRoots(ctx, query.SessionListFilter{Since: parsedSince, SourceID: sourceID, Search: search}, graph)
		if searchErr != nil {
			return 0, 0, canonical.TokenUsage{}, searchErr
		}
		keys := make([]string, 0, len(matched))
		for ref := range matched {
			keys = append(keys, ref.sourceID+"\x00"+ref.sessionID)
		}
		payload, _ := json.Marshal(keys)
		matchedJSON = string(payload)
	}
	statement := `WITH grouped AS (
  SELECT r.source, COALESCE(m.root_session_id, r.run_id) AS root_id,
    MAX(r.ended_at) AS ended_at, SUM(r.agent_count) AS agent_count,
    SUM(r.input_tokens) AS input_tokens, SUM(r.output_tokens) AS output_tokens,
    SUM(r.cache_read_tokens) AS cache_read_tokens, SUM(r.cache_write_tokens) AS cache_write_tokens,
    SUM(r.reasoning_tokens) AS reasoning_tokens, MAX(r.input_reported) AS input_reported,
    MAX(r.output_reported) AS output_reported, MAX(r.cache_read_reported) AS cache_read_reported,
    MAX(r.cache_write_reported) AS cache_write_reported, MAX(r.reasoning_reported) AS reasoning_reported
  FROM session_rollups r
  LEFT JOIN session_memberships m ON m.source = r.source AND m.session_id = r.run_id
  WHERE (? = '' OR r.source = ?)
  GROUP BY r.source, COALESCE(m.root_session_id, r.run_id)
), selected AS (
  SELECT * FROM grouped WHERE ended_at >= ?
    AND (? = '' OR source || char(0) || root_id IN (SELECT value FROM json_each(?)))
)
SELECT COUNT(*), COALESCE(SUM(agent_count), 0),
  COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
  COALESCE(SUM(reasoning_tokens), 0), COALESCE(MAX(input_reported), 0),
  COALESCE(MAX(output_reported), 0), COALESCE(MAX(cache_read_reported), 0),
  COALESCE(MAX(cache_write_reported), 0), COALESCE(MAX(reasoning_reported), 0)
FROM selected`
	var input, output, cacheRead, cacheWrite, reasoning int64
	var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported int
	err = store.readDB.QueryRowContext(ctx, statement, sourceID, sourceID, since, matchedJSON, matchedJSON).Scan(&runCount, &agentCount, &input, &output, &cacheRead, &cacheWrite, &reasoning, &inputReported, &outputReported, &cacheReadReported, &cacheWriteReported, &reasoningReported)
	if err != nil {
		return 0, 0, canonical.TokenUsage{}, fmt.Errorf("query dashboard aggregates: %w", err)
	}
	return runCount, agentCount, aggregateTokens(input, output, cacheRead, cacheWrite, reasoning, inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported), nil
}

func summaryBranches(since, sourceID, sessionID, search string) (string, []any) {
	searchValue := strings.ToLower(strings.TrimSpace(search))
	spanWhere := "ended_at >= ? AND run_id <> '' AND activity_kind <> 'unknown'"
	logWhere := "observed_at >= ? AND run_id <> '' AND activity_kind <> 'unknown'"
	args := []any{since}
	if sourceID != "" {
		spanWhere += " AND source = ?"
		logWhere += " AND source = ?"
		args = append(args, sourceID)
	}
	if sessionID != "" {
		spanWhere += " AND run_id = ?"
		logWhere += " AND run_id = ?"
		args = append(args, sessionID)
	}
	if searchValue != "" {
		predicate := " AND lower(run_id || ' ' || source || ' ' || name || ' ' || content || ' ' || tool_name || ' ' || agent_id || ' ' || agent_definition || ' ' || agent_type || ' ' || target_agent_id || ' ' || target_agent_type || ' ' || model || ' ' || trace_id) LIKE ?"
		spanWhere += predicate
		logWhere += strings.Replace(predicate, "content", "body", 1)
	}
	if searchValue != "" {
		args = append(args, "%"+searchValue+"%")
	}
	logArgs := []any{since}
	if sourceID != "" {
		logArgs = append(logArgs, sourceID)
	}
	if sessionID != "" {
		logArgs = append(logArgs, sessionID)
	}
	if searchValue != "" {
		logArgs = append(logArgs, "%"+searchValue+"%")
	}
	// The two branches share the same logical filters but keep their values
	// separate so the SQL remains easy for SQLite to plan.
	spanArgs := args
	return fmt.Sprintf(`  SELECT source, run_id, ended_at AS observed_at, trace_id, name, content, tool_name,
    agent_id, agent_definition, agent_type, target_agent_id, target_agent_type, model,
    activity_kind, cost_usd, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    (input_tokens_reported <> 0 OR input_tokens <> 0) AS input_reported,
    (output_tokens_reported <> 0 OR output_tokens <> 0) AS output_reported,
    (cache_read_tokens_reported <> 0 OR cache_read_tokens <> 0) AS cache_read_reported,
    (cache_write_tokens_reported <> 0 OR cache_write_tokens <> 0) AS cache_write_reported,
    (reasoning_tokens_reported <> 0 OR reasoning_tokens <> 0) AS reasoning_reported
  FROM spans WHERE %s
  UNION ALL
  SELECT source, run_id, observed_at, trace_id, name, body, tool_name,
    agent_id, agent_definition, agent_type, target_agent_id, target_agent_type, model,
    activity_kind, cost_usd, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    (input_tokens_reported <> 0 OR input_tokens <> 0) AS input_reported,
    (output_tokens_reported <> 0 OR output_tokens <> 0) AS output_reported,
    (cache_read_tokens_reported <> 0 OR cache_read_tokens <> 0) AS cache_read_reported,
    (cache_write_tokens_reported <> 0 OR cache_write_tokens <> 0) AS cache_write_reported,
    (reasoning_tokens_reported <> 0 OR reasoning_tokens <> 0) AS reasoning_reported
  FROM logs WHERE %s`, spanWhere, logWhere), append(spanArgs, logArgs...)
}

func parseStorageTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time: %w", err)
	}
	return parsed, nil
}
