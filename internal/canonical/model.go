package canonical

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrInvalidTokenUsage identifies a usage observation that violates the
// canonical token relationships.
var ErrInvalidTokenUsage = errors.New("invalid token usage")

type Signal string

type ActivityKind string

// Operation is the producer-neutral development action used by session
// efficiency analysis. It is intentionally smaller than the provider tool
// vocabulary so Claude and Codex activities can be compared.
type Operation string

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

	OperationRead    Operation = "read"
	OperationEdit    Operation = "edit"
	OperationExecute Operation = "execute"
	OperationTest    Operation = "test"
	OperationBuild   Operation = "build"
	OperationLint    Operation = "lint"
	OperationAPICall Operation = "api_call"
	OperationOther   Operation = "other"
)

type EventTarget struct {
	File    string `json:"file,omitempty"`
	Command string `json:"command,omitempty"`
}

// Event is a read-time, producer-neutral projection used by analyzers. Success
// is optional because missing outcome telemetry must not be interpreted as a
// successful attempt.
type Event struct {
	Source             string        `json:"source"`
	RunID              string        `json:"runId"`
	AgentID            string        `json:"agentId,omitempty"`
	ParentAgentID      string        `json:"parentAgentId,omitempty"`
	Operation          Operation     `json:"operation"`
	Target             EventTarget   `json:"target"`
	Success            *bool         `json:"success"`
	StartedAt          time.Time     `json:"startedAt"`
	EndedAt            time.Time     `json:"endedAt"`
	ObservedAt         time.Time     `json:"observedAt"`
	Duration           time.Duration `json:"duration"`
	Tokens             TokenUsage    `json:"tokenUsage"`
	ContributesToTotal bool          `json:"-"`
	TraceID            string        `json:"traceId,omitempty"`
	SpanID             string        `json:"spanId,omitempty"`
	Name               string        `json:"name"`
	ToolName           string        `json:"toolName,omitempty"`
	Tool               bool          `json:"-"`
}

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

// Validate checks relationships that must hold for a provider-reported usage
// observation. Missing breakdowns remain valid because a source may report a
// partial observation, but every reported component must be non-negative and
// consistent with the reported primary counter.
func (usage TokenUsage) Validate() error {
	for name, value := range map[string]int64{
		"input": usage.Input, "output": usage.Output, "cacheRead": usage.CacheRead,
		"cacheWrite": usage.CacheWrite, "reasoning": usage.Reasoning,
	} {
		if value < 0 {
			return fmt.Errorf("%w: %s is negative", ErrInvalidTokenUsage, name)
		}
	}

	if usage.InputReported() && (usage.CacheReadReported() || usage.CacheWriteReported()) {
		cacheTotal, ok := addNonNegative(usage.CacheRead, usage.CacheWrite)
		if !ok {
			return fmt.Errorf("%w: cache breakdown overflows int64", ErrInvalidTokenUsage)
		}
		if cacheTotal > usage.Input {
			return fmt.Errorf("%w: cache breakdown %d exceeds input %d", ErrInvalidTokenUsage, cacheTotal, usage.Input)
		}
	}
	if usage.OutputReported() && usage.ReasoningReported() && usage.Reasoning > usage.Output {
		return fmt.Errorf("%w: reasoning %d exceeds output %d", ErrInvalidTokenUsage, usage.Reasoning, usage.Output)
	}
	if usage.InputReported() && usage.OutputReported() {
		if _, ok := addNonNegative(usage.Input, usage.Output); !ok {
			return fmt.Errorf("%w: input and output overflow int64", ErrInvalidTokenUsage)
		}
	}
	return nil
}

func addNonNegative(first, second int64) (int64, bool) {
	const maxInt64 = int64(^uint64(0) >> 1)
	if first > maxInt64-second {
		return 0, false
	}
	return first + second, true
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

// SessionLink records a source-authoritative parent/child relationship between
// conversations. Source profiles decide when an event has this meaning; query
// storage never infers it from producer-specific attributes.
type SessionLink struct {
	Source          string    `json:"source,omitempty"`
	ParentSessionID string    `json:"parentSessionId"`
	ChildSessionID  string    `json:"childSessionId"`
	ObservedAt      time.Time `json:"observedAt"`
}

type Batch struct {
	Signal       Signal        `json:"signal"`
	Spans        []Span        `json:"spans,omitempty"`
	Logs         []Log         `json:"logs,omitempty"`
	Metrics      []MetricPoint `json:"metrics,omitempty"`
	SessionLinks []SessionLink `json:"sessionLinks,omitempty"`
}
