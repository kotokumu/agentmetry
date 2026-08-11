package claude

import (
	"encoding/json"
	"fmt"
)

// CLIUsage is the usage oracle returned by Claude Code's JSON print mode.
// InputTokens is the uncached input component; the complete input count is the
// sum of InputTokens, CacheReadInputTokens, and CacheCreationInputTokens.
type CLIUsage struct {
	InputTokens              int64
	OutputTokens             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
	TotalCostUSD             *float64
}

// ParseCLIResult extracts provider-reported usage from `claude -p
// --output-format json`. The required input/output fields make a malformed or
// changed provider response fail loudly instead of silently producing zeroes.
func ParseCLIResult(payload []byte) (CLIUsage, error) {
	var result struct {
		Usage *struct {
			InputTokens              *int64 `json:"input_tokens"`
			OutputTokens             *int64 `json:"output_tokens"`
			CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		TotalCostUSD *float64 `json:"total_cost_usd"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return CLIUsage{}, fmt.Errorf("decode Claude JSON result: %w", err)
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || result.Usage.OutputTokens == nil {
		return CLIUsage{}, fmt.Errorf("Claude JSON result has no complete usage object")
	}
	usage := CLIUsage{
		InputTokens:  *result.Usage.InputTokens,
		OutputTokens: *result.Usage.OutputTokens,
		TotalCostUSD: result.TotalCostUSD,
	}
	if result.Usage.CacheReadInputTokens != nil {
		usage.CacheReadInputTokens = *result.Usage.CacheReadInputTokens
	}
	if result.Usage.CacheCreationInputTokens != nil {
		usage.CacheCreationInputTokens = *result.Usage.CacheCreationInputTokens
	}
	if usage.InputTokens < 0 || usage.OutputTokens < 0 || usage.CacheReadInputTokens < 0 || usage.CacheCreationInputTokens < 0 {
		return CLIUsage{}, fmt.Errorf("Claude JSON result contains negative usage")
	}
	if usage.TotalCostUSD != nil && *usage.TotalCostUSD < 0 {
		return CLIUsage{}, fmt.Errorf("Claude JSON result contains negative cost")
	}
	return usage, nil
}
