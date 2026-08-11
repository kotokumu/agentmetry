package query

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidTraceID = errors.New("invalid OTLP trace ID")
	ErrInvalidSpanID  = errors.New("invalid OTLP span ID")
	ErrTraceNotFound  = errors.New("trace not found")
)

type TraceStatus string

const (
	TraceStatusUnknown TraceStatus = "unknown"
	TraceStatusOK      TraceStatus = "ok"
	TraceStatusError   TraceStatus = "error"
)

func ParseTraceID(value string) (string, error) {
	return parseOTLPID(value, 32, ErrInvalidTraceID)
}

func ParseSpanID(value string) (string, error) {
	return parseOTLPID(value, 16, ErrInvalidSpanID)
}

func parseOTLPID(value string, length int, kind error) (string, error) {
	if len(value) != length || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("%w: expected %d hexadecimal characters", kind, length)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("%w: expected %d hexadecimal characters", kind, length)
	}
	allZero := true
	for _, part := range decoded {
		if part != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return "", fmt.Errorf("%w: zero is not a valid identifier", kind)
	}
	return strings.ToLower(value), nil
}

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
	TraceID string
	Offset  int
	Limit   int
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
