package query

import (
	"context"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/planusage"
)

type OverviewFilter struct {
	Since          time.Time
	SourceID       string
	Search         string
	ActivityOffset int
	ActivityLimit  int
}

type TelemetrySource struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type SignalCounts struct {
	Traces  int64 `json:"traces"`
	Logs    int64 `json:"logs"`
	Metrics int64 `json:"metrics"`
}

type Activity struct {
	Source             string                 `json:"source"`
	Signal             canonical.Signal       `json:"signal"`
	TraceID            string                 `json:"traceId,omitempty"`
	SpanID             string                 `json:"spanId,omitempty"`
	ParentSpanID       string                 `json:"parentSpanId,omitempty"`
	Name               string                 `json:"name"`
	Kind               canonical.ActivityKind `json:"kind"`
	ToolName           string                 `json:"toolName,omitempty"`
	TargetAgentID      string                 `json:"targetAgentId,omitempty"`
	TargetAgentType    string                 `json:"targetAgentType,omitempty"`
	Content            string                 `json:"content,omitempty"`
	AgentID            string                 `json:"agentId"`
	AgentDefinition    string                 `json:"agentDefinition,omitempty"`
	AgentType          string                 `json:"agentType,omitempty"`
	ParentAgentID      string                 `json:"parentAgentId,omitempty"`
	RunID              string                 `json:"runId"`
	Model              string                 `json:"model"`
	StartedAt          time.Time              `json:"startedAt"`
	EndedAt            time.Time              `json:"endedAt"`
	ObservedAt         time.Time              `json:"observedAt"`
	Status             string                 `json:"status,omitempty"`
	Tokens             canonical.TokenUsage   `json:"tokens"`
	CostUSD            *float64               `json:"costUsd,omitempty"`
	ContributesToTotal bool                   `json:"contributesToTotal"`
	PromptID           string                 `json:"promptId,omitempty"`
	UsageID            string                 `json:"usageId,omitempty"`
	RelatedTraceID     string                 `json:"relatedTraceId,omitempty"`
	RelatedSpanID      string                 `json:"relatedSpanId,omitempty"`
	UsageRole          string                 `json:"-"`
}

type AgentSession struct {
	AgentID         string               `json:"agentId"`
	AgentDefinition string               `json:"agentDefinition,omitempty"`
	AgentType       string               `json:"agentType,omitempty"`
	ParentAgentID   string               `json:"parentAgentId,omitempty"`
	Model           string               `json:"model,omitempty"`
	ActivityCount   int64                `json:"activityCount"`
	Tokens          canonical.TokenUsage `json:"tokens"`
}

type Session struct {
	ID             string               `json:"id"`
	SourceID       string               `json:"sourceId"`
	Sources        []TelemetrySource    `json:"sources"`
	TraceIDs       []string             `json:"traceIds"`
	StartedAt      time.Time            `json:"startedAt"`
	EndedAt        time.Time            `json:"endedAt"`
	ActivityCount  int64                `json:"activityCount"`
	AgentCount     int64                `json:"agentCount"`
	ActivityOffset int                  `json:"activityOffset,omitempty"`
	HasEarlier     bool                 `json:"hasEarlier,omitempty"`
	HasMore        bool                 `json:"hasMore,omitempty"`
	Tokens         canonical.TokenUsage `json:"tokens"`
	CostUSD        *float64             `json:"costUsd,omitempty"`
	Agents         []AgentSession       `json:"agents"`
	Activities     []Activity           `json:"activities"`
}

type Overview struct {
	Sources        []TelemetrySource    `json:"sources"`
	SignalCounts   SignalCounts         `json:"signalCounts"`
	RunCount       int64                `json:"runCount"`
	AgentCount     int64                `json:"agentCount"`
	Tokens         canonical.TokenUsage `json:"tokens"`
	RecentActivity []Activity           `json:"recentActivity"`
	Sessions       []Session            `json:"sessions"`
	PlanUsage      []planusage.Snapshot `json:"planUsage"`
}

type OverviewReader interface {
	GetOverview(context.Context, OverviewFilter) (Overview, error)
}
