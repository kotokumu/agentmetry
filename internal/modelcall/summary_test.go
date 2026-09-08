package modelcall_test

import (
	"math"
	"testing"

	"github.com/kotokumu/agentmetry/internal/modelcall"
)

func amount(value int64) *int64 { return &value }

func TestSummarizeCost(t *testing.T) {
	tests := []struct {
		name     string
		calls    map[string]modelcall.Attribution
		amount   *int64
		basis    modelcall.SummaryBasis
		coverage modelcall.Coverage
		priced   int64
	}{
		{name: "empty", calls: map[string]modelcall.Attribution{}, basis: modelcall.SummaryUnavailable, coverage: modelcall.CoverageUnavailable},
		{name: "provider complete", calls: map[string]modelcall.Attribution{"a": {Basis: modelcall.CostProviderReported, AmountMicroUSD: amount(0)}}, amount: amount(0), basis: modelcall.SummaryProviderReported, coverage: modelcall.CoverageComplete, priced: 1},
		{name: "rate complete", calls: map[string]modelcall.Attribution{"a": {Basis: modelcall.CostRateCardEstimate, AmountMicroUSD: amount(4)}}, amount: amount(4), basis: modelcall.SummaryRateCardEstimate, coverage: modelcall.CoverageComplete, priced: 1},
		{name: "mixed partial", calls: map[string]modelcall.Attribution{
			"a": {Basis: modelcall.CostProviderReported, AmountMicroUSD: amount(2)},
			"b": {Basis: modelcall.CostRateCardEstimate, AmountMicroUSD: amount(3)},
			"c": {Basis: modelcall.CostUnavailable, Reason: modelcall.ReasonRateNotFound},
		}, amount: amount(5), basis: modelcall.SummaryMixed, coverage: modelcall.CoveragePartial, priced: 2},
		{name: "all unavailable", calls: map[string]modelcall.Attribution{"a": {Basis: modelcall.CostUnavailable, Reason: modelcall.ReasonMissingModel}}, basis: modelcall.SummaryUnavailable, coverage: modelcall.CoverageUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := modelcall.SummarizeCost(test.calls)
			if !sameAmount(got.AmountMicroUSD, test.amount) || got.Basis != test.basis || got.Coverage != test.coverage || got.PricedCalls != test.priced || got.EligibleCalls != int64(len(test.calls)) {
				t.Fatalf("SummarizeCost() = %#v", got)
			}
		})
	}
}

func TestSummarizeCostCountsReasonsAndDetectsOverflow(t *testing.T) {
	summary := modelcall.SummarizeCost(map[string]modelcall.Attribution{
		"a": {Basis: modelcall.CostProviderReported, AmountMicroUSD: amount(math.MaxInt64)},
		"b": {Basis: modelcall.CostRateCardEstimate, AmountMicroUSD: amount(1)},
		"c": {Basis: modelcall.CostUnavailable, Reason: modelcall.ReasonRateNotFound},
		"d": {Basis: modelcall.CostUnavailable, Reason: modelcall.ReasonRateNotFound},
	})
	if summary.AmountMicroUSD != nil || summary.Coverage != modelcall.CoverageUnavailable || summary.AggregateError != modelcall.AggregateCalculationError {
		t.Fatalf("overflow summary = %#v", summary)
	}
	if summary.EligibleCalls != 4 || summary.PricedCalls != 2 || summary.Basis != modelcall.SummaryMixed {
		t.Fatalf("overflow summary lost facts: %#v", summary)
	}
	if len(summary.UnpricedReasons) != 1 || summary.UnpricedReasons[0].Count != 2 || summary.UnpricedReasons[0].Reason != modelcall.ReasonRateNotFound {
		t.Fatalf("reason counts = %#v", summary.UnpricedReasons)
	}
}

func sameAmount(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
