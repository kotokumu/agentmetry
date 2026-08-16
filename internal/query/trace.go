package query

import (
	"context"
	"errors"
	"time"
)

var ErrTraceNotFound = errors.New("trace not found")

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
	Page    Page
	Tail    bool
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
