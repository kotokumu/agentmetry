package canonical

import (
	"encoding/json"
	"time"
)

type Signal string

type ActivityKind string

const (
	SignalTrace  Signal = "trace"
	SignalLog    Signal = "log"
	SignalMetric Signal = "metric"

	ActivityUnknown    ActivityKind = "unknown"
	ActivityPrompt     ActivityKind = "prompt"
	ActivityResponse   ActivityKind = "response"
	ActivityTool       ActivityKind = "tool"
	ActivityDelegation ActivityKind = "delegation"
	ActivityMessage    ActivityKind = "message"
	ActivityReasoning  ActivityKind = "reasoning"
)

type TokenUsage struct {
	// Input is the complete input count. Provider adapters must include cache
	// reads and writes here; the cache fields below are breakdowns of Input.
	Input int64 `json:"-"`
	// Output is the provider-reported output total. Reasoning is a breakdown
	// of Output and must not be added to it.
	Output           int64         `json:"-"`
	CacheRead        int64         `json:"-"`
	CacheWrite       int64         `json:"-"`
	Reasoning        int64         `json:"-"`
	Presence         TokenPresence `json:"-"`
	inputIncomplete  bool
	outputIncomplete bool
}

// TokenPresence records explicitly reported zero values. Non-zero counters
// are inherently reported, so this state is only required to distinguish an
// observed zero from a missing measurement.
type TokenPresence struct {
	Input      bool
	Output     bool
	CacheRead  bool
	CacheWrite bool
	Reasoning  bool
}

func (usage TokenUsage) Total() int64 {
	return usage.Input + usage.Output
}

func (usage TokenUsage) InputReported() bool  { return usage.Input != 0 || usage.Presence.Input }
func (usage TokenUsage) OutputReported() bool { return usage.Output != 0 || usage.Presence.Output }
func (usage TokenUsage) CacheReadReported() bool {
	return usage.CacheRead != 0 || usage.Presence.CacheRead
}
func (usage TokenUsage) CacheWriteReported() bool {
	return usage.CacheWrite != 0 || usage.Presence.CacheWrite
}
func (usage TokenUsage) ReasoningReported() bool {
	return usage.Reasoning != 0 || usage.Presence.Reasoning
}
func (usage TokenUsage) AnyReported() bool {
	return usage.InputReported() || usage.OutputReported() || usage.CacheReadReported() || usage.CacheWriteReported() || usage.ReasoningReported()
}

func (usage TokenUsage) TotalReported() bool {
	return usage.InputReported() && usage.OutputReported() && !usage.inputIncomplete && !usage.outputIncomplete
}

func (usage *TokenUsage) Add(other TokenUsage) {
	if other.AnyReported() && !other.InputReported() {
		usage.inputIncomplete = true
	}
	if other.AnyReported() && !other.OutputReported() {
		usage.outputIncomplete = true
	}
	usage.inputIncomplete = usage.inputIncomplete || other.inputIncomplete
	usage.outputIncomplete = usage.outputIncomplete || other.outputIncomplete
	inputReported := usage.InputReported() || other.InputReported()
	outputReported := usage.OutputReported() || other.OutputReported()
	cacheReadReported := usage.CacheReadReported() || other.CacheReadReported()
	cacheWriteReported := usage.CacheWriteReported() || other.CacheWriteReported()
	reasoningReported := usage.ReasoningReported() || other.ReasoningReported()
	usage.Input += other.Input
	usage.Output += other.Output
	usage.CacheRead += other.CacheRead
	usage.CacheWrite += other.CacheWrite
	usage.Reasoning += other.Reasoning
	usage.Presence.Input = inputReported && usage.Input == 0
	usage.Presence.Output = outputReported && usage.Output == 0
	usage.Presence.CacheRead = cacheReadReported && usage.CacheRead == 0
	usage.Presence.CacheWrite = cacheWriteReported && usage.CacheWrite == 0
	usage.Presence.Reasoning = reasoningReported && usage.Reasoning == 0
}

func (usage TokenUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Input      *int64 `json:"input"`
		Output     *int64 `json:"output"`
		CacheRead  *int64 `json:"cacheRead"`
		CacheWrite *int64 `json:"cacheWrite"`
		Reasoning  *int64 `json:"reasoning"`
		Total      *int64 `json:"total"`
	}{
		Input:      reportedValue(usage.Input, usage.InputReported()),
		Output:     reportedValue(usage.Output, usage.OutputReported()),
		CacheRead:  reportedValue(usage.CacheRead, usage.CacheReadReported()),
		CacheWrite: reportedValue(usage.CacheWrite, usage.CacheWriteReported()),
		Reasoning:  reportedValue(usage.Reasoning, usage.ReasoningReported()),
		Total:      reportedValue(usage.Total(), usage.TotalReported()),
	})
}

func (usage *TokenUsage) UnmarshalJSON(payload []byte) error {
	var encoded struct {
		Input      *int64 `json:"input"`
		Output     *int64 `json:"output"`
		CacheRead  *int64 `json:"cacheRead"`
		CacheWrite *int64 `json:"cacheWrite"`
		Reasoning  *int64 `json:"reasoning"`
		Total      *int64 `json:"total"`
	}
	if err := json.Unmarshal(payload, &encoded); err != nil {
		return err
	}
	*usage = TokenUsage{}
	assignReportedValue(&usage.Input, &usage.Presence.Input, encoded.Input)
	assignReportedValue(&usage.Output, &usage.Presence.Output, encoded.Output)
	assignReportedValue(&usage.CacheRead, &usage.Presence.CacheRead, encoded.CacheRead)
	assignReportedValue(&usage.CacheWrite, &usage.Presence.CacheWrite, encoded.CacheWrite)
	assignReportedValue(&usage.Reasoning, &usage.Presence.Reasoning, encoded.Reasoning)
	if encoded.Total == nil && encoded.Input != nil && encoded.Output != nil {
		usage.inputIncomplete = true
	}
	return nil
}

func assignReportedValue(target *int64, explicitZero *bool, value *int64) {
	if value == nil {
		return
	}
	*target = *value
	*explicitZero = *value == 0
}

func reportedValue(value int64, reported bool) *int64 {
	if !reported {
		return nil
	}
	return &value
}

type AgentContext struct {
	AgentID         string     `json:"agentId"`
	AgentDefinition string     `json:"agentDefinition"`
	AgentType       string     `json:"agentType"`
	ParentAgentID   string     `json:"parentAgentId"`
	RunID           string     `json:"runId"`
	Model           string     `json:"model"`
	Tokens          TokenUsage `json:"tokens"`
}

type Span struct {
	Source          string         `json:"source,omitempty"`
	TraceID         string         `json:"traceId"`
	SpanID          string         `json:"spanId"`
	ParentSpanID    string         `json:"parentSpanId,omitempty"`
	Name            string         `json:"name"`
	StartedAt       time.Time      `json:"startedAt"`
	EndedAt         time.Time      `json:"endedAt"`
	Status          string         `json:"status"`
	Kind            ActivityKind   `json:"kind"`
	ToolName        string         `json:"toolName,omitempty"`
	TargetAgentID   string         `json:"targetAgentId,omitempty"`
	TargetAgentType string         `json:"targetAgentType,omitempty"`
	Content         string         `json:"content,omitempty"`
	CostUSD         *float64       `json:"costUsd,omitempty"`
	Attributes      map[string]any `json:"attributes"`
	Agent           AgentContext   `json:"agent"`
}

type Log struct {
	Source          string         `json:"source,omitempty"`
	ObservedAt      time.Time      `json:"observedAt"`
	Severity        string         `json:"severity"`
	Name            string         `json:"name"`
	Body            string         `json:"body"`
	TraceID         string         `json:"traceId,omitempty"`
	SpanID          string         `json:"spanId,omitempty"`
	Kind            ActivityKind   `json:"kind"`
	ToolName        string         `json:"toolName,omitempty"`
	TargetAgentID   string         `json:"targetAgentId,omitempty"`
	TargetAgentType string         `json:"targetAgentType,omitempty"`
	CostUSD         *float64       `json:"costUsd,omitempty"`
	Attributes      map[string]any `json:"attributes"`
	Agent           AgentContext   `json:"agent"`
}

type MetricPoint struct {
	Source     string         `json:"source,omitempty"`
	ObservedAt time.Time      `json:"observedAt"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Value      float64        `json:"value"`
	CostUSD    *float64       `json:"costUsd,omitempty"`
	Attributes map[string]any `json:"attributes"`
	Agent      AgentContext   `json:"agent"`
}

type Batch struct {
	Signal  Signal        `json:"signal"`
	Spans   []Span        `json:"spans,omitempty"`
	Logs    []Log         `json:"logs,omitempty"`
	Metrics []MetricPoint `json:"metrics,omitempty"`
}
