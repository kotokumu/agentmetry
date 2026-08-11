package claude_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/theoden9014/agentmetry/internal/source/claude"
)

func TestParseCLIResult(t *testing.T) {
	got, err := claude.ParseCLIResult([]byte(`{
  "type":"result",
  "usage":{
    "input_tokens":100,
    "output_tokens":20,
    "cache_read_input_tokens":70,
    "cache_creation_input_tokens":4
  },
  "total_cost_usd":0.0125
}`))
	if err != nil {
		t.Fatal(err)
	}
	wantCost := 0.0125
	want := claude.CLIUsage{
		InputTokens:              100,
		OutputTokens:             20,
		CacheReadInputTokens:     70,
		CacheCreationInputTokens: 4,
		TotalCostUSD:             &wantCost,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("ParseCLIResult() mismatch (-want +got):\n%s", diff)
	}
}

func TestParseCLIResultRejectsMissingUsage(t *testing.T) {
	if _, err := claude.ParseCLIResult([]byte(`{"type":"result"}`)); err == nil {
		t.Fatal("ParseCLIResult() error = nil, want malformed usage error")
	}
}
