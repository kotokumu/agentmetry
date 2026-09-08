package claude_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/kotokumu/agentmetry/internal/source/claude"
)

func TestCallIdentityAliasesRetainsEveryValidTypedAlias(t *testing.T) {
	got := claude.CallIdentityAliases(map[string]any{
		"gen_ai.client.request.id": "client",
		"gen_ai.request.id":        "request",
		"event.sequence":           int64(7),
	})
	want := []claude.CallAlias{
		{Kind: claude.CallAliasClientRequestID, Value: "client"},
		{Kind: claude.CallAliasRequestID, Value: "request"},
		{Kind: claude.CallAliasEventSequence, Sequence: 7},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("aliases mismatch (-want +got):\n%s", diff)
	}
}

func TestCallIdentityAliasesRejectsNonOTLPIntegerSequence(t *testing.T) {
	for _, value := range []any{"7", float64(7), int(7), int64(-1)} {
		if got := claude.CallIdentityAliases(map[string]any{"event.sequence": value}); len(got) != 0 {
			t.Fatalf("sequence %#v produced aliases %#v", value, got)
		}
	}
}
