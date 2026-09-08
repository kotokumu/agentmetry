package connectapi

import (
	"testing"

	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/internal/query"
)

func TestMapCostSummaryPreservesZeroAndCoverage(t *testing.T) {
	zero := int64(0)
	got := mapCostSummary(query.CostSummary{AmountMicroUSD: &zero, Basis: "rate_card_estimate", Coverage: "partial", EligibleCalls: 2, PricedCalls: 1, UnpricedReasons: []query.CostReasonCount{{Reason: "rate_not_found", Count: 1}}})
	if got.AmountMicroUsd == nil || *got.AmountMicroUsd != 0 || got.Basis != v1.CostSummaryBasis_COST_SUMMARY_BASIS_RATE_CARD_ESTIMATE || got.Coverage != v1.CostCoverage_COST_COVERAGE_PARTIAL {
		t.Fatalf("mapped summary = %#v", got)
	}
	if len(got.UnpricedReasons) != 1 || got.UnpricedReasons[0].Reason != v1.CostUnavailableReason_COST_UNAVAILABLE_REASON_RATE_NOT_FOUND {
		t.Fatalf("mapped reasons = %#v", got.UnpricedReasons)
	}
}

func TestMapActivityUsesExclusiveModelCallRelation(t *testing.T) {
	amount := int64(12)
	mapped := mapActivities([]query.Activity{{ID: "activity", ModelCallCost: &query.ModelCallCost{CallID: "call", IdentityBasis: "journal_evidence_fallback", Basis: "rate_card_estimate", AmountMicroUSD: &amount}}})[0]
	if mapped.GetModelCallCost() == nil || mapped.GetModelCallRef() != nil || mapped.GetModelCallCost().GetAmountMicroUsd() != 12 {
		t.Fatalf("model call relation = %#v", mapped.ModelCallRelation)
	}
}
