package connectapi

import (
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/internal/query"
)

func mapModelCallCost(value query.ModelCallCost) *v1.ModelCallCost {
	result := &v1.ModelCallCost{CallId: value.CallID, IdentityBasis: identityBasis(value.IdentityBasis), Basis: callCostBasis(value.Basis), AmountMicroUsd: value.AmountMicroUSD}
	if value.PrimaryReason != "" {
		reason := unavailableReason(value.PrimaryReason)
		result.PrimaryReason = &reason
	}
	if value.RateEntryID != "" {
		result.RateEntryId = &value.RateEntryID
	}
	return result
}

func mapCostSummary(value query.CostSummary) *v1.CostSummary {
	result := &v1.CostSummary{AmountMicroUsd: value.AmountMicroUSD, Basis: summaryBasis(value.Basis), Coverage: costCoverage(value.Coverage), EligibleCalls: value.EligibleCalls, PricedCalls: value.PricedCalls}
	for _, reason := range value.UnpricedReasons {
		result.UnpricedReasons = append(result.UnpricedReasons, &v1.CostReasonCount{Reason: unavailableReason(reason.Reason), Count: reason.Count})
	}
	if value.AggregateError != "" {
		errorValue := v1.CostAggregationError_COST_AGGREGATION_ERROR_CALCULATION_ERROR
		result.AggregateError = &errorValue
	}
	return result
}

func identityBasis(value string) v1.CallIdentityBasis {
	return map[string]v1.CallIdentityBasis{
		"claude_client_request_id":  v1.CallIdentityBasis_CALL_IDENTITY_BASIS_CLAUDE_CLIENT_REQUEST_ID,
		"claude_request_id":         v1.CallIdentityBasis_CALL_IDENTITY_BASIS_CLAUDE_REQUEST_ID,
		"claude_event_sequence":     v1.CallIdentityBasis_CALL_IDENTITY_BASIS_CLAUDE_EVENT_SEQUENCE,
		"journal_evidence_fallback": v1.CallIdentityBasis_CALL_IDENTITY_BASIS_JOURNAL_EVIDENCE_FALLBACK,
	}[value]
}

func callCostBasis(value string) v1.CallCostBasis {
	return map[string]v1.CallCostBasis{"provider_reported": v1.CallCostBasis_CALL_COST_BASIS_PROVIDER_REPORTED, "rate_card_estimate": v1.CallCostBasis_CALL_COST_BASIS_RATE_CARD_ESTIMATE, "unavailable": v1.CallCostBasis_CALL_COST_BASIS_UNAVAILABLE}[value]
}

func summaryBasis(value string) v1.CostSummaryBasis {
	return map[string]v1.CostSummaryBasis{"provider_reported": v1.CostSummaryBasis_COST_SUMMARY_BASIS_PROVIDER_REPORTED, "rate_card_estimate": v1.CostSummaryBasis_COST_SUMMARY_BASIS_RATE_CARD_ESTIMATE, "mixed": v1.CostSummaryBasis_COST_SUMMARY_BASIS_MIXED, "unavailable": v1.CostSummaryBasis_COST_SUMMARY_BASIS_UNAVAILABLE}[value]
}

func costCoverage(value string) v1.CostCoverage {
	return map[string]v1.CostCoverage{"complete": v1.CostCoverage_COST_COVERAGE_COMPLETE, "partial": v1.CostCoverage_COST_COVERAGE_PARTIAL, "unavailable": v1.CostCoverage_COST_COVERAGE_UNAVAILABLE}[value]
}

func evidenceRole(value string) v1.ModelCallEvidenceRole {
	return map[string]v1.ModelCallEvidenceRole{"duplicate_authoritative": v1.ModelCallEvidenceRole_MODEL_CALL_EVIDENCE_ROLE_DUPLICATE_AUTHORITATIVE, "corroborating": v1.ModelCallEvidenceRole_MODEL_CALL_EVIDENCE_ROLE_CORROBORATING}[value]
}

func unavailableReason(value string) v1.CostUnavailableReason {
	return map[string]v1.CostUnavailableReason{
		"conflicting_authoritative_evidence": v1.CostUnavailableReason_COST_UNAVAILABLE_REASON_CONFLICTING_AUTHORITATIVE_EVIDENCE,
		"invalid_provider_amount":            v1.CostUnavailableReason_COST_UNAVAILABLE_REASON_INVALID_PROVIDER_AMOUNT,
		"missing_model":                      v1.CostUnavailableReason_COST_UNAVAILABLE_REASON_MISSING_MODEL,
		"missing_occurred_at":                v1.CostUnavailableReason_COST_UNAVAILABLE_REASON_MISSING_OCCURRED_AT,
		"missing_token_usage":                v1.CostUnavailableReason_COST_UNAVAILABLE_REASON_MISSING_TOKEN_USAGE,
		"unsupported_billing_mode":           v1.CostUnavailableReason_COST_UNAVAILABLE_REASON_UNSUPPORTED_BILLING_MODE,
		"unsupported_usage_condition":        v1.CostUnavailableReason_COST_UNAVAILABLE_REASON_UNSUPPORTED_USAGE_CONDITION,
		"rate_not_found":                     v1.CostUnavailableReason_COST_UNAVAILABLE_REASON_RATE_NOT_FOUND,
		"rate_conflict":                      v1.CostUnavailableReason_COST_UNAVAILABLE_REASON_RATE_CONFLICT,
		"calculation_error":                  v1.CostUnavailableReason_COST_UNAVAILABLE_REASON_CALCULATION_ERROR,
	}[value]
}
