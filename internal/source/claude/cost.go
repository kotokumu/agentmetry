package claude

import (
	"encoding/json"
	"math"
	"math/big"
	"regexp"
	"strconv"
)

type ProviderCostState string

const (
	ProviderCostAbsent  ProviderCostState = "absent"
	ProviderCostValid   ProviderCostState = "valid"
	ProviderCostInvalid ProviderCostState = "invalid"
)

type ProviderCostEvidence struct {
	AmountMicroUSD *int64
	State          ProviderCostState
}

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// ResolveProviderCost interprets Claude's provider-owned cost fields before
// persistence. A present malformed micros field deliberately blocks decimal
// fallback, preserving the provider precedence contract.
func ResolveProviderCost(attributes map[string]any) ProviderCostEvidence {
	if raw, exists := attributes["cost_usd_micros"]; exists {
		value, ok := costInteger(raw)
		if !ok || value < 0 {
			return ProviderCostEvidence{State: ProviderCostInvalid}
		}
		return ProviderCostEvidence{AmountMicroUSD: &value, State: ProviderCostValid}
	}
	for _, key := range []string{"gen_ai.usage.cost_usd", "cost_usd", "estimated_cost_usd"} {
		raw, exists := attributes[key]
		if !exists {
			continue
		}
		value, ok := decimalMicroUSD(raw)
		if !ok {
			return ProviderCostEvidence{State: ProviderCostInvalid}
		}
		return ProviderCostEvidence{AmountMicroUSD: &value, State: ProviderCostValid}
	}
	return ProviderCostEvidence{State: ProviderCostAbsent}
}

func decimalMicroUSD(raw any) (int64, bool) {
	var valueText string
	switch value := raw.(type) {
	case string:
		valueText = value
	case json.Number:
		valueText = value.String()
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return 0, false
		}
		valueText = strconv.FormatFloat(value, 'g', -1, 64)
	case float32:
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < 0 {
			return 0, false
		}
		valueText = strconv.FormatFloat(float64(value), 'g', -1, 32)
	case int:
		valueText = strconv.Itoa(value)
	case int64:
		valueText = strconv.FormatInt(value, 10)
	default:
		return 0, false
	}
	if !decimalPattern.MatchString(valueText) {
		return 0, false
	}
	value, ok := new(big.Rat).SetString(valueText)
	if !ok || value.Sign() < 0 {
		return 0, false
	}
	value.Mul(value, big.NewRat(1_000_000, 1))
	numerator := new(big.Int).Set(value.Num())
	denominator := value.Denom()
	numerator.Add(numerator, new(big.Int).Quo(new(big.Int).Set(denominator), big.NewInt(2)))
	result := new(big.Int).Quo(numerator, denominator)
	if !result.IsInt64() {
		return 0, false
	}
	return result.Int64(), true
}

func costInteger(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case json.Number:
		result, err := value.Int64()
		return result, err == nil
	case float64:
		if value < math.MinInt64 || value > math.MaxInt64 || value != math.Trunc(value) {
			return 0, false
		}
		return int64(value), true
	default:
		return 0, false
	}
}
