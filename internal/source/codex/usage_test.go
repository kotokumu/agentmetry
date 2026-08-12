package codex_test

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/theoden9014/agentmetry/internal/source/codex"
)

func TestParseExecJSONAggregatesCompletedTurns(t *testing.T) {
	got, err := codex.ParseExecJSON(strings.NewReader(`{"type":"turn.started"}
{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":70,"output_tokens":20,"reasoning_output_tokens":12}}
{"type":"turn.completed","usage":{"input_tokens":3,"cached_input_tokens":1,"output_tokens":2}}
`))
	if err != nil {
		t.Fatal(err)
	}
	want := codex.JSONUsage{InputTokens: 103, CachedInputTokens: 71, OutputTokens: 22, ReasoningOutputTokens: 12}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ParseExecJSON() mismatch (-want +got):\n%s", diff)
	}
}

func TestParseExecJSONRejectsMissingCompletedUsage(t *testing.T) {
	if _, err := codex.ParseExecJSON(strings.NewReader(`{"type":"turn.completed"}`)); err == nil {
		t.Fatal("ParseExecJSON() error = nil, want missing usage error")
	}
}

func TestParseExecJSONRejectsInconsistentProviderBreakdowns(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "cached input exceeds input",
			input: `{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":11,"output_tokens":2}}`,
		},
		{
			name:  "reasoning output exceeds output",
			input: `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2,"reasoning_output_tokens":3}}`,
		},
		{
			name:  "total does not match input and output",
			input: `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2,"total_tokens":11}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := codex.ParseExecJSON(strings.NewReader(tt.input)); err == nil {
				t.Fatal("ParseExecJSON() error = nil, want inconsistent usage error")
			}
		})
	}
}
