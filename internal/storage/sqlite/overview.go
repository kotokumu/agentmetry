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
	"github.com/theoden9014/agentmetry/sourceplugin"
)

func (store *Store) GetOverview(ctx context.Context, filter query.OverviewFilter) (query.Overview, error) {
	since := formatTime(filter.Since)
	var overview query.Overview

	if err := store.db.QueryRowContext(ctx, `SELECT
  (SELECT COUNT(*) FROM spans WHERE ended_at >= ?),
  (SELECT COUNT(*) FROM logs WHERE observed_at >= ?),
  (SELECT COUNT(*) FROM metrics WHERE observed_at >= ?)`, since, since, since).Scan(
		&overview.SignalCounts.Traces,
		&overview.SignalCounts.Logs,
		&overview.SignalCounts.Metrics,
	); err != nil {
		return query.Overview{}, fmt.Errorf("query signal counts: %w", err)
	}

	activities, err := store.activities(ctx, since, -1, "", "")
	if err != nil {
		return query.Overview{}, err
	}
	overview.Sources = store.sourceCatalog(activities)
	overview.Sessions = buildSessions(activities, filter, store.describeSource, meaningful)
	for _, session := range overview.Sessions {
		overview.RunCount++
		overview.AgentCount += int64(len(session.Agents))
		addTokens(&overview.Tokens, session.Tokens)
		overview.RecentActivity = append(overview.RecentActivity, session.Activities...)
	}
	sort.Slice(overview.RecentActivity, func(i, j int) bool {
		return overview.RecentActivity[i].ObservedAt.After(overview.RecentActivity[j].ObservedAt)
	})
	if len(overview.RecentActivity) > 50 {
		overview.RecentActivity = overview.RecentActivity[:50]
	}
	overview.PlanUsage, err = store.LatestPlanUsage(ctx)
	if err != nil {
		return query.Overview{}, err
	}
	return overview, nil
}

func (store *Store) activities(ctx context.Context, since string, limit int, sourceID, conversationID string) ([]query.Activity, error) {
	return store.activitiesWindow(ctx, since, limit, 0, sourceID, conversationID)
}

func (store *Store) activitiesWindow(ctx context.Context, since string, limit, offset int, sourceID, conversationID string) ([]query.Activity, error) {
	return store.activitiesWindowWithMeaningful(ctx, since, limit, offset, sourceID, conversationID, false, "")
}

func (store *Store) activitiesWindowWithMeaningful(ctx context.Context, since string, limit, offset int, sourceID, conversationID string, meaningfulOnly bool, agentID string) ([]query.Activity, error) {
	spanWhere, spanArgs := activityWhere("ended_at", since, sourceID, conversationID, agentID)
	logWhere, logArgs := activityWhere("observed_at", since, sourceID, conversationID, agentID)
	metricWhere, metricArgs := activityWhere("observed_at", since, sourceID, conversationID, agentID)
	if meaningfulOnly {
		spanWhere += " AND activity_kind <> 'unknown'"
		logWhere += " AND activity_kind <> 'unknown'"
		metricWhere += " AND 0"
	}
	statement := fmt.Sprintf(`SELECT source, signal, trace_id, span_id, parent_span_id, name,
  activity_kind, tool_name, target_agent_id, target_agent_type, content,
  agent_id, agent_definition, agent_type, parent_agent_id, run_id, model, started_at, ended_at, observed_at, status, cost_usd,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
  cache_write_tokens_reported, reasoning_tokens_reported, usage_role, prompt_id, usage_id, attributes_json
FROM (
  SELECT source, 'trace' AS signal, trace_id, span_id, parent_span_id, name,
    activity_kind, tool_name, target_agent_id, target_agent_type, content,
    agent_id, agent_definition, agent_type, parent_agent_id, run_id, model, started_at, ended_at, ended_at AS observed_at, status, cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), '') AS usage_role,
    COALESCE(json_extract(attributes_json, '$."gen_ai.turn.id"'), '') AS prompt_id,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.id"'), '') AS usage_id,
    attributes_json
  FROM (SELECT source, trace_id, span_id, parent_span_id, name,
    activity_kind, tool_name, target_agent_id, target_agent_type, content,
    agent_id, agent_definition, agent_type, parent_agent_id, run_id, model, started_at, ended_at, status, cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported, attributes_json
    FROM spans WHERE %s ORDER BY ended_at DESC LIMIT ?)
  UNION ALL
  SELECT source, 'log', trace_id, span_id, '', name,
    activity_kind, tool_name, target_agent_id, target_agent_type, body,
    agent_id, agent_definition, agent_type, parent_agent_id, run_id, model, observed_at, observed_at, observed_at, '', cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), ''),
    COALESCE(json_extract(attributes_json, '$."gen_ai.turn.id"'), ''),
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.id"'), ''),
    attributes_json
  FROM (SELECT source, trace_id, span_id, name, body,
    activity_kind, tool_name, target_agent_id, target_agent_type,
    agent_id, agent_definition, agent_type, parent_agent_id, run_id, model, observed_at, cost_usd,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
    input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
    cache_write_tokens_reported, reasoning_tokens_reported, attributes_json
    FROM logs WHERE %s ORDER BY observed_at DESC LIMIT ?)
  UNION ALL
  SELECT source, 'metric', '', '', '', name,
    'unknown', '', '', '', CAST(value AS TEXT),
    agent_id, agent_definition, agent_type, parent_agent_id, run_id, model, observed_at, observed_at, observed_at, '', cost_usd,
    0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), 'aggregate'),
    COALESCE(json_extract(attributes_json, '$."gen_ai.turn.id"'), ''),
    COALESCE(json_extract(attributes_json, '$."gen_ai.usage.id"'), ''),
    attributes_json
  FROM (SELECT source, name, value, agent_id, agent_definition, agent_type, parent_agent_id,
    run_id, model, observed_at, cost_usd, attributes_json
    FROM metrics WHERE %s ORDER BY observed_at DESC LIMIT ?)
)
ORDER BY observed_at DESC
LIMIT ? OFFSET ?`, spanWhere, logWhere, metricWhere)
	branchLimit := limit
	if limit >= 0 {
		branchLimit = offset + limit
	}
	args := append(append(append(append(append(append(spanArgs, branchLimit), logArgs...), branchLimit), metricArgs...), branchLimit), limit, offset)
	rows, err := store.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query recent activity: %w", err)
	}
	defer rows.Close()

	activities := make([]query.Activity, 0, 50)
	for rows.Next() {
		activity, err := store.scanActivity(rows)
		if err != nil {
			return nil, fmt.Errorf("scan recent activity: %w", err)
		}
		activities = append(activities, activity)
	}
	if err := rows.Err(); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("iterate recent activity: %w", err)
	}
	return enrichActivityRelationships(activities), nil
}

// activityWhere only interpolates trusted SQL fragments while keeping all
// values parameterized. Omitting optional OR predicates lets SQLite select the
// conversation indexes for exact-route lookups.
func activityWhere(timeColumn, since, sourceID, conversationID, agentID string) (string, []any) {
	where := timeColumn + " >= ? AND run_id <> ''"
	args := []any{since}
	if sourceID != "" {
		where += " AND source = ?"
		args = append(args, sourceID)
	}
	if conversationID != "" {
		where += " AND run_id = ?"
		args = append(args, conversationID)
	}
	if agentID != "" {
		where += " AND CASE WHEN agent_id = '' THEN 'main' ELSE agent_id END = ?"
		args = append(args, agentID)
	}
	return where, args
}

type rowScanner interface {
	Scan(...any) error
}

func (store *Store) scanActivity(row rowScanner) (query.Activity, error) {
	var activity query.Activity
	var signal string
	var startedAt, endedAt, observedAt string
	var attributesJSON string
	var cost sql.NullFloat64
	var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported bool
	if err := row.Scan(
		&activity.Source, &signal, &activity.TraceID, &activity.SpanID, &activity.ParentSpanID, &activity.Name,
		&activity.Kind, &activity.ToolName, &activity.TargetAgentID, &activity.TargetAgentType, &activity.Content,
		&activity.AgentID, &activity.AgentDefinition, &activity.AgentType, &activity.ParentAgentID, &activity.RunID, &activity.Model,
		&startedAt, &endedAt, &observedAt, &activity.Status, &cost,
		&activity.Tokens.Input, &activity.Tokens.Output, &activity.Tokens.CacheRead, &activity.Tokens.CacheWrite, &activity.Tokens.Reasoning,
		&inputReported, &outputReported, &cacheReadReported, &cacheWriteReported, &reasoningReported,
		&activity.UsageRole, &activity.PromptID, &activity.UsageID, &attributesJSON,
	); err != nil {
		return query.Activity{}, err
	}
	activity.Signal = canonical.Signal(signal)
	if err := json.Unmarshal([]byte(attributesJSON), &activity.Attributes); err != nil {
		return query.Activity{}, fmt.Errorf("decode activity attributes: %w", err)
	}
	activity.Tokens.Presence = canonical.TokenPresence{
		Input: inputReported && activity.Tokens.Input == 0, Output: outputReported && activity.Tokens.Output == 0,
		CacheRead:  cacheReadReported && activity.Tokens.CacheRead == 0,
		CacheWrite: cacheWriteReported && activity.Tokens.CacheWrite == 0,
		Reasoning:  reasoningReported && activity.Tokens.Reasoning == 0,
	}
	if cost.Valid {
		activity.CostUSD = &cost.Float64
	}
	var err error
	if activity.ObservedAt, err = time.Parse(time.RFC3339Nano, observedAt); err != nil {
		return query.Activity{}, fmt.Errorf("parse observed time: %w", err)
	}
	if activity.StartedAt, err = time.Parse(time.RFC3339Nano, startedAt); err != nil {
		return query.Activity{}, fmt.Errorf("parse start time: %w", err)
	}
	if activity.EndedAt, err = time.Parse(time.RFC3339Nano, endedAt); err != nil {
		return query.Activity{}, fmt.Errorf("parse end time: %w", err)
	}
	metadata := store.profiles.NormalizeAgentMetadata(activity.Source, sourceplugin.AgentMetadata{
		ID: activity.AgentID, Definition: activity.AgentDefinition, Type: activity.AgentType,
		ParentID: activity.ParentAgentID, Model: activity.Model,
	})
	activity.AgentID, activity.AgentDefinition, activity.AgentType = metadata.ID, metadata.Definition, metadata.Type
	activity.ParentAgentID, activity.Model = metadata.ParentID, metadata.Model
	return activity, nil
}

func buildSessions(activities []query.Activity, filter query.OverviewFilter, describeSource func(string) query.TelemetrySource, include func(query.Activity) bool) []query.Session {
	type conversationKey struct {
		sourceID string
		runID    string
	}
	type sessionState struct {
		key           conversationKey
		allActivities []query.Activity
	}
	states := make(map[conversationKey]*sessionState)
	for _, activity := range activities {
		if activity.RunID == "" {
			continue
		}
		key := conversationKey{sourceID: activity.Source, runID: activity.RunID}
		state := states[key]
		if state == nil {
			state = &sessionState{key: key}
			states[key] = state
		}
		state.allActivities = append(state.allActivities, activity)
	}

	sessions := make([]query.Session, 0, len(states))
	for _, state := range states {
		traces := make(map[string]struct{})
		for _, activity := range state.allActivities {
			if activity.TraceID != "" {
				traces[activity.TraceID] = struct{}{}
			}
		}
		rootRun := state.key.runID
		session := query.Session{ID: rootRun, SourceID: state.key.sourceID}
		agents := make(map[string]*query.AgentSession)
		sourceIDs := make(map[string]struct{})
		normalizedActivities := make([]query.Activity, 0, len(state.allActivities))
		for _, activity := range enrichAgentEvidence(state.allActivities) {
			if session.StartedAt.IsZero() || activity.ObservedAt.Before(session.StartedAt) {
				session.StartedAt = activity.ObservedAt
			}
			if session.EndedAt.IsZero() || activity.ObservedAt.After(session.EndedAt) {
				session.EndedAt = activity.ObservedAt
			}
			if !include(activity) {
				continue
			}
			sourceIDs[activity.Source] = struct{}{}
			agentID, agentType, parentAgentID := "main", "root", ""
			if activity.AgentID != "" {
				agentID = activity.AgentID
			}
			if activity.AgentType != "" {
				agentType = activity.AgentType
			}
			if activity.ParentAgentID != "" {
				parentAgentID = activity.ParentAgentID
			}
			activity.AgentID, activity.AgentType, activity.ParentAgentID = agentID, agentType, parentAgentID
			normalizedActivities = append(normalizedActivities, activity)
		}
		if !sessionMatches(rootRun, normalizedActivities, filter) {
			continue
		}
		for sourceID := range sourceIDs {
			session.Sources = append(session.Sources, describeSource(sourceID))
		}
		sort.Slice(session.Sources, func(i, j int) bool { return session.Sources[i].Label < session.Sources[j].Label })
		usageContributions := selectUsageContributions(normalizedActivities)
		var cost float64
		for index, activity := range normalizedActivities {
			activity.ContributesToTotal = usageContributions[index]
			session.Activities = append(session.Activities, activity)
			session.ActivityCount++
			if activity.ContributesToTotal {
				addTokens(&session.Tokens, activity.Tokens)
			}
			if activity.ContributesToTotal && activity.CostUSD != nil {
				cost += *activity.CostUSD
				session.CostUSD = &cost
			}
			agent := agents[activity.AgentID]
			if agent == nil {
				agent = &query.AgentSession{
					AgentID: activity.AgentID, AgentDefinition: activity.AgentDefinition,
					AgentType: activity.AgentType, ParentAgentID: activity.ParentAgentID, Model: activity.Model,
				}
				agents[activity.AgentID] = agent
			}
			fillMissingAgentMetadata(agent, activity)
			agent.ActivityCount++
			if activity.ContributesToTotal {
				addTokens(&agent.Tokens, activity.Tokens)
			}
		}
		if session.ActivityCount == 0 {
			continue
		}
		for traceID := range traces {
			session.TraceIDs = append(session.TraceIDs, traceID)
		}
		sort.Strings(session.TraceIDs)
		for _, agent := range agents {
			session.Agents = append(session.Agents, *agent)
		}
		sort.Slice(session.Agents, func(i, j int) bool { return session.Agents[i].AgentID < session.Agents[j].AgentID })
		sort.Slice(session.Activities, func(i, j int) bool { return session.Activities[i].ObservedAt.After(session.Activities[j].ObservedAt) })
		session.ActivityOffset = boundedOffset(len(session.Activities), filter.ActivityOffset)
		session.Activities = activityPage(session.Activities, session.ActivityOffset, filter.ActivityLimit)
		session.HasEarlier = session.ActivityOffset > 0
		session.HasMore = int64(session.ActivityOffset+len(session.Activities)) < session.ActivityCount
		sessions = append(sessions, session)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].EndedAt.After(sessions[j].EndedAt)
	})
	return sessions
}

func (store *Store) sourceCatalog(activities []query.Activity) []query.TelemetrySource {
	seen := make(map[string]struct{})
	var sources []query.TelemetrySource
	for _, activity := range activities {
		if activity.Source == "" {
			continue
		}
		if _, exists := seen[activity.Source]; exists {
			continue
		}
		seen[activity.Source] = struct{}{}
		sources = append(sources, store.describeSource(activity.Source))
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Label < sources[j].Label })
	return sources
}

func (store *Store) describeSource(sourceID string) query.TelemetrySource {
	descriptor := store.profiles.Describe(sourceID)
	return query.TelemetrySource{ID: descriptor.ID, Label: descriptor.Label}
}

func sessionMatches(sessionID string, activities []query.Activity, filter query.OverviewFilter) bool {
	if filter.SourceID != "" {
		found := false
		for _, activity := range activities {
			if activity.Source == filter.SourceID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	needle := strings.ToLower(strings.TrimSpace(filter.Search))
	if needle == "" {
		return true
	}
	if strings.Contains(strings.ToLower(sessionID), needle) {
		return true
	}
	for _, activity := range activities {
		fields := [...]string{
			activity.Source, activity.Name, activity.Content, activity.ToolName,
			activity.AgentID, activity.AgentDefinition, activity.AgentType,
			activity.TargetAgentID, activity.TargetAgentType, activity.Model,
			activity.TraceID,
		}
		for _, field := range fields {
			if strings.Contains(strings.ToLower(field), needle) {
				return true
			}
		}
	}
	return false
}

type agentMetadataField struct {
	value    string
	priority int
}

type agentMetadataEvidence struct {
	agentID, definition, agentType, parentAgentID, model agentMetadataField
}

// enrichAgentEvidence combines complementary OTLP records for one model call.
// Source plugins own producer-specific field mapping; this layer only joins the
// canonical evidence key and prefers the most authoritative usage record.
func enrichAgentEvidence(activities []query.Activity) []query.Activity {
	evidenceByUsage := make(map[string]*agentMetadataEvidence)
	for _, activity := range activities {
		if activity.UsageID == "" {
			continue
		}
		key := activity.Source + "|" + activity.UsageID
		evidence := evidenceByUsage[key]
		if evidence == nil {
			evidence = &agentMetadataEvidence{}
			evidenceByUsage[key] = evidence
		}
		priority := usagePriority(usageRole(activity))
		mergeAgentMetadataField(&evidence.agentID, activity.AgentID, priority)
		mergeAgentMetadataField(&evidence.definition, activity.AgentDefinition, priority)
		mergeAgentMetadataField(&evidence.agentType, activity.AgentType, priority)
		mergeAgentMetadataField(&evidence.parentAgentID, activity.ParentAgentID, priority)
		mergeAgentMetadataField(&evidence.model, activity.Model, priority)
	}

	enriched := append([]query.Activity(nil), activities...)
	for index := range enriched {
		activity := &enriched[index]
		if activity.UsageID == "" {
			continue
		}
		evidence := evidenceByUsage[activity.Source+"|"+activity.UsageID]
		if evidence == nil {
			continue
		}
		activity.AgentID = evidence.agentID.value
		activity.AgentDefinition = evidence.definition.value
		activity.AgentType = evidence.agentType.value
		activity.ParentAgentID = evidence.parentAgentID.value
		activity.Model = evidence.model.value
	}
	return enriched
}

type activityLink struct {
	traceID string
	spanID  string
}

// enrichActivityRelationships keeps native trace identity intact while making
// source events navigable through the trace/span that carries the same usage
// or prompt identity. Source-specific plugins define the identity aliases;
// storage only resolves relationships between already-normalized evidence.
func enrichActivityRelationships(activities []query.Activity) []query.Activity {
	byUsage := make(map[string]activityLink)
	byPrompt := make(map[string]activityLink)
	for _, activity := range activities {
		if activity.TraceID == "" {
			continue
		}
		link := activityLink{traceID: activity.TraceID, spanID: activity.SpanID}
		if activity.UsageID != "" {
			byUsage[activity.Source+"|"+activity.UsageID] = link
		}
		if activity.PromptID != "" {
			byPrompt[activity.Source+"|"+activity.PromptID] = link
		}
	}
	result := append([]query.Activity(nil), activities...)
	for index := range result {
		activity := &result[index]
		if activity.TraceID != "" {
			continue
		}
		var link activityLink
		if activity.UsageID != "" {
			link = byUsage[activity.Source+"|"+activity.UsageID]
		}
		if link.traceID == "" && activity.PromptID != "" {
			link = byPrompt[activity.Source+"|"+activity.PromptID]
		}
		if link.traceID != "" {
			activity.RelatedTraceID = link.traceID
			activity.RelatedSpanID = link.spanID
		}
	}
	return result
}

func mergeAgentMetadataField(field *agentMetadataField, value string, priority int) {
	if value != "" && (field.value == "" || priority > field.priority) {
		field.value = value
		field.priority = priority
	}
}

func fillMissingAgentMetadata(agent *query.AgentSession, activity query.Activity) {
	if agent.AgentDefinition == "" {
		agent.AgentDefinition = activity.AgentDefinition
	}
	if agent.AgentType == "" {
		agent.AgentType = activity.AgentType
	}
	if agent.ParentAgentID == "" {
		agent.ParentAgentID = activity.ParentAgentID
	}
	if agent.Model == "" {
		agent.Model = activity.Model
	}
}

func activityPage(activities []query.Activity, offset, limit int) []query.Activity {
	offset = boundedOffset(len(activities), offset)
	if limit <= 0 {
		limit = 100
	}
	if offset >= len(activities) {
		return []query.Activity{}
	}
	end := len(activities)
	if offset+limit < end {
		end = offset + limit
	}
	return activities[offset:end]
}

func boundedOffset(length, offset int) int {
	if offset < 0 {
		return 0
	}
	if offset > length {
		return length
	}
	return offset
}

func meaningful(activity query.Activity) bool {
	return activity.Kind != canonical.ActivityUnknown
}

func addTokens(total *canonical.TokenUsage, usage canonical.TokenUsage) {
	total.Add(usage)
}

func selectUsageContributions(activities []query.Activity) map[int]bool {
	type candidate struct {
		index int
		role  string
	}
	selectedByKey := make(map[string]candidate)
	for index, activity := range activities {
		if !activity.Tokens.AnyReported() && activity.CostUSD == nil {
			continue
		}
		role := usageRole(activity)
		key := usageKey(activity)
		current, exists := selectedByKey[key]
		if !exists || usagePriority(role) > usagePriority(current.role) {
			selectedByKey[key] = candidate{index: index, role: role}
		}
	}

	selected := make(map[int]bool, len(selectedByKey))
	for _, value := range selectedByKey {
		selected[value.index] = true
	}
	authoritativeBySecond := make(map[string][]int)
	for _, authoritative := range selectedByKey {
		if usagePriority(authoritative.role) < usagePriority("authoritative_call") || !selected[authoritative.index] {
			continue
		}
		activity := activities[authoritative.index]
		key := usageTimeBucket(activity.Source, activity.ObservedAt.Unix())
		authoritativeBySecond[key] = append(authoritativeBySecond[key], authoritative.index)
	}
	for _, corroborating := range selectedByKey {
		if corroborating.role != "corroborating" || !selected[corroborating.index] {
			continue
		}
		candidateActivity := activities[corroborating.index]
		candidateSecond := candidateActivity.ObservedAt.Unix()
		matched := false
		for second := candidateSecond - 1; second <= candidateSecond+1 && !matched; second++ {
			for _, authoritativeIndex := range authoritativeBySecond[usageTimeBucket(candidateActivity.Source, second)] {
				if sameUsageEvidence(candidateActivity, activities[authoritativeIndex]) {
					delete(selected, corroborating.index)
					matched = true
					break
				}
			}
		}
	}
	return selected
}

func usageTimeBucket(source string, unixSecond int64) string {
	return fmt.Sprintf("%s|%d", source, unixSecond)
}

func usageRole(activity query.Activity) string {
	if activity.UsageRole != "" {
		return activity.UsageRole
	}
	switch activity.Signal {
	case canonical.SignalTrace:
		return "corroborating"
	case canonical.SignalMetric:
		return "aggregate"
	default:
		return "authoritative_call"
	}
}

func usagePriority(role string) int {
	switch role {
	case "authoritative_call":
		return 3
	case "estimate":
		return 2
	case "corroborating":
		return 1
	default:
		return 0
	}
}

func usageKey(activity query.Activity) string {
	if activity.UsageID != "" {
		return activity.Source + "|" + activity.UsageID
	}
	return fmt.Sprintf("%s|%s|%d|%d|%d|%d|%d|%d|%d",
		activity.Source, activity.RunID, activity.ObservedAt.UnixNano(),
		activity.Tokens.Input, activity.Tokens.Output, activity.Tokens.CacheRead,
		activity.Tokens.CacheWrite, activity.Tokens.Reasoning, boolInt(activity.CostUSD != nil))
}

func sameUsageEvidence(left, right query.Activity) bool {
	if left.Source != right.Source || !compatibleUsage(left.Tokens, right.Tokens) {
		return false
	}
	if left.RunID != "" && right.RunID != "" && left.RunID != right.RunID {
		return false
	}
	delta := left.ObservedAt.Sub(right.ObservedAt)
	if delta < 0 {
		delta = -delta
	}
	return delta <= time.Second
}

func compatibleUsage(left, right canonical.TokenUsage) bool {
	measurements := []struct {
		leftValue, rightValue       int64
		leftReported, rightReported bool
	}{
		{left.Input, right.Input, left.InputReported(), right.InputReported()},
		{left.Output, right.Output, left.OutputReported(), right.OutputReported()},
		{left.CacheRead, right.CacheRead, left.CacheReadReported(), right.CacheReadReported()},
		{left.CacheWrite, right.CacheWrite, left.CacheWriteReported(), right.CacheWriteReported()},
		{left.Reasoning, right.Reasoning, left.ReasoningReported(), right.ReasoningReported()},
	}
	sharedPrimary := false
	for index, measurement := range measurements {
		if !measurement.leftReported || !measurement.rightReported {
			continue
		}
		if index < 2 {
			sharedPrimary = true
		}
		if measurement.leftValue != measurement.rightValue {
			return false
		}
	}
	return sharedPrimary
}
