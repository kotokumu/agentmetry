package canonical

import (
	"encoding/json"
	"strconv"
)

var (
	agentKeys           = []string{"gen_ai.agent.id", "agent.id"}
	agentDefinitionKeys = []string{"gen_ai.agent.definition"}
	agentTypeKeys       = []string{"gen_ai.agent.type"}
	parentAgentKeys     = []string{"gen_ai.agent.parent.id"}
	runKeys             = []string{"gen_ai.conversation.id"}
	modelKeys           = []string{"gen_ai.request.model", "gen_ai.response.model"}
)

func DeriveAgentContext(attributes map[string]any) AgentContext {
	input, inputReported := firstNonNegativeInteger(attributes, "gen_ai.usage.input_tokens")
	output, outputReported := firstNonNegativeInteger(attributes, "gen_ai.usage.output_tokens")
	cacheRead, cacheReadReported := firstNonNegativeInteger(attributes,
		"gen_ai.usage.cache_read.input_tokens", "gen_ai.usage.cache_read_tokens")
	cacheWrite, cacheWriteReported := firstNonNegativeInteger(attributes,
		"gen_ai.usage.cache_write.input_tokens", "gen_ai.usage.cache_write_tokens")
	reasoning, reasoningReported := firstNonNegativeInteger(attributes, "gen_ai.usage.reasoning_tokens")
	return AgentContext{
		AgentID:         firstString(attributes, agentKeys...),
		AgentDefinition: firstString(attributes, agentDefinitionKeys...),
		AgentType:       firstString(attributes, agentTypeKeys...),
		ParentAgentID:   firstString(attributes, parentAgentKeys...),
		RunID:           firstString(attributes, runKeys...),
		Model:           firstString(attributes, modelKeys...),
		Tokens: TokenUsage{
			Input: input, Output: output, CacheRead: cacheRead, CacheWrite: cacheWrite, Reasoning: reasoning,
			Presence: TokenPresence{
				Input: inputReported && input == 0, Output: outputReported && output == 0,
				CacheRead: cacheReadReported && cacheRead == 0, CacheWrite: cacheWriteReported && cacheWrite == 0,
				Reasoning: reasoningReported && reasoning == 0,
			},
		},
	}
}

func DeriveCostUSD(attributes map[string]any) *float64 {
	if role, _ := attributes["gen_ai.usage.role"].(string); role == "corroborating" {
		return nil
	}
	for _, key := range []string{"gen_ai.usage.cost", "gen_ai.usage.cost_usd", "cost_usd", "estimated_cost_usd"} {
		value, ok := number(attributes[key])
		if ok && value >= 0 {
			return &value
		}
	}
	return nil
}

func firstString(attributes map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := attributes[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func firstNonNegativeInteger(attributes map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		if value, ok := integer(attributes[key]); ok && value >= 0 {
			return value, true
		}
	}
	return 0, false
}

func integer(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		if typed != float64(int64(typed)) {
			return 0, false
		}
		return int64(typed), true
	case json.Number:
		value, err := typed.Int64()
		return value, err == nil
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
