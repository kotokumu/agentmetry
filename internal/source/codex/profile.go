package codex

import (
	"encoding/json"
	"fmt"
	"strings"

	source "github.com/kotokumu/agentmetry/sourceplugin"
)

type Plugin struct{}

func New() Plugin { return Plugin{} }

func (Plugin) ID() string { return "codex" }

func (Plugin) DisplayName() string { return "Codex" }

func (Plugin) Match(event source.Event) bool {
	combined := strings.ToLower(event.Name + " " + stringValue(event.Attributes["event.name"]) + " " + stringValue(event.Attributes["service.name"]))
	return strings.Contains(combined, "codex")
}

// Normalize interprets Codex event attributes and emits common semantic aliases.
func (Plugin) Normalize(input source.Event) source.Event {
	event := source.CloneEvent(input)
	name, normalized := event.Name, event.Attributes
	if eventName := stringValue(normalized["event.name"]); eventName != "" {
		name = eventName
	}

	if name == "codex.sse_event" && stringValue(normalized["event.kind"]) == "response.completed" {
		name = "gen_ai.response.completed"
	} else if name == "codex.agent_communication" {
		communicationKind := stringValue(normalized["kind"])
		switch communicationKind {
		case "spawn", "followup":
			name = "gen_ai.agent.delegation"
		default:
			name = "gen_ai.agent.message"
		}
		if communicationKind == "spawn" && stringValue(normalized["state"]) == "send" {
			copyAlias(normalized, "agentmetry.session.parent.id", "sender_thread_id")
			copyAlias(normalized, "agentmetry.session.child.id", "receiver_thread_id")
		}
	} else if strings.HasPrefix(name, "codex.") {
		name = "gen_ai." + strings.TrimPrefix(name, "codex.")
	}
	if stringValue(normalized["model"]) == "codex-auto-review" {
		normalized["gen_ai.agent.type"] = "system"
	}
	copyAlias(normalized, "gen_ai.conversation.id", "conversation.id")
	copyFirstAlias(normalized, "gen_ai.turn.id", "turn_id", "turn.id", "prompt_id", "prompt.id")
	copyAlias(normalized, "gen_ai.request.model", "model")
	copyFirstAlias(normalized, "gen_ai.usage.input_tokens", "input_token_count", "input_tokens")
	copyFirstAlias(normalized, "gen_ai.usage.output_tokens", "output_token_count", "output_tokens")
	copyFirstAlias(normalized, "gen_ai.usage.cache_read.input_tokens", "cached_token_count", "cached_input_tokens")
	copyFirstAlias(normalized, "gen_ai.usage.cache_write.input_tokens", "cache_write_token_count", "cache_write_tokens")
	copyFirstAlias(normalized, "gen_ai.usage.reasoning_tokens", "reasoning_token_count", "codex.usage.reasoning_output_tokens", "reasoning_output_tokens")
	copyAlias(normalized, "gen_ai.agent.id", "sender_thread_id")
	copyFirstAlias(normalized, "gen_ai.agent.definition", "agent_definition", "agent.name", "subagent_type")
	copyAlias(normalized, "gen_ai.agent.type", "agent_type")
	copyAlias(normalized, "gen_ai.agent.target.id", "receiver_thread_id")
	if hasUsage(normalized) {
		role := "corroborating"
		if name == "gen_ai.response.completed" {
			role = "authoritative_call"
		}
		normalized["gen_ai.usage.role"] = role
		if identity := usageIdentity(normalized); identity != "" {
			normalized["gen_ai.usage.id"] = identity
		}
	}

	toolName := normalizeToolName(stringValue(normalized["tool_name"]))
	if toolName == "" {
		return source.Event{Name: name, Attributes: normalized}
	}
	normalized["tool_name"] = toolName
	normalized["gen_ai.tool.name"] = toolName

	arguments := objectValue(normalized["arguments"])
	if value := stringValue(arguments["agent_type"]); value != "" {
		normalized["target_agent_type"] = value
		normalized["gen_ai.agent.target.type"] = value
	}
	if value := firstString(arguments, "target", "task_name"); value != "" {
		normalized["target_agent_id"] = value
		normalized["gen_ai.agent.target.id"] = value
	}

	output := stringValue(normalized["output"])
	if target := firstString(objectValue(output), "task_name", "target"); target != "" {
		normalized["target_agent_id"] = target
		normalized["gen_ai.agent.target.id"] = target
	}

	parts := make([]string, 0, 2)
	if message := stringValue(arguments["message"]); message != "" {
		if strings.HasPrefix(message, "gAAAA") {
			parts = append(parts, "Instruction content encrypted by source telemetry")
		} else {
			parts = append(parts, message)
		}
	}
	if output != "" {
		parts = append(parts, fmt.Sprintf("Result: %s", output))
	}
	if len(parts) > 0 {
		normalized["content"] = strings.Join(parts, "\n")
	}
	return source.Event{Name: name, Attributes: normalized}
}

func hasUsage(attributes map[string]any) bool {
	for _, key := range []string{
		"gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens",
		"gen_ai.usage.cache_read.input_tokens", "gen_ai.usage.cache_write.input_tokens",
		"gen_ai.usage.reasoning_tokens",
	} {
		if _, exists := attributes[key]; exists {
			return true
		}
	}
	return false
}

func usageIdentity(attributes map[string]any) string {
	if value := firstString(attributes, "gen_ai.client.request.id", "gen_ai.request.id", "request_id", "response.id"); value != "" {
		return value
	}
	conversation := stringValue(attributes["gen_ai.conversation.id"])
	timestamp := stringValue(attributes["event.timestamp"])
	if conversation == "" || timestamp == "" {
		return ""
	}
	return conversation + "|" + timestamp
}

func copyAlias(attributes map[string]any, destination, sourceKey string) {
	if _, exists := attributes[destination]; exists {
		return
	}
	if value, exists := attributes[sourceKey]; exists {
		attributes[destination] = value
	}
}

func copyFirstAlias(attributes map[string]any, destination string, sourceKeys ...string) {
	if _, exists := attributes[destination]; exists {
		return
	}
	for _, sourceKey := range sourceKeys {
		if value, exists := attributes[sourceKey]; exists {
			attributes[destination] = value
			return
		}
	}
}

func normalizeToolName(name string) string {
	for _, prefix := range []string{"collaboration.", "collaboration"} {
		if strings.HasPrefix(name, prefix) {
			return strings.TrimPrefix(name, prefix)
		}
	}
	return name
}

func objectValue(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return map[string]any{}
	}
	var object map[string]any
	if json.Unmarshal([]byte(text), &object) != nil {
		return map[string]any{}
	}
	return object
}

func firstString(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(object[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
