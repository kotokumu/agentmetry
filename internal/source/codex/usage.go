package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// JSONUsage is the provider-reported usage from a Codex exec JSONL turn.
// OpenAI/Codex input_tokens already includes cached_input_tokens; the cached
// value is retained as a breakdown and must not be added again.
type JSONUsage struct {
	InputTokens           int64
	CachedInputTokens     int64
	OutputTokens          int64
	ReasoningOutputTokens int64
}

// ParseExecJSON extracts and aggregates turn.completed usage from `codex exec
// --json`. A single invocation may contain more than one completed turn, so
// every usage event is included in the result.
func ParseExecJSON(input io.Reader) (JSONUsage, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var total JSONUsage
	var found bool
	for scanner.Scan() {
		var event struct {
			Type  string `json:"type"`
			Usage *struct {
				InputTokens           *int64 `json:"input_tokens"`
				CachedInputTokens     *int64 `json:"cached_input_tokens"`
				OutputTokens          *int64 `json:"output_tokens"`
				ReasoningOutputTokens *int64 `json:"reasoning_output_tokens"`
				TotalTokens           *int64 `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return JSONUsage{}, fmt.Errorf("decode Codex JSONL event: %w", err)
		}
		if event.Type != "turn.completed" || event.Usage == nil || event.Usage.InputTokens == nil || event.Usage.OutputTokens == nil {
			continue
		}
		usage := JSONUsage{
			InputTokens:           *event.Usage.InputTokens,
			OutputTokens:          *event.Usage.OutputTokens,
			CachedInputTokens:     valueOrZero(event.Usage.CachedInputTokens),
			ReasoningOutputTokens: valueOrZero(event.Usage.ReasoningOutputTokens),
		}
		if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CachedInputTokens < 0 || usage.ReasoningOutputTokens < 0 {
			return JSONUsage{}, fmt.Errorf("Codex JSONL event contains negative usage")
		}
		if usage.CachedInputTokens > usage.InputTokens {
			return JSONUsage{}, fmt.Errorf("Codex JSONL event cached input exceeds input")
		}
		if usage.ReasoningOutputTokens > usage.OutputTokens {
			return JSONUsage{}, fmt.Errorf("Codex JSONL event reasoning output exceeds output")
		}
		if event.Usage.TotalTokens != nil {
			if *event.Usage.TotalTokens < 0 {
				return JSONUsage{}, fmt.Errorf("Codex JSONL event contains negative total")
			}
			computedTotal, ok := addNonNegative(usage.InputTokens, usage.OutputTokens)
			if !ok || *event.Usage.TotalTokens != computedTotal {
				return JSONUsage{}, fmt.Errorf("Codex JSONL event total does not match input and output")
			}
		}
		if total.InputTokens > maxInt64-usage.InputTokens || total.CachedInputTokens > maxInt64-usage.CachedInputTokens || total.OutputTokens > maxInt64-usage.OutputTokens || total.ReasoningOutputTokens > maxInt64-usage.ReasoningOutputTokens {
			return JSONUsage{}, fmt.Errorf("Codex JSONL usage overflows int64")
		}
		total.InputTokens += usage.InputTokens
		total.CachedInputTokens += usage.CachedInputTokens
		total.OutputTokens += usage.OutputTokens
		total.ReasoningOutputTokens += usage.ReasoningOutputTokens
		found = true
	}
	if err := scanner.Err(); err != nil {
		return JSONUsage{}, fmt.Errorf("read Codex JSONL output: %w", err)
	}
	if !found {
		return JSONUsage{}, fmt.Errorf("Codex JSONL output has no complete turn.completed usage")
	}
	return total, nil
}

const maxInt64 = int64(^uint64(0) >> 1)

func valueOrZero(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func addNonNegative(first, second int64) (int64, bool) {
	if first > maxInt64-second {
		return 0, false
	}
	return first + second, true
}
