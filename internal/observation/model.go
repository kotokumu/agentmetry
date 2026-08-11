package observation

import (
	"encoding/json"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
)

// Observation is a source-neutral, append-only projection of one OTLP record.
// Payload retains the complete OTLP JSON subtree needed to reinterpret it.
type Observation struct {
	Ordinal           int
	Signal            canonical.Signal
	Kind              canonical.ActivityKind
	Source            string
	SourceEventName   string
	OccurredAt        time.Time
	ObservedAt        time.Time
	TraceID           string
	SpanID            string
	ParentSpanID      string
	SessionID         string
	AgentID           string
	AgentDefinition   string
	AgentType         string
	ParentAgentID     string
	Model             string
	Usage             canonical.TokenUsage
	Payload           json.RawMessage
	SourceAttributes  json.RawMessage
	NormalizerVersion int
}
