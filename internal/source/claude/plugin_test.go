package claude_test

import (
	"testing"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/source/claude"
	source "github.com/theoden9014/agentmetry/sourceplugin"
)

func TestPluginProfilesClaudeCodeRequestUsage(t *testing.T) {
	plugin := claude.New()
	event := source.Event{
		Name: "claude_code.api_request",
		Attributes: map[string]any{
			"service.name":          "claude-code",
			"session.id":            "session-1",
			"prompt.id":             "prompt-1",
			"agent_id":              "agent-1",
			"parent_agent_id":       "parent-1",
			"model":                 "claude-example",
			"input_tokens":          int64(100),
			"output_tokens":         int64(20),
			"cache_read_tokens":     int64(70),
			"cache_creation_tokens": int64(4),
			"cost_usd_micros":       int64(12500),
			"query_source":          "subagent",
		},
	}

	if !plugin.Match(event) {
		t.Fatal("Claude Code event was not matched")
	}
	normalized := plugin.Normalize(event)
	if normalized.Name != "gen_ai.model.request" {
		t.Fatalf("event name = %q", normalized.Name)
	}

	if normalized.Attributes["gen_ai.conversation.id"] != "session-1" || normalized.Attributes["gen_ai.agent.id"] != "agent-1" {
		t.Fatalf("identity aliases were not normalized: %#v", normalized.Attributes)
	}
	if normalized.Attributes["gen_ai.agent.parent.id"] != "parent-1" || normalized.Attributes["gen_ai.agent.type"] != "subagent" {
		t.Fatalf("agent aliases were not normalized: %#v", normalized.Attributes)
	}
	if normalized.Attributes["gen_ai.usage.cache_write.input_tokens"] != int64(4) {
		t.Fatalf("cache creation was not normalized: %#v", normalized.Attributes)
	}
	if normalized.Attributes["cost_usd"] != 0.0125 {
		t.Fatalf("cost micros were not normalized: %#v", normalized.Attributes)
	}
	context := canonical.DeriveAgentContext(normalized.Attributes)
	if context.RunID != "session-1" || context.AgentID != "agent-1" || context.ParentAgentID != "parent-1" {
		t.Fatalf("canonical identity was not derived: %#v", context)
	}
	if context.Tokens.Input != 100 || context.Tokens.Output != 20 || context.Tokens.CacheRead != 70 || context.Tokens.CacheWrite != 4 {
		t.Fatalf("canonical usage was not derived: %#v", context.Tokens)
	}
}

func TestPluginUsesClaudeEventNameAttribute(t *testing.T) {
	plugin := claude.New()
	event := source.Event{
		Name: "",
		Attributes: map[string]any{
			"service.name": "claude-code",
			"event.name":   "assistant_response",
		},
	}

	normalized := plugin.Normalize(event)

	if normalized.Name != "gen_ai.response.completed" {
		t.Fatalf("event name = %q", normalized.Name)
	}
}

func TestPluginProfilesClaudeTraceAgentAndToolOperations(t *testing.T) {
	plugin := claude.New()

	request := plugin.Normalize(source.Event{
		Name: "claude_code.llm_request",
		Attributes: map[string]any{
			"service.name":         "claude-code-desktop",
			"session.id":           "session-1",
			"agent_id":             "agent-7",
			"parent_agent_id":      "agent-1",
			"agent.name":           "Explore",
			"gen_ai.request.model": "claude-example",
			"client_request_id":    "client-request-1",
			"input_tokens":         int64(100),
			"output_tokens":        int64(20),
		},
	})
	if !plugin.Match(request) {
		t.Fatal("Claude Desktop event was not matched")
	}
	if request.Name != "gen_ai.model.request.trace" {
		t.Fatalf("trace event name = %q", request.Name)
	}
	if request.Attributes["gen_ai.agent.id"] != "agent-7" || request.Attributes["gen_ai.agent.parent.id"] != "agent-1" {
		t.Fatalf("trace agent identity was not normalized: %#v", request.Attributes)
	}
	if request.Attributes["gen_ai.agent.type"] != "Explore" || request.Attributes["gen_ai.client.request.id"] != "client-request-1" {
		t.Fatalf("trace semantics were not normalized: %#v", request.Attributes)
	}
	if _, exists := request.Attributes["gen_ai.usage.input_tokens"]; exists {
		t.Fatalf("trace token usage must not duplicate the authoritative api_request event: %#v", request.Attributes)
	}

	tool := plugin.Normalize(source.Event{
		Name: "claude_code.tool",
		Attributes: map[string]any{
			"service.name":  "claude-code",
			"session.id":    "session-1",
			"agent_id":      "agent-7",
			"tool_name":     "Read",
			"tool_use_id":   "tool-1",
			"full_command":  "sed -n '1,20p' main.go",
			"subagent_type": "Explore",
		},
	})
	if tool.Name != "gen_ai.tool" || tool.Attributes["gen_ai.tool.name"] != "Read" || tool.Attributes["gen_ai.tool.call.id"] != "tool-1" {
		t.Fatalf("tool semantics were not normalized: %#v", tool)
	}
	if tool.Attributes["content"] != "sed -n '1,20p' main.go" {
		t.Fatalf("tool content was not normalized: %#v", tool.Attributes)
	}
}

func TestPluginExtractsClaudeAgentDefinitionFromRuntimeAgentID(t *testing.T) {
	event := claude.New().Normalize(source.Event{
		Name: "claude_code.llm_request",
		Attributes: map[string]any{
			"service.name":      "claude-code",
			"agent_id":          "repo-overview@session-2863a235",
			"client_request_id": "request-1",
			"model":             "claude-opus-5[1m]",
		},
	})

	if event.Attributes["gen_ai.agent.definition"] != "repo-overview" {
		t.Fatalf("agent definition was not extracted: %#v", event.Attributes)
	}
	context := canonical.DeriveAgentContext(event.Attributes)
	if context.AgentDefinition != "repo-overview" {
		t.Fatalf("canonical definition was not derived: %#v", context)
	}
}

func TestPluginProfilesClaudePromptResponseAndToolResultContent(t *testing.T) {
	plugin := claude.New()
	tests := []struct {
		name        string
		event       source.Event
		wantName    string
		wantContent string
	}{
		{
			name: "prompt",
			event: source.Event{Name: "event", Attributes: map[string]any{
				"service.name": "claude-code", "event.name": "user_prompt", "prompt": "Inspect the repository",
			}},
			wantName: "gen_ai.user_prompt", wantContent: "Inspect the repository",
		},
		{
			name: "response",
			event: source.Event{Name: "claude_code.assistant_response", Attributes: map[string]any{
				"service.name": "claude-code", "response": "The repository is healthy", "request_id": "request-1",
			}},
			wantName: "gen_ai.response.completed", wantContent: "The repository is healthy",
		},
		{
			name: "tool result",
			event: source.Event{Name: "claude_code.tool_result", Attributes: map[string]any{
				"service.name": "claude-code", "tool_name": "Read", "tool_input": `{"file_path":"main.go"}`,
			}},
			wantName: "gen_ai.tool_result", wantContent: `{"file_path":"main.go"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized := plugin.Normalize(test.event)
			if normalized.Name != test.wantName || normalized.Attributes["content"] != test.wantContent {
				t.Fatalf("normalized event = %#v", normalized)
			}
		})
	}
}
