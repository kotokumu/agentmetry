package query

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
)

var ErrTraceNotFound = errors.New("trace not found")

var ErrTraceTargetNotFound = errors.New("trace target span not found")

type TraceStatus string

const (
	TraceStatusUnknown TraceStatus = "unknown"
	TraceStatusOK      TraceStatus = "ok"
	TraceStatusError   TraceStatus = "error"
)

type ConversationRef struct {
	SourceID string `json:"sourceId"`
	ID       string `json:"id"`
}

type TraceAgent struct {
	SourceID        string `json:"sourceId"`
	ConversationID  string `json:"conversationId"`
	AgentID         string `json:"agentId"`
	AgentDefinition string `json:"agentDefinition,omitempty"`
	AgentType       string `json:"agentType,omitempty"`
	ParentAgentID   string `json:"parentAgentId,omitempty"`
	Model           string `json:"model,omitempty"`
}

type TraceFilter struct {
	TraceID TraceID
	// SpanID selects the page containing this native span, ahead of Page and Tail.
	// The zero value leaves the page selection unchanged.
	SpanID SpanID
	Page   Page
	Tail   bool
}

type Trace struct {
	TraceID            string            `json:"traceId"`
	StartedAt          time.Time         `json:"startedAt"`
	EndedAt            time.Time         `json:"endedAt"`
	Status             TraceStatus       `json:"status"`
	RootSpanCount      int64             `json:"rootSpanCount"`
	MissingParentCount int64             `json:"missingParentCount"`
	Conversations      []ConversationRef `json:"conversations"`
	Agents             []TraceAgent      `json:"agents"`
	Activities         []Activity        `json:"activities"`
	ActivityOffset     int               `json:"activityOffset"`
	ActivityCount      int64             `json:"activityCount"`
	HasMore            bool              `json:"hasMore"`
}

type TraceReader interface {
	GetTrace(context.Context, TraceFilter) (Trace, error)
}

const TraceOverviewLimit = 5000
const TraceOverviewCoverageComplete = "complete"
const TraceOverviewCoveragePartial = "partial"

type TraceOverviewActivity struct {
	ID, Source, SpanID, ParentSpanID, Name, Status string
	Signal                                         canonical.Signal
	Kind                                           canonical.ActivityKind
	StartedAt, EndedAt                             time.Time
	MissingParent                                  bool
}

type TraceOverview struct {
	TraceID                             string
	StartedAt, EndedAt                  time.Time
	TotalActivities, ReturnedActivities int64
	Coverage                            string
	Activities                          []TraceOverviewActivity
}

type TraceWindow struct {
	StartedAt, EndedAt *time.Time
	Kind               canonical.ActivityKind
	ErrorsOnly         bool
}

type TraceWindowFilter struct {
	TraceID TraceID
	Window  TraceWindow
	Page    Page
}
type TraceWindowResult struct {
	Trace              Trace
	MatchingActivities int64
}

type TraceOverviewReader interface {
	GetTraceOverview(context.Context, TraceID) (TraceOverview, error)
}
type TraceWindowReader interface {
	GetTraceWindow(context.Context, TraceWindowFilter) (TraceWindowResult, error)
}

func ValidateTraceWindow(window TraceWindow) error {
	if (window.StartedAt == nil) != (window.EndedAt == nil) {
		return fmt.Errorf("startedAt and endedAt must be provided together")
	}
	if window.StartedAt != nil && (window.StartedAt.IsZero() || window.EndedAt.IsZero() || window.StartedAt.After(*window.EndedAt)) {
		return fmt.Errorf("trace window must be a valid ordered range")
	}
	switch window.Kind {
	case "", canonical.ActivityPrompt, canonical.ActivityResponse, canonical.ActivityTool, canonical.ActivityDelegation, canonical.ActivityMessage, canonical.ActivityReasoning, canonical.ActivityUnknown:
	default:
		return fmt.Errorf("unsupported activity kind %q", window.Kind)
	}
	return nil
}

func TraceWindowIncludes(window TraceWindow, activity TraceOverviewActivity) bool {
	if window.StartedAt != nil && (activity.EndedAt.Before(*window.StartedAt) || activity.StartedAt.After(*window.EndedAt)) {
		return false
	}
	if window.Kind != "" && activity.Kind != window.Kind {
		return false
	}
	if window.ErrorsOnly && activity.Status != string(TraceStatusError) {
		return false
	}
	return true
}
