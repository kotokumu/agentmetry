package canonical

import "testing"

func TestIsSemanticSpanRequiresExplicitAgentEvidence(t *testing.T) {
	tests := []struct {
		name string
		span Span
		want bool
	}{
		{
			name: "runtime span whose name happens to contain response",
			span: Span{Name: "handle_responses", Kind: ActivityResponse},
			want: false,
		},
		{
			name: "conversation span",
			span: Span{Name: "poll", Agent: AgentContext{RunID: "conversation-1"}},
			want: true,
		},
		{
			name: "tool span",
			span: Span{Name: "call", ToolName: "exec_command"},
			want: true,
		},
		{
			name: "recognized semantic event",
			span: Span{Name: "gen_ai.response.completed", Kind: ActivityResponse},
			want: true,
		},
		{
			name: "reported usage",
			span: Span{Name: "request", Agent: AgentContext{Tokens: TokenUsage{Input: 3}}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSemanticSpan(tt.span); got != tt.want {
				t.Fatalf("IsSemanticSpan() = %v, want %v", got, tt.want)
			}
		})
	}
}
