package codex_test

import (
	"strings"
	"testing"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/source/codex"
	source "github.com/kotokumu/agentmetry/sourceplugin"
)

func TestNormalizeSpawnResultFromCodexOTLP(t *testing.T) {
	event := codex.New().Normalize(source.Event{Name: "event telemetry.rs:974", Attributes: map[string]any{
		"event.name": "codex.tool_result",
		"tool_name":  "collaborationspawn_agent",
		"arguments":  `{"task_name":"calculate_product","agent_type":"explorer","message":"gAAAA-secret"}`,
		"output":     `{"task_name":"/root/calculate_product"}`,
	}})

	if event.Name != "gen_ai.tool_result" || event.Attributes["tool_name"] != "spawn_agent" {
		t.Fatalf("unexpected event identity: %#v", event)
	}
	if event.Attributes["target_agent_type"] != "explorer" || event.Attributes["target_agent_id"] != "/root/calculate_product" {
		t.Fatalf("unexpected target projection: %#v", event.Attributes)
	}
	content, _ := event.Attributes["content"].(string)
	if !strings.Contains(content, "encrypted by source telemetry") || strings.Contains(content, "gAAAA-secret") {
		t.Fatalf("encrypted instruction was not represented safely: %q", content)
	}
}

func TestNormalizeCompletedResponseAndSystemSession(t *testing.T) {
	event := codex.New().Normalize(source.Event{Name: "codex.sse_event", Attributes: map[string]any{
		"event.kind": "response.completed",
		"model":      "codex-auto-review",
	}})
	if event.Name != "gen_ai.response.completed" || event.Attributes["gen_ai.agent.type"] != "system" {
		t.Fatalf("unexpected response projection: %#v", event)
	}
}

func TestNormalizeUsageAndAgentCommunicationAliases(t *testing.T) {
	event := codex.New().Normalize(source.Event{Name: "codex.agent_communication", Attributes: map[string]any{
		"conversation.id":                     "thread-1",
		"sender_thread_id":                    "parent-thread",
		"receiver_thread_id":                  "child-thread",
		"input_token_count":                   int64(100),
		"output_token_count":                  int64(20),
		"cached_token_count":                  int64(70),
		"cache_write_token_count":             int64(4),
		"codex.usage.reasoning_output_tokens": int64(12),
	}})

	if event.Attributes["gen_ai.conversation.id"] != "thread-1" || event.Attributes["gen_ai.agent.id"] != "parent-thread" {
		t.Fatalf("identity aliases were not normalized: %#v", event.Attributes)
	}
	if event.Attributes["gen_ai.agent.target.id"] != "child-thread" || event.Attributes["gen_ai.usage.reasoning_tokens"] != int64(12) {
		t.Fatalf("communication or usage aliases were not normalized: %#v", event.Attributes)
	}
	if event.Name != "gen_ai.agent.message" {
		t.Fatalf("agent communication name = %q", event.Name)
	}
	context := canonical.DeriveAgentContext(event.Attributes)
	if context.RunID != "thread-1" || context.AgentID != "parent-thread" {
		t.Fatalf("canonical identity was not derived: %#v", context)
	}
	if context.Tokens.Input != 100 || context.Tokens.Output != 20 || context.Tokens.CacheRead != 70 || context.Tokens.CacheWrite != 4 || context.Tokens.Reasoning != 12 {
		t.Fatalf("canonical usage was not derived: %#v", context.Tokens)
	}
}

func TestNormalizeSpawnSendDeclaresSessionLink(t *testing.T) {
	event := codex.New().Normalize(source.Event{Name: "codex.agent_communication", Attributes: map[string]any{
		"kind": "spawn", "state": "send", "sender_thread_id": "parent", "receiver_thread_id": "child",
	}})
	if event.Attributes["agentmetry.session.parent.id"] != "parent" || event.Attributes["agentmetry.session.child.id"] != "child" {
		t.Fatalf("session link aliases were not declared: %#v", event.Attributes)
	}
}

func TestNormalizeNativeResponseUsageAliases(t *testing.T) {
	event := codex.New().Normalize(source.Event{Name: "codex.sse_event", Attributes: map[string]any{
		"event.kind":              "response.completed",
		"conversation.id":         "thread-1",
		"input_tokens":            int64(32161),
		"cached_input_tokens":     int64(1920),
		"output_tokens":           int64(47),
		"reasoning_output_tokens": int64(41),
	}})

	context := canonical.DeriveAgentContext(event.Attributes)
	if context.Tokens.Input != 32161 || context.Tokens.Output != 47 || context.Tokens.CacheRead != 1920 || context.Tokens.Reasoning != 41 {
		t.Fatalf("native Codex usage was not normalized: %#v", context.Tokens)
	}
	if context.Tokens.Total() != 32208 || !context.Tokens.TotalReported() {
		t.Fatalf("native Codex total was not derived from input + output: %#v", context.Tokens)
	}
}

func TestNormalizeCorroboratingUsageRetainsAStableRequestIdentity(t *testing.T) {
	event := codex.New().Normalize(source.Event{Name: "codex.handle_responses", Attributes: map[string]any{
		"request_id":        "request-1",
		"input_token_count": int64(10),
	}})

	if event.Attributes["gen_ai.usage.role"] != "corroborating" || event.Attributes["gen_ai.usage.id"] != "request-1" {
		t.Fatalf("corroborating usage identity was not preserved: %#v", event.Attributes)
	}
}
