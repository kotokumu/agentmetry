package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kotokumu/agentmetry/internal/modelcall"
	"github.com/kotokumu/agentmetry/internal/query"
)

func loadCostSummary(ctx context.Context, reader sqlReader, where string, args ...any) (query.CostSummary, error) {
	statement := `SELECT c.call_id, a.basis, a.amount_micro_usd, a.primary_reason
FROM model_calls c JOIN model_call_attributions a ON a.call_id = c.call_id ` + where
	rows, err := reader.QueryContext(ctx, statement, args...)
	if err != nil {
		return query.CostSummary{}, fmt.Errorf("query model call costs: %w", err)
	}
	defer rows.Close()
	calls := make(map[string]modelcall.Attribution)
	for rows.Next() {
		var id, basis, reason string
		var amount sql.NullInt64
		if err := rows.Scan(&id, &basis, &amount, &reason); err != nil {
			return query.CostSummary{}, fmt.Errorf("scan model call cost: %w", err)
		}
		attribution := modelcall.Attribution{Basis: parseCostBasis(basis), Reason: parseReason(reason)}
		if amount.Valid {
			value := amount.Int64
			attribution.AmountMicroUSD = &value
		}
		calls[id] = attribution
	}
	if err := rows.Err(); err != nil {
		return query.CostSummary{}, fmt.Errorf("iterate model call costs: %w", err)
	}
	return costSummaryView(modelcall.SummarizeCost(calls)), nil
}

func (store *Store) dashboardCostSummary(ctx context.Context, reader sqlReader, filter query.DashboardFilter) (query.CostSummary, error) {
	var keys []string
	hasSearch := strings.TrimSpace(filter.Search) != ""
	if hasSearch {
		graph, err := loadSessionGraphWithReader(ctx, reader, filter.SourceID)
		if err != nil {
			return query.CostSummary{}, err
		}
		matched, err := store.searchSessionRoots(ctx, reader, query.SessionListFilter{Since: filter.Since, SourceID: filter.SourceID, Search: filter.Search, View: query.SessionListRoots}, graph)
		if err != nil {
			return query.CostSummary{}, err
		}
		keys = make([]string, 0, len(matched))
		for ref := range matched {
			keys = append(keys, ref.sourceID+"\x00"+ref.sessionID)
		}
	} else {
		rows, err := reader.QueryContext(ctx, `SELECT r.source, COALESCE(sm.root_session_id, r.run_id)
FROM session_rollups r
LEFT JOIN session_memberships sm ON sm.source = r.source AND sm.session_id = r.run_id
WHERE (? = '' OR r.source = ?)
GROUP BY r.source, COALESCE(sm.root_session_id, r.run_id)
HAVING MAX(r.ended_at) >= ?`, filter.SourceID, filter.SourceID, formatTime(filter.Since))
		if err != nil {
			return query.CostSummary{}, fmt.Errorf("select dashboard cost roots: %w", err)
		}
		for rows.Next() {
			var sourceID, rootID string
			if err := rows.Scan(&sourceID, &rootID); err != nil {
				_ = rows.Close()
				return query.CostSummary{}, err
			}
			keys = append(keys, sourceID+"\x00"+rootID)
		}
		if err := rows.Close(); err != nil {
			return query.CostSummary{}, err
		}
	}
	payload, _ := json.Marshal(keys)
	matchedJSON := string(payload)
	return loadCostSummary(ctx, reader, `LEFT JOIN session_memberships sm ON sm.source = c.source AND sm.session_id = c.native_session_id
WHERE (c.native_session_id <> '' AND c.source || char(0) || COALESCE(sm.root_session_id, c.native_session_id) IN (SELECT value FROM json_each(?)))
   OR (? = 0 AND c.native_session_id = '' AND c.filter_at >= ? AND (? = '' OR c.source = ?))`,
		matchedJSON, boolInt(hasSearch), formatTime(filter.Since), filter.SourceID, filter.SourceID)
}

func sessionCostSummary(ctx context.Context, reader sqlReader, sourceID string, sessionIDs []string) (query.CostSummary, error) {
	payload, _ := json.Marshal(sessionIDs)
	return loadCostSummary(ctx, reader, `WHERE c.source = ? AND c.native_session_id IN (SELECT value FROM json_each(?))`, sourceID, string(payload))
}

func sessionListCostSummary(ctx context.Context, reader sqlReader, sourceID, sessionID string, roots bool) (query.CostSummary, error) {
	if !roots {
		return sessionCostSummary(ctx, reader, sourceID, []string{sessionID})
	}
	return loadCostSummary(ctx, reader, `LEFT JOIN session_memberships sm ON sm.source = c.source AND sm.session_id = c.native_session_id
WHERE c.source = ? AND COALESCE(sm.root_session_id, c.native_session_id) = ?`, sourceID, sessionID)
}

func traceCostSummary(ctx context.Context, reader sqlReader, traceID string) (query.CostSummary, error) {
	return loadCostSummary(ctx, reader, `WHERE EXISTS (
  SELECT 1 FROM model_call_trace_memberships m WHERE m.call_id = c.call_id AND m.trace_id = ?
)`, traceID)
}

func (store *Store) hydrateModelCallRelations(ctx context.Context, reader sqlReader, activities []query.Activity) error {
	if len(activities) == 0 {
		return nil
	}
	ids := make([]string, len(activities))
	positions := make(map[string]int, len(activities))
	for index := range activities {
		ids[index] = activities[index].ID
		positions[activities[index].ID] = index
	}
	payload, _ := json.Marshal(ids)
	rows, err := reader.QueryContext(ctx, `SELECT l.activity_id, l.call_id, l.evidence_role,
  c.identity_basis, a.basis, a.amount_micro_usd, a.primary_reason, a.rate_entry_id
FROM model_call_activity_links l
JOIN model_calls c ON c.call_id = l.call_id
JOIN model_call_attributions a ON a.call_id = l.call_id
WHERE l.activity_id IN (SELECT value FROM json_each(?))`, string(payload))
	if err != nil {
		return fmt.Errorf("query activity model call relations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var activityID, callID, role, identity, basis, reason string
		var rateID sql.NullString
		var amount sql.NullInt64
		if err := rows.Scan(&activityID, &callID, &role, &identity, &basis, &amount, &reason, &rateID); err != nil {
			return fmt.Errorf("scan activity model call relation: %w", err)
		}
		index, exists := positions[activityID]
		if !exists {
			continue
		}
		if role == "representative" {
			value := query.ModelCallCost{CallID: callID, IdentityBasis: identity, Basis: basis, PrimaryReason: reason, RateEntryID: rateID.String}
			if amount.Valid {
				microUSD := amount.Int64
				value.AmountMicroUSD = &microUSD
			}
			activities[index].ModelCallCost = &value
			activities[index].ModelCallRef = nil
		} else {
			activities[index].ModelCallCost = nil
			activities[index].ModelCallRef = &query.ModelCallRef{CallID: callID, IdentityBasis: identity, EvidenceRole: role}
			activities[index].CostUSD = nil
		}
	}
	return rows.Err()
}

func costSummaryView(value modelcall.CostSummary) query.CostSummary {
	result := query.CostSummary{
		AmountMicroUSD: value.AmountMicroUSD, Basis: summaryBasisText(value.Basis), Coverage: coverageText(value.Coverage),
		EligibleCalls: value.EligibleCalls, PricedCalls: value.PricedCalls, UnpricedReasons: make([]query.CostReasonCount, len(value.UnpricedReasons)),
	}
	for index, reason := range value.UnpricedReasons {
		result.UnpricedReasons[index] = query.CostReasonCount{Reason: reasonText(reason.Reason), Count: reason.Count}
	}
	if value.AggregateError == modelcall.AggregateCalculationError {
		result.AggregateError = "calculation_error"
	}
	return result
}

func parseCostBasis(value string) modelcall.CostBasis {
	switch value {
	case "provider_reported":
		return modelcall.CostProviderReported
	case "rate_card_estimate":
		return modelcall.CostRateCardEstimate
	default:
		return modelcall.CostUnavailable
	}
}

func parseReason(value string) modelcall.UnavailableReason {
	for reason := modelcall.ReasonConflictingAuthoritativeEvidence; reason <= modelcall.ReasonCalculationError; reason++ {
		if reasonText(reason) == value {
			return reason
		}
	}
	return modelcall.ReasonUnspecified
}

func reasonText(value modelcall.UnavailableReason) string {
	return map[modelcall.UnavailableReason]string{
		modelcall.ReasonConflictingAuthoritativeEvidence: "conflicting_authoritative_evidence",
		modelcall.ReasonInvalidProviderAmount:            "invalid_provider_amount",
		modelcall.ReasonMissingModel:                     "missing_model",
		modelcall.ReasonMissingOccurredAt:                "missing_occurred_at",
		modelcall.ReasonMissingTokenUsage:                "missing_token_usage",
		modelcall.ReasonUnsupportedBillingMode:           "unsupported_billing_mode",
		modelcall.ReasonUnsupportedUsageCondition:        "unsupported_usage_condition",
		modelcall.ReasonRateNotFound:                     "rate_not_found",
		modelcall.ReasonRateConflict:                     "rate_conflict",
		modelcall.ReasonCalculationError:                 "calculation_error",
	}[value]
}

func summaryBasisText(value modelcall.SummaryBasis) string {
	return map[modelcall.SummaryBasis]string{modelcall.SummaryProviderReported: "provider_reported", modelcall.SummaryRateCardEstimate: "rate_card_estimate", modelcall.SummaryMixed: "mixed", modelcall.SummaryUnavailable: "unavailable"}[value]
}

func coverageText(value modelcall.Coverage) string {
	return map[modelcall.Coverage]string{modelcall.CoverageComplete: "complete", modelcall.CoveragePartial: "partial", modelcall.CoverageUnavailable: "unavailable"}[value]
}

func completeLegacyCost(summary query.CostSummary) *float64 {
	if summary.Coverage != "complete" || summary.AmountMicroUSD == nil {
		return nil
	}
	value := float64(*summary.AmountMicroUSD) / 1_000_000
	return &value
}
