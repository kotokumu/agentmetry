package mcpserver

import (
	"testing"

	"github.com/kotokumu/agentmetry/internal/query"
)

func TestMapCostSummaryOutputUsesExactDecimalStrings(t *testing.T) {
	amount := int64(3_345)
	got := mapCostSummaryOutput(query.CostSummary{AmountMicroUSD: &amount, Basis: "rate_card_estimate", Coverage: "partial", EligibleCalls: 2, PricedCalls: 1, UnpricedReasons: []query.CostReasonCount{{Reason: "rate_not_found", Count: 1}}})
	if got.AmountMicroUSD != "3345" || got.AmountUSD != "0.003345" || got.EligibleCalls != "2" || got.PricedCalls != "1" {
		t.Fatalf("cost summary output = %#v", got)
	}
	if len(got.UnpricedReasons) != 1 || got.UnpricedReasons[0].Count != "1" {
		t.Fatalf("reason output = %#v", got.UnpricedReasons)
	}
}

func TestMapUnavailableCallOmitsAmounts(t *testing.T) {
	got := mapModelCallCostOutput(query.ModelCallCost{CallID: "call-1", IdentityBasis: "journal_evidence_fallback", Basis: "unavailable", PrimaryReason: "rate_not_found"})
	if got.AmountMicroUSD != "" || got.AmountUSD != "" || got.PrimaryReason != "rate_not_found" {
		t.Fatalf("unavailable call output = %#v", got)
	}
}
