// Package billing contains deterministic, provider-neutral cost calculations.
//
// Provider adapters are responsible for normalizing usage into the OpenTelemetry
// convention: Input is the complete input count, including cache reads and
// writes. CacheRead and CacheWrite are breakdowns of that input count, while
// Reasoning is a breakdown of Output. This package only applies a versioned
// rate card to those canonical counters.
package billing

import (
	"fmt"
	"math"

	"github.com/theoden9014/agentmetry/internal/canonical"
)

const tokensPerMillion = int64(1_000_000)

// Pricing stores rates in micro-USD per one million tokens. Keeping the rate
// card in integer units makes the result deterministic and avoids float64
// rounding differences in tests and persisted calculations.
type Pricing struct {
	InputMicroUSDPerMillion      int64
	CacheReadMicroUSDPerMillion  int64
	CacheWriteMicroUSDPerMillion int64
	OutputMicroUSDPerMillion     int64
}

// CostBreakdown is the auditable result of applying a rate card to usage.
// TotalMicroUSD is the sum of the four component costs.
type CostBreakdown struct {
	InputTokens         int64
	UncachedInputTokens int64
	CacheReadTokens     int64
	CacheWriteTokens    int64
	OutputTokens        int64

	InputMicroUSD      int64
	CacheReadMicroUSD  int64
	CacheWriteMicroUSD int64
	OutputMicroUSD     int64
	TotalMicroUSD      int64
}

// Calculate applies pricing to canonical token usage.
//
// Input is split into uncached input, cache reads, and cache writes. Reasoning
// is deliberately not charged separately because provider usage reports it as
// a breakdown of output_tokens, not an additional token stream.
func Calculate(usage canonical.TokenUsage, pricing Pricing) (CostBreakdown, error) {
	if err := validate(usage, pricing); err != nil {
		return CostBreakdown{}, err
	}

	uncachedInput := usage.Input - usage.CacheRead - usage.CacheWrite
	inputCost, err := charge(uncachedInput, pricing.InputMicroUSDPerMillion)
	if err != nil {
		return CostBreakdown{}, fmt.Errorf("uncached input: %w", err)
	}
	cacheReadCost, err := charge(usage.CacheRead, pricing.CacheReadMicroUSDPerMillion)
	if err != nil {
		return CostBreakdown{}, fmt.Errorf("cache read: %w", err)
	}
	cacheWriteCost, err := charge(usage.CacheWrite, pricing.CacheWriteMicroUSDPerMillion)
	if err != nil {
		return CostBreakdown{}, fmt.Errorf("cache write: %w", err)
	}
	outputCost, err := charge(usage.Output, pricing.OutputMicroUSDPerMillion)
	if err != nil {
		return CostBreakdown{}, fmt.Errorf("output: %w", err)
	}

	return CostBreakdown{
		InputTokens:         usage.Input,
		UncachedInputTokens: uncachedInput,
		CacheReadTokens:     usage.CacheRead,
		CacheWriteTokens:    usage.CacheWrite,
		OutputTokens:        usage.Output,
		InputMicroUSD:       inputCost,
		CacheReadMicroUSD:   cacheReadCost,
		CacheWriteMicroUSD:  cacheWriteCost,
		OutputMicroUSD:      outputCost,
		TotalMicroUSD:       inputCost + cacheReadCost + cacheWriteCost + outputCost,
	}, nil
}

func validate(usage canonical.TokenUsage, pricing Pricing) error {
	for name, value := range map[string]int64{
		"input":       usage.Input,
		"output":      usage.Output,
		"cache read":  usage.CacheRead,
		"cache write": usage.CacheWrite,
		"reasoning":   usage.Reasoning,
	} {
		if value < 0 {
			return fmt.Errorf("%s tokens must be non-negative: %d", name, value)
		}
	}
	for name, value := range map[string]int64{
		"input":       pricing.InputMicroUSDPerMillion,
		"cache read":  pricing.CacheReadMicroUSDPerMillion,
		"cache write": pricing.CacheWriteMicroUSDPerMillion,
		"output":      pricing.OutputMicroUSDPerMillion,
	} {
		if value < 0 {
			return fmt.Errorf("%s price must be non-negative: %d", name, value)
		}
	}
	if usage.CacheRead+usage.CacheWrite < usage.CacheRead || usage.CacheRead+usage.CacheWrite > usage.Input {
		return fmt.Errorf("cache tokens exceed input tokens: input=%d cacheRead=%d cacheWrite=%d", usage.Input, usage.CacheRead, usage.CacheWrite)
	}
	return nil
}

func charge(tokens, microUSDPerMillion int64) (int64, error) {
	if tokens == 0 || microUSDPerMillion == 0 {
		return 0, nil
	}
	if tokens > (math.MaxInt64-tokensPerMillion/2)/microUSDPerMillion {
		return 0, fmt.Errorf("token-rate product overflows int64")
	}
	return (tokens*microUSDPerMillion + tokensPerMillion/2) / tokensPerMillion, nil
}
