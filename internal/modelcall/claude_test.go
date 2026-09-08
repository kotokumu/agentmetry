package modelcall_test

import (
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/modelcall"
)

func TestResolveClaudeCallsBridgesAliasesIndependentOfArrivalOrder(t *testing.T) {
	amount := int64(10)
	at := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	evidence := []modelcall.ClaudeEvidence{
		{ActivityID: "client", SessionID: "session", Locator: []byte{3}, Aliases: []modelcall.IdentityAlias{{Basis: modelcall.IdentityClaudeClientRequestID, Value: "client-1"}}, Model: "claude", OccurredAt: at, ProviderAmount: &amount, ProviderAmountState: "valid"},
		{ActivityID: "request", SessionID: "session", Locator: []byte{2}, Aliases: []modelcall.IdentityAlias{{Basis: modelcall.IdentityClaudeRequestID, Value: "request-1"}}, Model: "claude", OccurredAt: at, ProviderAmount: &amount, ProviderAmountState: "valid"},
		{ActivityID: "bridge", SessionID: "session", Locator: []byte{1}, Aliases: []modelcall.IdentityAlias{{Basis: modelcall.IdentityClaudeClientRequestID, Value: "client-1"}, {Basis: modelcall.IdentityClaudeRequestID, Value: "request-1"}}, Model: "claude", OccurredAt: at, ProviderAmount: &amount, ProviderAmountState: "valid"},
	}
	wantID := modelcall.CallID("claude", "session", modelcall.IdentityClaudeClientRequestID, modelcall.StringAlias("client-1"))
	for _, order := range [][]int{{0, 1, 2}, {2, 1, 0}, {1, 2, 0}, {0, 2, 1}} {
		ordered := []modelcall.ClaudeEvidence{evidence[order[0]], evidence[order[1]], evidence[order[2]]}
		calls := modelcall.ResolveClaudeCalls(ordered)
		if len(calls) != 1 || calls[0].ID != wantID || calls[0].RepresentativeActivityID != "bridge" || calls[0].Attribution.AmountMicroUSD == nil || *calls[0].Attribution.AmountMicroUSD != amount {
			t.Fatalf("order %v resolved to %#v", order, calls)
		}
	}
}

func TestResolveClaudeCallsReconcilesAllAuthoritativeFacts(t *testing.T) {
	amount := int64(10)
	at := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	alias := []modelcall.IdentityAlias{{Basis: modelcall.IdentityClaudeRequestID, Value: "request-1"}}
	calls := modelcall.ResolveClaudeCalls([]modelcall.ClaudeEvidence{
		{ActivityID: "a", SessionID: "session", Locator: []byte{1}, Aliases: alias, Model: "claude-a", OccurredAt: at, Usage: canonical.TokenUsage{Input: 1}, ProviderAmount: &amount, ProviderAmountState: "valid"},
		{ActivityID: "b", SessionID: "session", Locator: []byte{2}, Aliases: alias, Model: "claude-b", OccurredAt: at, Usage: canonical.TokenUsage{Input: 2}, ProviderAmount: &amount, ProviderAmountState: "valid"},
	})
	if len(calls) != 1 || calls[0].Attribution.Basis != modelcall.CostUnavailable || calls[0].Attribution.Reason != modelcall.ReasonConflictingAuthoritativeEvidence {
		t.Fatalf("conflicting facts resolved to %#v", calls)
	}
}

func TestResolveClaudeCallsComplementsAbsentFactsFromDuplicateEvidence(t *testing.T) {
	amount := int64(10)
	at := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	alias := []modelcall.IdentityAlias{{Basis: modelcall.IdentityClaudeRequestID, Value: "request-1"}}
	calls := modelcall.ResolveClaudeCalls([]modelcall.ClaudeEvidence{
		{ActivityID: "representative", SessionID: "session", Locator: []byte{1}, Aliases: alias, ProviderAmount: &amount, ProviderAmountState: "valid"},
		{ActivityID: "facts", SessionID: "session", Locator: []byte{2}, Aliases: alias, Model: "claude", OccurredAt: at, Usage: canonical.TokenUsage{Input: 7, Presence: canonical.TokenPresence{Output: true}}, ProviderAmount: &amount, ProviderAmountState: "valid"},
	})
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	call := calls[0]
	if call.Model != "claude" || !call.OccurredAt.Equal(at) || call.Usage.Input != 7 || !call.Usage.InputReported() || call.Usage.Output != 0 || !call.Usage.OutputReported() {
		t.Fatalf("reconciled facts = model %q, occurred %s, usage %#v", call.Model, call.OccurredAt, call.Usage)
	}
}
