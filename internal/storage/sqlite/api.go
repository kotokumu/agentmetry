package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
)

func (store *Store) GetDashboard(ctx context.Context, filter query.DashboardFilter) (query.Overview, error) {
	since := formatTime(filter.Since)
	var dashboard query.Overview
	if err := store.db.QueryRowContext(ctx, `SELECT
  COALESCE(SUM(trace_count), 0), COALESCE(SUM(log_count), 0)
FROM session_rollups WHERE ended_at >= ? AND (? = '' OR source = ?)`, since, filter.SourceID, filter.SourceID).Scan(
		&dashboard.SignalCounts.Traces, &dashboard.SignalCounts.Logs,
	); err != nil {
		return query.Overview{}, fmt.Errorf("query dashboard signal counts: %w", err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM metrics WHERE observed_at >= ? AND (? = '' OR source = ?)`, since, filter.SourceID, filter.SourceID).Scan(&dashboard.SignalCounts.Metrics); err != nil {
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
	dashboard.PlanUsage, err = store.LatestPlanUsage(ctx)
	if err != nil {
		return query.Overview{}, err
	}
	return dashboard, nil
}

func (store *Store) ListSessions(ctx context.Context, filter query.SessionListFilter) (query.SessionPage, error) {
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	if strings.TrimSpace(filter.Search) == "" {
		return store.listSessionsFromRollups(ctx, filter)
	}
	branches, args := summaryBranches(formatTime(filter.Since), filter.SourceID, filter.SessionID, filter.Search)
	statement := fmt.Sprintf(`WITH activity AS (
%s
)
SELECT source, run_id, MIN(observed_at), MAX(observed_at), COUNT(*),
  COUNT(DISTINCT CASE WHEN agent_id <> '' THEN agent_id ELSE 'main' END)
FROM activity
GROUP BY source, run_id
ORDER BY MAX(observed_at) DESC, source ASC, run_id ASC
LIMIT ? OFFSET ?`, branches)
	args = append(args, filter.PageSize+1, filter.Offset)
	rows, err := store.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return query.SessionPage{}, fmt.Errorf("query session summaries: %w", err)
	}
	defer rows.Close()

	page := query.SessionPage{Sessions: make([]query.Session, 0, filter.PageSize)}
	for rows.Next() {
		var session query.Session
		var startedAt, endedAt string
		if err := rows.Scan(&session.SourceID, &session.ID, &startedAt, &endedAt, &session.ActivityCount, &session.AgentCount); err != nil {
			return query.SessionPage{}, fmt.Errorf("scan session summary: %w", err)
		}
		session.StartedAt, err = parseStorageTime(startedAt)
		if err != nil {
			return query.SessionPage{}, err
		}
		session.EndedAt, err = parseStorageTime(endedAt)
		if err != nil {
			return query.SessionPage{}, err
		}
		session.Sources = []query.TelemetrySource{store.describeSource(session.SourceID)}
		page.Sessions = append(page.Sessions, session)
	}
	if err := rows.Err(); err != nil {
		return query.SessionPage{}, fmt.Errorf("iterate session summaries: %w", err)
	}
	if len(page.Sessions) > filter.PageSize {
		page.Sessions = page.Sessions[:filter.PageSize]
		page.HasMore = true
		page.NextOffset = filter.Offset + filter.PageSize
	}
	return page, nil
}

func (store *Store) listSessionsFromRollups(ctx context.Context, filter query.SessionListFilter) (query.SessionPage, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT source, run_id, started_at, ended_at, activity_count, agent_count
FROM session_rollups
WHERE ended_at >= ? AND (? = '' OR source = ?) AND (? = '' OR run_id = ?)
ORDER BY ended_at DESC, source ASC, run_id ASC
LIMIT ? OFFSET ?`, formatTime(filter.Since), filter.SourceID, filter.SourceID, filter.SessionID, filter.SessionID, filter.PageSize+1, filter.Offset)
	if err != nil {
		return query.SessionPage{}, fmt.Errorf("query session rollups: %w", err)
	}
	defer rows.Close()
	page := query.SessionPage{Sessions: make([]query.Session, 0, filter.PageSize)}
	for rows.Next() {
		var session query.Session
		var startedAt, endedAt string
		if err := rows.Scan(&session.SourceID, &session.ID, &startedAt, &endedAt, &session.ActivityCount, &session.AgentCount); err != nil {
			return query.SessionPage{}, fmt.Errorf("scan session rollup: %w", err)
		}
		var err error
		session.StartedAt, err = parseStorageTime(startedAt)
		if err != nil {
			return query.SessionPage{}, err
		}
		session.EndedAt, err = parseStorageTime(endedAt)
		if err != nil {
			return query.SessionPage{}, err
		}
		session.Sources = []query.TelemetrySource{store.describeSource(session.SourceID)}
		session.Agents = make([]query.AgentSession, 0)
		session.Activities = make([]query.Activity, 0)
		page.Sessions = append(page.Sessions, session)
	}
	if err := rows.Err(); err != nil {
		return query.SessionPage{}, fmt.Errorf("iterate session rollups: %w", err)
	}
	if len(page.Sessions) > filter.PageSize {
		page.Sessions = page.Sessions[:filter.PageSize]
		page.HasMore = true
		page.NextOffset = filter.Offset + filter.PageSize
	}
	return page, nil
}

func (store *Store) GetSessionSummary(ctx context.Context, sourceID, sessionID string) (query.Session, error) {
	return store.loadSessionSummary(ctx, sourceID, sessionID)
}

func (store *Store) ListSessionActivities(ctx context.Context, filter query.ActivityPageFilter) (query.ActivityPage, error) {
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	offset := filter.Offset
	if offset == 0 && filter.TraceID != "" && filter.SpanID != "" {
		var err error
		offset, err = store.anchorOffset(ctx, filter.SourceID, filter.ConversationID, filter.TraceID, filter.SpanID)
		if err != nil {
			return query.ActivityPage{}, err
		}
	}
	var total int64
	if err := store.db.QueryRowContext(ctx, `SELECT
  (SELECT COUNT(*) FROM spans WHERE ended_at >= ? AND run_id = ? AND source = ? AND activity_kind <> 'unknown') +
  (SELECT COUNT(*) FROM logs WHERE observed_at >= ? AND run_id = ? AND source = ? AND activity_kind <> 'unknown')`,
		formatTime(time.Unix(0, 0)), filter.ConversationID, filter.SourceID,
		formatTime(time.Unix(0, 0)), filter.ConversationID, filter.SourceID).Scan(&total); err != nil {
		return query.ActivityPage{}, fmt.Errorf("count session activities: %w", err)
	}
	if total == 0 {
		return query.ActivityPage{}, query.ErrConversationNotFound
	}
	activities, err := store.activitiesWindowWithMeaningful(ctx, formatTime(time.Unix(0, 0)), filter.PageSize, offset, filter.SourceID, filter.ConversationID, true)
	if err != nil {
		return query.ActivityPage{}, err
	}
	return query.ActivityPage{
		Activities: activities, Total: total, Offset: offset,
		HasEarlier: offset > 0, HasMore: int64(offset+len(activities)) < total,
	}, nil
}

func (store *Store) anchorOffset(ctx context.Context, sourceID, sessionID, traceID, spanID string) (int, error) {
	var observedAt string
	if err := store.db.QueryRowContext(ctx, `SELECT observed_at FROM (
  SELECT ended_at AS observed_at FROM spans WHERE source = ? AND run_id = ? AND trace_id = ? AND span_id = ? AND activity_kind <> 'unknown'
  UNION ALL
  SELECT observed_at FROM logs WHERE source = ? AND run_id = ? AND trace_id = ? AND span_id = ? AND activity_kind <> 'unknown'
) ORDER BY observed_at DESC LIMIT 1`, sourceID, sessionID, traceID, spanID, sourceID, sessionID, traceID, spanID).Scan(&observedAt); err != nil {
		if err == sql.ErrNoRows {
			return 0, query.ErrConversationTargetNotFound
		}
		return 0, fmt.Errorf("query activity anchor: %w", err)
	}
	var before int64
	if err := store.db.QueryRowContext(ctx, `SELECT
  (SELECT COUNT(*) FROM spans WHERE ended_at >= ? AND ended_at > ? AND run_id = ? AND source = ? AND activity_kind <> 'unknown') +
  (SELECT COUNT(*) FROM logs WHERE observed_at >= ? AND observed_at > ? AND run_id = ? AND source = ? AND activity_kind <> 'unknown')`,
		formatTime(time.Unix(0, 0)), observedAt, sessionID, sourceID,
		formatTime(time.Unix(0, 0)), observedAt, sessionID, sourceID).Scan(&before); err != nil {
		return 0, fmt.Errorf("count activity anchor offset: %w", err)
	}
	if before < 25 {
		return 0, nil
	}
	return int(before - 25), nil
}

func (store *Store) dashboardSources(ctx context.Context, since, sourceID string) ([]query.TelemetrySource, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT source FROM (
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
	if strings.TrimSpace(search) == "" {
		var input, output, cacheRead, cacheWrite, reasoning int64
		var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported int
		err = store.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(agent_count), 0),
  COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0), COALESCE(SUM(reasoning_tokens), 0),
  COALESCE(MAX(input_reported), 0), COALESCE(MAX(output_reported), 0),
  COALESCE(MAX(cache_read_reported), 0), COALESCE(MAX(cache_write_reported), 0), COALESCE(MAX(reasoning_reported), 0)
FROM session_rollups WHERE ended_at >= ? AND (? = '' OR source = ?)`,
			since, sourceID, sourceID).Scan(&runCount, &agentCount, &input, &output, &cacheRead, &cacheWrite, &reasoning, &inputReported, &outputReported, &cacheReadReported, &cacheWriteReported, &reasoningReported)
		if err != nil {
			return 0, 0, canonical.TokenUsage{}, fmt.Errorf("query dashboard rollups: %w", err)
		}
		return runCount, agentCount, aggregateTokens(input, output, cacheRead, cacheWrite, reasoning, inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported), nil
	}
	branches, args := summaryBranches(since, sourceID, "", search)
	statement := fmt.Sprintf(`WITH activity AS (
%s
)
SELECT COUNT(DISTINCT source || char(0) || run_id),
       COUNT(DISTINCT source || char(0) || run_id || char(0) || CASE WHEN agent_id <> '' THEN agent_id ELSE 'main' END),
       COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
       COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0),
       COALESCE(SUM(reasoning_tokens), 0),
       MAX(input_reported), MAX(output_reported), MAX(cache_read_reported),
       MAX(cache_write_reported), MAX(reasoning_reported)
FROM activity`, branches)
	var input, output, cacheRead, cacheWrite, reasoning int64
	var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported int
	err = store.db.QueryRowContext(ctx, statement, args...).Scan(&runCount, &agentCount, &input, &output, &cacheRead, &cacheWrite, &reasoning, &inputReported, &outputReported, &cacheReadReported, &cacheWriteReported, &reasoningReported)
	if err != nil {
		return 0, 0, canonical.TokenUsage{}, fmt.Errorf("query dashboard aggregates: %w", err)
	}
	tokens = canonical.TokenUsage{Input: input, Output: output, CacheRead: cacheRead, CacheWrite: cacheWrite, Reasoning: reasoning, Presence: canonical.TokenPresence{Input: inputReported != 0 && input == 0, Output: outputReported != 0 && output == 0, CacheRead: cacheReadReported != 0 && cacheRead == 0, CacheWrite: cacheWriteReported != 0 && cacheWrite == 0, Reasoning: reasoningReported != 0 && reasoning == 0}}
	return runCount, agentCount, tokens, nil
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
		predicate := " AND lower(source || ' ' || name || ' ' || content || ' ' || tool_name || ' ' || agent_id || ' ' || agent_definition || ' ' || agent_type || ' ' || target_agent_id || ' ' || target_agent_type || ' ' || model || ' ' || trace_id) LIKE ?"
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
