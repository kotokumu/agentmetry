package claude

import (
	"strconv"
	"strings"

	source "github.com/kotokumu/agentmetry/sourceplugin"
)

type Plugin struct{}

func New() Plugin { return Plugin{} }

func (Plugin) ID() string { return "claude" }

func (Plugin) DisplayName() string { return "Claude Code" }

func (Plugin) NormalizeAgentMetadata(metadata source.AgentMetadata) source.AgentMetadata {
	if metadata.Definition == "" {
		metadata.Definition = definitionFromRuntimeAgentID(metadata.ID)
	}
	metadata.Type = strings.TrimPrefix(metadata.Type, "agent:")
	return metadata
}

func (Plugin) Match(event source.Event) bool {
	name := strings.ToLower(event.Name + " " + text(event.Attributes["event.name"]) + " " + text(event.Attributes["service.name"]))
	return strings.Contains(name, "claude")
}

func (Plugin) Normalize(input source.Event) source.Event {
	event := source.CloneEvent(input)
	eventName := text(event.Attributes["event.name"])
	sourceName := sourceEventName(event.Name, eventName)
	event.Name = canonicalEventName(sourceName)

	copyAlias(event.Attributes, "gen_ai.conversation.id", "session.id")
	copyAlias(event.Attributes, "gen_ai.agent.id", "agent_id")
	copyFirstAlias(event.Attributes, "gen_ai.agent.definition", "agent_definition", "agent.name", "subagent_type")
	copyAlias(event.Attributes, "gen_ai.agent.parent.id", "parent_agent_id")
	copyAlias(event.Attributes, "gen_ai.request.model", "model")
	copyAlias(event.Attributes, "gen_ai.tool.call.id", "tool_use_id")
	copyAlias(event.Attributes, "gen_ai.tool.name", "tool_name")
	copyAlias(event.Attributes, "gen_ai.agent.target.id", "target_agent_id")
	copyAlias(event.Attributes, "gen_ai.agent.target.type", "target_agent_type")
	copyAlias(event.Attributes, "gen_ai.turn.id", "prompt.id")
	copyAlias(event.Attributes, "gen_ai.client.request.id", "client_request_id")
	copyAlias(event.Attributes, "gen_ai.request.id", "request_id")
	copyFirstAlias(event.Attributes, "gen_ai.agent.type", "agent.name", "subagent_type")
	if _, exists := event.Attributes["gen_ai.agent.definition"]; !exists {
		if definition := definitionFromRuntimeAgentID(text(event.Attributes["gen_ai.agent.id"])); definition != "" {
			event.Attributes["gen_ai.agent.definition"] = definition
		}
	}
	copyFirstAlias(event.Attributes, "content", "prompt", "response", "tool_input", "tool_parameters", "full_command", "file_path", "error", "body", "body_ref")

	// API request events are the request-level usage source. The same counters
	// also occur on llm_request spans and aggregate metrics, so projecting those
	// again would inflate dashboard totals. Their original attributes remain in
	// the lossless OTLP journal and observation payloads.
	if sourceName == "api_request" {
		event.Attributes["gen_ai.usage.role"] = "authoritative_call"
		normalizeInputTokens(event.Attributes)
		copyAlias(event.Attributes, "gen_ai.usage.output_tokens", "output_tokens")
		copyFirstAlias(event.Attributes, "gen_ai.usage.cache_read.input_tokens", "cache_read_tokens", "cache_read_input_tokens")
		copyFirstAlias(event.Attributes, "gen_ai.usage.cache_write.input_tokens", "cache_creation_tokens", "cache_creation_input_tokens")
		copyFirstAlias(event.Attributes, "gen_ai.usage.id", "client_request_id", "request_id")
	} else if sourceName == "llm_request" {
		event.Attributes["gen_ai.usage.role"] = "corroborating"
		copyFirstAlias(event.Attributes, "gen_ai.usage.id", "client_request_id", "request_id")
	}
	if _, exists := event.Attributes["gen_ai.agent.type"]; !exists {
		switch querySource := text(event.Attributes["query_source"]); querySource {
		case "main", "repl_main_thread":
			event.Attributes["gen_ai.agent.type"] = "root"
		case "compact", "auxiliary":
			event.Attributes["gen_ai.agent.type"] = "auxiliary"
		case "subagent":
			event.Attributes["gen_ai.agent.type"] = "subagent"
		case "":
		default:
			// Detailed log and span values identify the subagent type/name.
			event.Attributes["gen_ai.agent.type"] = strings.TrimPrefix(querySource, "agent:")
		}
	}
	if micros, ok := number(event.Attributes["cost_usd_micros"]); ok && micros >= 0 {
		cost := micros / 1_000_000
		event.Attributes["gen_ai.usage.cost_usd"] = cost
		if _, exists := event.Attributes["cost_usd"]; !exists {
			event.Attributes["cost_usd"] = cost
		}
	}
	return event
}

func definitionFromRuntimeAgentID(agentID string) string {
	definition, _, found := strings.Cut(agentID, "@session-")
	if !found || definition == "" {
		return ""
	}
	return definition
}

func sourceEventName(recordName, attributeName string) string {
	if attributeName != "" {
		return strings.TrimPrefix(attributeName, "claude_code.")
	}
	return strings.TrimPrefix(recordName, "claude_code.")
}

func canonicalEventName(name string) string {
	switch name {
	case "assistant_response":
		return "gen_ai.response.completed"
	case "api_request":
		return "gen_ai.model.request"
	case "llm_request":
		return "gen_ai.model.request.trace"
	case "api_error":
		return "gen_ai.model.error"
	default:
		if name == "" {
			return "gen_ai.telemetry.event"
		}
		return "gen_ai." + name
	}
}

func copyFirstAlias(attributes map[string]any, destination string, sourceKeys ...string) {
	if _, exists := attributes[destination]; exists {
		return
	}
	for _, sourceKey := range sourceKeys {
		if value, exists := attributes[sourceKey]; exists {
			if text, ok := value.(string); !ok || text != "" {
				attributes[destination] = value
				return
			}
		}
	}
}

func copyAlias(attributes map[string]any, destination, sourceKey string) {
	if _, exists := attributes[destination]; exists {
		return
	}
	if value, exists := attributes[sourceKey]; exists {
		attributes[destination] = value
	}
}

// normalizeInputTokens adapts Claude Code's raw request fields to the
// OpenTelemetry convention. Claude reports uncached input_tokens separately
// from cache_read_tokens and cache_creation_tokens, while gen_ai.usage.input_tokens
// represents the complete input count. Keep an already normalized value intact
// so repeated normalization cannot double-count cache tokens.
func normalizeInputTokens(attributes map[string]any) {
	if _, exists := attributes["gen_ai.usage.input_tokens"]; exists {
		return
	}
	input, ok := nonNegativeTokenCount(attributes["input_tokens"])
	if !ok {
		return
	}
	cacheRead, cacheReadOK := firstNonNegativeTokenCount(attributes, "cache_read_tokens", "cache_read_input_tokens")
	cacheWrite, cacheWriteOK := firstNonNegativeTokenCount(attributes, "cache_creation_tokens", "cache_creation_input_tokens")
	if !cacheReadOK {
		cacheRead = 0
	}
	if !cacheWriteOK {
		cacheWrite = 0
	}
	if input > int64(^uint64(0)>>1)-cacheRead || input+cacheRead > int64(^uint64(0)>>1)-cacheWrite {
		return
	}
	attributes["gen_ai.usage.input_tokens"] = input + cacheRead + cacheWrite
}

func firstNonNegativeTokenCount(attributes map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		if value, ok := nonNegativeTokenCount(attributes[key]); ok {
			return value, true
		}
	}
	return 0, false
}

func nonNegativeTokenCount(value any) (int64, bool) {
	var parsed int64
	switch value := value.(type) {
	case int:
		parsed = int64(value)
	case int32:
		parsed = int64(value)
	case int64:
		parsed = value
	case float64:
		text := strconv.FormatFloat(value, 'f', -1, 64)
		var err error
		parsed, err = strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, false
		}
	case string:
		var err error
		parsed, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return parsed, parsed >= 0
}

func text(value any) string {
	text, _ := value.(string)
	return text
}

func number(value any) (float64, bool) {
	switch value := value.(type) {
	case int:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case float32:
		return float64(value), true
	case float64:
		return value, true
	case string:
		parsed, err := strconv.ParseFloat(value, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
