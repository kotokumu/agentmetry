package mcpserver

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/planusage"
	"github.com/theoden9014/agentmetry/internal/query"
)

type Clock func() time.Time

type OverviewInput struct {
	Range     string `json:"range,omitempty" jsonschema:"Time range: 1h, 24h, or 7d. Defaults to 24h."`
	Source    string `json:"source,omitempty" jsonschema:"Optional telemetry source ID, such as one returned in overview.sources."`
	Search    string `json:"search,omitempty" jsonschema:"Optional case-insensitive full-session text search."`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"Maximum number of sessions to return. Defaults to 100; capped at 100."`
	PageToken string `json:"pageToken,omitempty" jsonschema:"Opaque continuation token returned by the previous call."`
}

type TraceInput struct {
	TraceID string `json:"traceId" jsonschema:"Required OTLP trace ID."`
}

type SessionActivitiesInput struct {
	Source    string `json:"source" jsonschema:"Required telemetry source ID."`
	SessionID string `json:"sessionId" jsonschema:"Required session ID."`
	PageSize  int    `json:"pageSize,omitempty" jsonschema:"Maximum number of activities to return. Defaults to 100; capped at 100."`
	PageToken string `json:"pageToken,omitempty" jsonschema:"Opaque continuation token returned by the previous call."`
	Direction string `json:"direction,omitempty" jsonschema:"older or newer. Defaults to older."`
}

type SessionActivitiesOutput struct {
	Source            string           `json:"source"`
	SessionID         string           `json:"sessionId"`
	Activities        []ActivityOutput `json:"activities"`
	Total             int64            `json:"total"`
	NextPageToken     string           `json:"nextPageToken,omitempty"`
	PreviousPageToken string           `json:"previousPageToken,omitempty"`
	HasEarlier        bool             `json:"hasEarlier"`
	HasMore           bool             `json:"hasMore"`
}

type TokenUsageOutput struct {
	Input      *int64 `json:"input"`
	Output     *int64 `json:"output"`
	CacheRead  *int64 `json:"cacheRead"`
	CacheWrite *int64 `json:"cacheWrite"`
	Reasoning  *int64 `json:"reasoning"`
	Total      *int64 `json:"total"`
}

type ActivityOutput struct {
	Source             string           `json:"source"`
	Signal             string           `json:"signal"`
	TraceID            string           `json:"traceId,omitempty"`
	SpanID             string           `json:"spanId,omitempty"`
	ParentSpanID       string           `json:"parentSpanId,omitempty"`
	Name               string           `json:"name"`
	Kind               string           `json:"kind"`
	ToolName           string           `json:"toolName,omitempty"`
	TargetAgentID      string           `json:"targetAgentId,omitempty"`
	TargetAgentType    string           `json:"targetAgentType,omitempty"`
	Content            string           `json:"content,omitempty"`
	AgentID            string           `json:"agentId"`
	AgentDefinition    string           `json:"agentDefinition,omitempty"`
	AgentType          string           `json:"agentType,omitempty"`
	ParentAgentID      string           `json:"parentAgentId,omitempty"`
	RunID              string           `json:"runId"`
	Model              string           `json:"model"`
	StartedAt          time.Time        `json:"startedAt"`
	EndedAt            time.Time        `json:"endedAt"`
	ObservedAt         time.Time        `json:"observedAt"`
	Status             string           `json:"status,omitempty"`
	Tokens             TokenUsageOutput `json:"tokens"`
	CostUSD            *float64         `json:"costUsd,omitempty"`
	ContributesToTotal bool             `json:"contributesToTotal"`
}

type AgentSessionOutput struct {
	AgentID         string           `json:"agentId"`
	AgentDefinition string           `json:"agentDefinition,omitempty"`
	AgentType       string           `json:"agentType,omitempty"`
	ParentAgentID   string           `json:"parentAgentId,omitempty"`
	Model           string           `json:"model,omitempty"`
	ActivityCount   int64            `json:"activityCount"`
	Tokens          TokenUsageOutput `json:"tokens"`
}

type SessionOutput struct {
	ID            string                  `json:"id"`
	SourceID      string                  `json:"sourceId"`
	Sources       []query.TelemetrySource `json:"sources"`
	TraceIDs      []string                `json:"traceIds"`
	StartedAt     time.Time               `json:"startedAt"`
	EndedAt       time.Time               `json:"endedAt"`
	ActivityCount int64                   `json:"activityCount"`
	Tokens        TokenUsageOutput        `json:"tokens"`
	CostUSD       *float64                `json:"costUsd,omitempty"`
	Agents        []AgentSessionOutput    `json:"agents"`
	Activities    []ActivityOutput        `json:"activities"`
}

type SignalCountsOutput struct {
	Traces  int64 `json:"traces"`
	Logs    int64 `json:"logs"`
	Metrics int64 `json:"metrics"`
}

type OverviewDataOutput struct {
	Sources        []query.TelemetrySource `json:"sources"`
	SignalCounts   SignalCountsOutput      `json:"signalCounts"`
	RunCount       int64                   `json:"runCount"`
	AgentCount     int64                   `json:"agentCount"`
	Tokens         TokenUsageOutput        `json:"tokens"`
	RecentActivity []ActivityOutput        `json:"recentActivity"`
	Sessions       []SessionOutput         `json:"sessions"`
	PlanUsage      []planusage.Snapshot    `json:"planUsage"`
}

type OverviewOutput struct {
	Overview          OverviewDataOutput `json:"overview"`
	NextPageToken     string             `json:"nextPageToken,omitempty"`
	PreviousPageToken string             `json:"previousPageToken,omitempty"`
}

type TraceDataOutput struct {
	TraceID            string                  `json:"traceId"`
	StartedAt          time.Time               `json:"startedAt"`
	EndedAt            time.Time               `json:"endedAt"`
	Status             string                  `json:"status"`
	RootSpanCount      int64                   `json:"rootSpanCount"`
	MissingParentCount int64                   `json:"missingParentCount"`
	Conversations      []query.ConversationRef `json:"conversations"`
	Agents             []query.TraceAgent      `json:"agents"`
	Activities         []ActivityOutput        `json:"activities"`
}

type TraceOutput struct {
	Trace TraceDataOutput `json:"trace"`
}

type Reader interface {
	query.DashboardReader
	query.SessionListReader
	query.SessionActivitiesReader
	query.TraceReader
}

type Service struct {
	reader Reader
	now    Clock
}

func New(reader Reader, now Clock) http.Handler {
	service := &Service{reader: reader, now: now}
	server := mcp.NewServer(&mcp.Implementation{
		Name:        "agentmetry",
		Title:       "Agentmetry Agent Trace Lab",
		Description: "Inspect local agent sessions, subagents, messages, traces, and token usage.",
		Version:     "0.1.0-poc",
	}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_agent_sessions",
		Title:       "Get agent sessions",
		Description: "Returns bounded dashboard aggregates and session summaries. Operations and messages are intentionally fetched separately.",
	}, service.getOverview)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_session_activities",
		Title:       "Get session activities",
		Description: "Returns one bounded page of operations and messages for a source-qualified session.",
	}, service.getSessionActivities)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_trace",
		Title:       "Get trace",
		Description: "Returns a trace-wide causal view with spans, correlated logs, conversations, agents, status, and missing-parent evidence.",
	}, service.getTrace)
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:                    true,
			JSONResponse:                 true,
			PropagateRequestCancellation: true,
		},
	)
}

func (service *Service) getTrace(ctx context.Context, _ *mcp.CallToolRequest, input TraceInput) (*mcp.CallToolResult, TraceOutput, error) {
	traceID, err := query.ParseTraceID(input.TraceID)
	if err != nil {
		return nil, TraceOutput{}, err
	}
	trace, err := service.reader.GetTrace(ctx, query.TraceFilter{TraceID: traceID})
	if err != nil {
		return nil, TraceOutput{}, err
	}
	return nil, TraceOutput{Trace: mapTrace(trace)}, nil
}

func (service *Service) getOverview(ctx context.Context, _ *mcp.CallToolRequest, input OverviewInput) (*mcp.CallToolResult, OverviewOutput, error) {
	filter, err := query.FilterForRange(service.now(), input.Range)
	if err != nil {
		return nil, OverviewOutput{}, err
	}
	filter.SourceID, filter.Search = input.Source, input.Search
	dashboard, err := service.reader.GetDashboard(ctx, query.DashboardFilter{Since: filter.Since, SourceID: input.Source, Search: input.Search})
	if err != nil {
		return nil, OverviewOutput{}, err
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, OverviewOutput{}, fmt.Errorf("pageSize must be between 1 and 100")
	}
	offset, err := parsePageToken(input.PageToken)
	if err != nil {
		return nil, OverviewOutput{}, err
	}
	sessions, err := service.reader.ListSessions(ctx, query.SessionListFilter{Since: filter.Since, SourceID: input.Source, Search: input.Search, PageSize: pageSize, Offset: offset})
	if err != nil {
		return nil, OverviewOutput{}, err
	}
	output := OverviewOutput{Overview: mapDashboardAndSessions(dashboard, sessions.Sessions)}
	if sessions.HasMore {
		output.NextPageToken = encodePageToken(sessions.NextOffset)
	}
	if offset > 0 {
		output.PreviousPageToken = encodePageToken(max(0, offset-pageSize))
	}
	return nil, output, nil
}

func (service *Service) getSessionActivities(ctx context.Context, _ *mcp.CallToolRequest, input SessionActivitiesInput) (*mcp.CallToolResult, SessionActivitiesOutput, error) {
	if input.Source == "" || input.SessionID == "" {
		return nil, SessionActivitiesOutput{}, fmt.Errorf("source and sessionId are required")
	}
	pageSize := input.PageSize
	if pageSize == 0 {
		pageSize = 100
	}
	if pageSize < 1 || pageSize > 100 {
		return nil, SessionActivitiesOutput{}, fmt.Errorf("pageSize must be between 1 and 100")
	}
	offset, err := parsePageToken(input.PageToken)
	if err != nil {
		return nil, SessionActivitiesOutput{}, err
	}
	direction := input.Direction
	if direction == "" {
		direction = "older"
	}
	if direction != "older" && direction != "newer" {
		return nil, SessionActivitiesOutput{}, fmt.Errorf("direction must be older or newer")
	}
	page, err := service.reader.ListSessionActivities(ctx, query.ActivityPageFilter{SourceID: input.Source, ConversationID: input.SessionID, PageSize: pageSize, Offset: offset, Direction: direction})
	if err != nil {
		return nil, SessionActivitiesOutput{}, err
	}
	output := SessionActivitiesOutput{Source: input.Source, SessionID: input.SessionID, Total: page.Total, HasEarlier: page.HasEarlier, HasMore: page.HasMore, Activities: make([]ActivityOutput, 0, len(page.Activities))}
	for _, activity := range page.Activities {
		output.Activities = append(output.Activities, mapActivity(activity))
	}
	if page.HasMore {
		output.NextPageToken = encodePageToken(page.Offset + len(page.Activities))
	}
	if page.HasEarlier {
		output.PreviousPageToken = encodePageToken(max(0, page.Offset-pageSize))
	}
	return nil, output, nil
}

func parsePageToken(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || !strings.HasPrefix(string(decoded), "agentmetry:v1:") {
		return 0, fmt.Errorf("invalid pageToken")
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(string(decoded), "agentmetry:v1:"))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid pageToken")
	}
	return offset, nil
}

func encodePageToken(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("agentmetry:v1:%d", offset)))
}

func mapDashboardAndSessions(overview query.Overview, sessions []query.Session) OverviewDataOutput {
	output := mapOverview(overview)
	output.Sessions = make([]SessionOutput, 0, len(sessions))
	for _, session := range sessions {
		output.Sessions = append(output.Sessions, mapSessionSummary(session))
	}
	return output
}

func mapSessionSummary(session query.Session) SessionOutput {
	return SessionOutput{
		ID: session.ID, SourceID: session.SourceID, Sources: session.Sources, TraceIDs: session.TraceIDs,
		StartedAt: session.StartedAt, EndedAt: session.EndedAt, ActivityCount: session.ActivityCount,
		Tokens: mapTokens(session.Tokens), CostUSD: session.CostUSD,
		Agents: make([]AgentSessionOutput, 0), Activities: make([]ActivityOutput, 0),
	}
}

func mapOverview(overview query.Overview) OverviewDataOutput {
	output := OverviewDataOutput{
		Sources: overview.Sources,
		SignalCounts: SignalCountsOutput{
			Traces: overview.SignalCounts.Traces, Logs: overview.SignalCounts.Logs, Metrics: overview.SignalCounts.Metrics,
		},
		RunCount: overview.RunCount, AgentCount: overview.AgentCount, Tokens: mapTokens(overview.Tokens),
		PlanUsage: overview.PlanUsage,
	}
	for _, activity := range overview.RecentActivity {
		output.RecentActivity = append(output.RecentActivity, mapActivity(activity))
	}
	for _, session := range overview.Sessions {
		mapped := SessionOutput{
			ID: session.ID, SourceID: session.SourceID, Sources: session.Sources, TraceIDs: session.TraceIDs, StartedAt: session.StartedAt, EndedAt: session.EndedAt,
			ActivityCount: session.ActivityCount, Tokens: mapTokens(session.Tokens), CostUSD: session.CostUSD,
		}
		for _, agent := range session.Agents {
			mapped.Agents = append(mapped.Agents, AgentSessionOutput{
				AgentID: agent.AgentID, AgentDefinition: agent.AgentDefinition, AgentType: agent.AgentType,
				ParentAgentID: agent.ParentAgentID, Model: agent.Model,
				ActivityCount: agent.ActivityCount, Tokens: mapTokens(agent.Tokens),
			})
		}
		for _, activity := range session.Activities {
			mapped.Activities = append(mapped.Activities, mapActivity(activity))
		}
		output.Sessions = append(output.Sessions, mapped)
	}
	return output
}

func mapTrace(trace query.Trace) TraceDataOutput {
	output := TraceDataOutput{
		TraceID: trace.TraceID, StartedAt: trace.StartedAt, EndedAt: trace.EndedAt, Status: string(trace.Status),
		RootSpanCount: trace.RootSpanCount, MissingParentCount: trace.MissingParentCount,
		Conversations: append([]query.ConversationRef{}, trace.Conversations...),
		Agents:        append([]query.TraceAgent{}, trace.Agents...),
		Activities:    make([]ActivityOutput, 0, len(trace.Activities)),
	}
	for _, activity := range trace.Activities {
		output.Activities = append(output.Activities, mapActivity(activity))
	}
	return output
}

func mapActivity(activity query.Activity) ActivityOutput {
	return ActivityOutput{
		Source: activity.Source, Signal: string(activity.Signal), TraceID: activity.TraceID, SpanID: activity.SpanID,
		ParentSpanID: activity.ParentSpanID, Name: activity.Name, Kind: string(activity.Kind),
		ToolName: activity.ToolName, TargetAgentID: activity.TargetAgentID, TargetAgentType: activity.TargetAgentType, Content: activity.Content,
		AgentID: activity.AgentID, AgentDefinition: activity.AgentDefinition,
		AgentType: activity.AgentType, ParentAgentID: activity.ParentAgentID,
		RunID: activity.RunID, Model: activity.Model, StartedAt: activity.StartedAt, EndedAt: activity.EndedAt,
		ObservedAt: activity.ObservedAt, Status: activity.Status,
		Tokens: mapTokens(activity.Tokens), CostUSD: activity.CostUSD, ContributesToTotal: activity.ContributesToTotal,
	}
}

func mapTokens(tokens canonical.TokenUsage) TokenUsageOutput {
	total := tokens.Total()
	return TokenUsageOutput{
		Input:      reportedToken(tokens.Input, tokens.InputReported()),
		Output:     reportedToken(tokens.Output, tokens.OutputReported()),
		CacheRead:  reportedToken(tokens.CacheRead, tokens.CacheReadReported()),
		CacheWrite: reportedToken(tokens.CacheWrite, tokens.CacheWriteReported()),
		Reasoning:  reportedToken(tokens.Reasoning, tokens.ReasoningReported()),
		Total:      reportedToken(total, tokens.TotalReported()),
	}
}

func reportedToken(value int64, reported bool) *int64 {
	if !reported {
		return nil
	}
	return &value
}
