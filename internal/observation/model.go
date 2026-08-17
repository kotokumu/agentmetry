package observation

import (
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
)

// Observation is a source-neutral, append-only semantic projection of one OTLP
// record. The lossless protobuf journal, rather than this read model, owns the
// replayable source data.
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
	NormalizerVersion int
}
