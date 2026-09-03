package query

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/kotokumu/agentmetry/internal/canonical"
)

func TestDescribeActivityContent(t *testing.T) {
	type args struct {
		activity Activity
	}
	tests := []struct {
		name string
		args args
		want ContentEvidence
	}{
		{
			name: "legacy body keeps unknown provenance",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.user_prompt", Content: "Hello", Attributes: map[string]any{}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "unknown", Evidence: "unknown", Availability: "available", Fields: nil, Truncated: false, RedactionReason: ""},
		},
		{
			name: "unknown provider does not borrow Claude aliases",
			args: args{activity: Activity{ID: "activity-1", Source: "other", Signal: canonical.SignalLog, Name: "user_prompt", Content: "Hello", Attributes: map[string]any{"prompt": "Hello"}}},
			want: ContentEvidence{Source: "other", ActivityID: "activity-1", Signal: "log", Kind: "unknown", Evidence: "unknown", Availability: "available", Fields: nil, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Claude prompt carries received field provenance",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.user_prompt", Content: "Hello", Attributes: map[string]any{"prompt": "Hello", "content": "Hello"}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "prompt", Evidence: "unknown", Availability: "available", Fields: []string{"prompt"}, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Claude response uses its own body field",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.response.completed", Content: "Answer", Attributes: map[string]any{"response": "Answer", "content": "Answer"}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "response", Evidence: "unknown", Availability: "available", Fields: []string{"response"}, Truncated: false, RedactionReason: ""},
		},
		{
			name: "absent Claude response is unreported without a cause",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.response.completed", Content: "", Attributes: map[string]any{}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "response", Evidence: "unknown", Availability: "not_reported", Fields: nil, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Claude tool input is not confirmed model input",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.tool_result", Content: "{\"file_path\":\"AGENTS.md\"}", Attributes: map[string]any{"tool_input": "{\"file_path\":\"AGENTS.md\"}"}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "tool_input", Evidence: "unknown", Availability: "available", Fields: []string{"tool_input"}, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Claude command remains tool input",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.tool", Content: "cat AGENTS.md", Attributes: map[string]any{"full_command": "cat AGENTS.md"}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "tool_input", Evidence: "unknown", Availability: "available", Fields: []string{"full_command"}, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Claude file reference does not imply body or model inclusion",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.tool", Content: "AGENTS.md", Attributes: map[string]any{"file_path": "AGENTS.md"}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "reference", Evidence: "reference", Availability: "not_reported", Fields: []string{"file_path"}, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Claude body reference is not readable model input",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.api_request_body", Content: "file:///private/request.json", Attributes: map[string]any{"body_ref": "file:///private/request.json"}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "reference", Evidence: "reference", Availability: "not_reported", Fields: []string{"body_ref"}, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Claude inline API request explicitly reports model input",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.api_request_body", Content: "{\"messages\":[]}", Attributes: map[string]any{"body": "{\"messages\":[]}", "body_truncated": true}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "model_input", Evidence: "explicit_model_input", Availability: "available", Fields: []string{"body"}, Truncated: true, RedactionReason: ""},
		},
		{
			name: "Claude API response is not model input",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.api_response_body", Content: "{\"output\":\"ok\"}", Attributes: map[string]any{"body": "{\"output\":\"ok\"}", "body_truncated": "true"}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "response", Evidence: "unknown", Availability: "available", Fields: []string{"body"}, Truncated: true, RedactionReason: ""},
		},
		{
			name: "unverified Claude context attributes do not create content",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.model.request.trace", Content: "", Attributes: map[string]any{"llm_request.context": "private context"}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "unknown", Evidence: "unknown", Availability: "not_reported", Fields: nil, Truncated: false, RedactionReason: ""},
		},
		{
			name: "unsupported interaction user_prompt is not projected body",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.interaction", Content: "", Attributes: map[string]any{"user_prompt": "private prompt"}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "unknown", Evidence: "unknown", Availability: "not_reported", Fields: nil, Truncated: false, RedactionReason: ""},
		},
		{
			name: "unrelated content does not inherit prompt field meaning",
			args: args{activity: Activity{ID: "activity-1", Source: "claude", Signal: canonical.SignalLog, Name: "gen_ai.user_prompt", Content: "generic body", Attributes: map[string]any{"prompt": "different prompt", "content": "generic body"}}},
			want: ContentEvidence{Source: "claude", ActivityID: "activity-1", Signal: "log", Kind: "unknown", Evidence: "unknown", Availability: "available", Fields: nil, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Codex prompt uses received prompt field",
			args: args{activity: Activity{ID: "activity-1", Source: "codex", Signal: canonical.SignalLog, Name: "gen_ai.user_prompt", Content: "Hello", Attributes: map[string]any{"prompt": "Hello"}}},
			want: ContentEvidence{Source: "codex", ActivityID: "activity-1", Signal: "log", Kind: "prompt", Evidence: "unknown", Availability: "available", Fields: []string{"prompt"}, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Codex explicit redaction marker is unavailable content",
			args: args{activity: Activity{ID: "activity-1", Source: "codex", Signal: canonical.SignalLog, Name: "gen_ai.user_prompt", Content: "[REDACTED]", Attributes: map[string]any{"prompt": "[REDACTED]"}}},
			want: ContentEvidence{Source: "codex", ActivityID: "activity-1", Signal: "log", Kind: "prompt", Evidence: "unknown", Availability: "redacted", Fields: []string{"prompt"}, Truncated: false, RedactionReason: "producer_redacted"},
		},
		{
			name: "Codex absent prompt is unreported",
			args: args{activity: Activity{ID: "activity-1", Source: "codex", Signal: canonical.SignalLog, Name: "gen_ai.user_prompt", Content: "", Attributes: map[string]any{}}},
			want: ContentEvidence{Source: "codex", ActivityID: "activity-1", Signal: "log", Kind: "prompt", Evidence: "unknown", Availability: "not_reported", Fields: nil, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Codex tool output does not prove model inclusion",
			args: args{activity: Activity{ID: "activity-1", Source: "codex", Signal: canonical.SignalLog, Name: "gen_ai.tool_result", Content: "Result: received file text", Attributes: map[string]any{"tool_name": "exec_command", "output": "received file text"}}},
			want: ContentEvidence{Source: "codex", ActivityID: "activity-1", Signal: "log", Kind: "tool_output", Evidence: "read_output", Availability: "available", Fields: []string{"output"}, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Codex input and output retain their mixed meaning",
			args: args{activity: Activity{ID: "activity-1", Source: "codex", Signal: canonical.SignalLog, Name: "gen_ai.tool_result", Content: "instruction\nResult: done", Attributes: map[string]any{"tool_name": "send_message", "arguments": "{\"message\":\"instruction\"}", "output": "done"}}},
			want: ContentEvidence{Source: "codex", ActivityID: "activity-1", Signal: "log", Kind: "tool_input_output", Evidence: "unknown", Availability: "available", Fields: []string{"arguments.message", "output"}, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Codex instruction alone is not confirmed model input",
			args: args{activity: Activity{ID: "activity-1", Source: "codex", Signal: canonical.SignalLog, Name: "gen_ai.tool_result", Content: "instruction", Attributes: map[string]any{"tool_name": "send_message", "arguments": "{\"message\":\"instruction\"}"}}},
			want: ContentEvidence{Source: "codex", ActivityID: "activity-1", Signal: "log", Kind: "tool_input", Evidence: "unknown", Availability: "available", Fields: []string{"arguments.message"}, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Codex encrypted instruction has no readable body",
			args: args{activity: Activity{ID: "activity-1", Source: "codex", Signal: canonical.SignalLog, Name: "gen_ai.tool_result", Content: "Instruction content encrypted by source telemetry", Attributes: map[string]any{"tool_name": "send_message", "arguments": "{\"message\":\"gAAAAprivate-ciphertext\"}"}}},
			want: ContentEvidence{Source: "codex", ActivityID: "activity-1", Signal: "log", Kind: "tool_input", Evidence: "unknown", Availability: "redacted", Fields: []string{"arguments.message"}, Truncated: false, RedactionReason: "encrypted_input"},
		},
		{
			name: "Codex encrypted input preserves readable tool output",
			args: args{activity: Activity{ID: "activity-1", Source: "codex", Signal: canonical.SignalLog, Name: "gen_ai.tool_result", Content: "Instruction content encrypted by source telemetry\nResult: done", Attributes: map[string]any{"tool_name": "send_message", "arguments": "{\"message\":\"gAAAAprivate-ciphertext\"}", "output": "done"}}},
			want: ContentEvidence{Source: "codex", ActivityID: "activity-1", Signal: "log", Kind: "tool_output", Evidence: "read_output", Availability: "available", Fields: []string{"arguments.message", "output"}, Truncated: false, RedactionReason: "encrypted_input"},
		},
		{
			name: "older ciphertext projection is not exposed as a body",
			args: args{activity: Activity{ID: "activity-1", Source: "codex", Signal: canonical.SignalLog, Name: "gen_ai.tool_result", Content: "gAAAAprivate-ciphertext", Attributes: map[string]any{"arguments": "{\"message\":\"gAAAAprivate-ciphertext\"}"}}},
			want: ContentEvidence{Source: "codex", ActivityID: "activity-1", Signal: "log", Kind: "tool_input", Evidence: "unknown", Availability: "redacted", Fields: []string{"arguments.message"}, Truncated: false, RedactionReason: "encrypted_input"},
		},
		{
			name: "malformed optional arguments do not fail generic content",
			args: args{activity: Activity{ID: "activity-1", Source: "codex", Signal: canonical.SignalLog, Name: "gen_ai.tool_result", Content: "native body", Attributes: map[string]any{"arguments": "{"}}},
			want: ContentEvidence{Source: "codex", ActivityID: "activity-1", Signal: "log", Kind: "unknown", Evidence: "unknown", Availability: "available", Fields: nil, Truncated: false, RedactionReason: ""},
		},
		{
			name: "Codex unprojected command arguments are not invented content",
			args: args{activity: Activity{ID: "activity-1", Source: "codex", Signal: canonical.SignalLog, Name: "gen_ai.tool_result", Content: "", Attributes: map[string]any{"arguments": "{\"cmd\":\"cat AGENTS.md\"}"}}},
			want: ContentEvidence{Source: "codex", ActivityID: "activity-1", Signal: "log", Kind: "unknown", Evidence: "unknown", Availability: "not_reported", Fields: nil, Truncated: false, RedactionReason: ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DescribeActivityContent(tt.args.activity); !cmp.Equal(tt.want, got) {
				t.Errorf("DescribeActivityContent() = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got))
			}
		})
	}
}

func TestContentForDelivery(t *testing.T) {
	type args struct {
		activity Activity
	}
	tests := []struct {
		name  string
		args  args
		want  string
		want1 ContentEvidence
	}{
		{
			name: "redaction evidence is not a readable body", args: args{activity: Activity{Source: "codex", Content: "[REDACTED]", Attributes: map[string]any{"prompt": "[REDACTED]"}}}, want: "", want1: ContentEvidence{Source: "codex", Kind: "prompt", Evidence: "unknown", Availability: "redacted", Fields: []string{"prompt"}, RedactionReason: "producer_redacted"},
		},
		{
			name: "reference text remains available without claiming a body", args: args{activity: Activity{Source: "claude", Content: "file:///request", Attributes: map[string]any{"body_ref": "file:///request"}}}, want: "file:///request", want1: ContentEvidence{Source: "claude", Kind: "reference", Evidence: "reference", Availability: "not_reported", Fields: []string{"body_ref"}},
		},
		{
			name: "readable output survives encrypted input", args: args{activity: Activity{Source: "codex", Content: "Instruction content encrypted by source telemetry\nResult: output", Attributes: map[string]any{"arguments": map[string]any{"message": "gAAAAcipher"}, "output": "output"}}}, want: "Instruction content encrypted by source telemetry\nResult: output", want1: ContentEvidence{Source: "codex", Kind: "tool_output", Evidence: "read_output", Availability: "available", Fields: []string{"arguments.message", "output"}, RedactionReason: "encrypted_input"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := ContentForDelivery(tt.args.activity)
			if got != tt.want {
				t.Errorf("ContentForDelivery() got = %v, want %v", got, tt.want)
			}
			if !cmp.Equal(tt.want1, got1) {
				t.Errorf("ContentForDelivery() got1 = %v, want %v\ndiff=%s", got1, tt.want1, cmp.Diff(tt.want1, got1))
			}
		})
	}
}
