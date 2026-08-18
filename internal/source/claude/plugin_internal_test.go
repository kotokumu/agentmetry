package claude

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	source "github.com/kotokumu/agentmetry/sourceplugin"
)

func TestPlugin_Normalize(t *testing.T) {
	type args struct {
		input source.Event
	}
	tests := []struct {
		name string
		p    Plugin
		args args
		want source.Event
	}{
		{
			name: "agent name remains descriptive when API request has no runtime ID",
			p:    Plugin{},
			args: args{input: source.Event{
				Name: "claude_code.api_request",
				Attributes: map[string]any{
					"service.name":      "claude-code",
					"session.id":        "session-1",
					"agent.name":        "general-purpose",
					"client_request_id": "request-1",
					"input_tokens":      int64(8),
					"output_tokens":     int64(2),
				},
			}},
			want: source.Event{
				Name: "gen_ai.model.request",
				Attributes: map[string]any{
					"service.name":               "claude-code",
					"session.id":                 "session-1",
					"agent.name":                 "general-purpose",
					"client_request_id":          "request-1",
					"input_tokens":               int64(8),
					"output_tokens":              int64(2),
					"gen_ai.conversation.id":     "session-1",
					"gen_ai.agent.definition":    "general-purpose",
					"gen_ai.agent.type":          "general-purpose",
					"gen_ai.client.request.id":   "request-1",
					"gen_ai.usage.role":          "authoritative_call",
					"gen_ai.usage.input_tokens":  int64(8),
					"gen_ai.usage.output_tokens": int64(2),
					"gen_ai.usage.id":            "request-1",
				},
			},
		},
		{
			name: "explicit runtime ID remains separate from agent name",
			p:    Plugin{},
			args: args{input: source.Event{
				Name: "claude_code.llm_request",
				Attributes: map[string]any{
					"service.name":      "claude-code",
					"session.id":        "session-1",
					"agent.name":        "general-purpose",
					"agent_id":          "a548a8af0601337f2",
					"parent_agent_id":   "main",
					"client_request_id": "request-1",
				},
			}},
			want: source.Event{
				Name: "gen_ai.model.request.trace",
				Attributes: map[string]any{
					"service.name":             "claude-code",
					"session.id":               "session-1",
					"agent.name":               "general-purpose",
					"agent_id":                 "a548a8af0601337f2",
					"parent_agent_id":          "main",
					"client_request_id":        "request-1",
					"gen_ai.conversation.id":   "session-1",
					"gen_ai.agent.id":          "a548a8af0601337f2",
					"gen_ai.agent.definition":  "general-purpose",
					"gen_ai.agent.parent.id":   "main",
					"gen_ai.agent.type":        "general-purpose",
					"gen_ai.client.request.id": "request-1",
					"gen_ai.usage.role":        "corroborating",
					"gen_ai.usage.id":          "request-1",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Plugin{}
			if got := p.Normalize(tt.args.input); !cmp.Equal(tt.want, got) {
				t.Errorf("Plugin.Normalize() = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got))
			}
		})
	}
}
