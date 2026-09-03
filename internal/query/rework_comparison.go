package query

import (
	"context"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
)

// ReworkComparisonPair names requested baseline and current conversations.
// Readers resolve both identities to canonical roots in one retained snapshot.
type ReworkComparisonPair struct {
	Baseline ConversationIdentity
	Current  ConversationIdentity
}

// ReworkDiagnosticSnapshot contains one resolved conversation's diagnostic facts.
// Timing and Analysis must come from the same read as the other comparison side.
type ReworkDiagnosticSnapshot struct {
	Identity  ConversationIdentity
	StartedAt time.Time
	EndedAt   time.Time
	Analysis  SessionRework
}

type ReworkComparisonSummary struct {
	SourceID           string         `json:"sourceId"`
	SessionID          string         `json:"sessionId"`
	StartedAt          time.Time      `json:"startedAt"`
	EndedAt            time.Time      `json:"endedAt"`
	Coverage           ReworkCoverage `json:"coverage"`
	ProjectionCoverage string         `json:"projectionCoverage"`
	Harness            HarnessContext `json:"-"`
}

type ReworkComparisonValue struct {
	Availability string   `json:"availability"`
	Reason       string   `json:"reason,omitempty"`
	Numerator    *float64 `json:"numerator"`
	Denominator  *float64 `json:"denominator"`
	Value        *float64 `json:"value"`
}

type ReworkComparisonRow struct {
	ID           string                `json:"id"`
	Unit         string                `json:"unit"`
	Availability string                `json:"availability"`
	Baseline     ReworkComparisonValue `json:"baseline"`
	Current      ReworkComparisonValue `json:"current"`
	Delta        *float64              `json:"delta"`
}

// ReworkComparison carries no activities, episodes, or content. Adapters map the
// existing harness value explicitly, using its validated public view.
type ReworkComparison struct {
	Status   string                  `json:"status"`
	Code     string                  `json:"code,omitempty"`
	Reason   string                  `json:"reason,omitempty"`
	Baseline ReworkComparisonSummary `json:"baseline"`
	Current  ReworkComparisonSummary `json:"current"`
	Rows     []ReworkComparisonRow   `json:"rows"`
}

type ReworkComparisonReader interface {
	CompareRework(context.Context, ReworkComparisonPair) (ReworkComparison, error)
}

// CompareReworkSnapshots compares already resolved, coherently read facts.
// Raw values and differences are never rounded for presentation here.
func CompareReworkSnapshots(baseline, current ReworkDiagnosticSnapshot) ReworkComparison {
	result := ReworkComparison{
		Status: "invalid", Baseline: reworkComparisonSummary(baseline), Current: reworkComparisonSummary(current),
	}
	if result.Code, result.Reason = reworkComparisonEligibility(baseline, current); result.Code != "" {
		return result
	}
	before, after := reworkComparisonValues(baseline), reworkComparisonValues(current)
	result.Status = "ready"
	result.Rows = []ReworkComparisonRow{
		compareReworkValue("initial_validation_success_proxy", "percent", before[0], after[0]),
		compareReworkValue("rework_token_share", "percent", before[1], after[1]),
		compareReworkValue("retry_cycle_effort_share", "percent", before[2], after[2]),
		compareReworkValue("tool_failure_rate", "percent", before[3], after[3]),
		compareReworkValue("recurring_loops_per_100_validations", "per100", before[4], after[4]),
	}
	return result
}

func reworkComparisonEligibility(baseline, current ReworkDiagnosticSnapshot) (string, string) {
	for _, snapshot := range []ReworkDiagnosticSnapshot{baseline, current} {
		if snapshot.Identity.SourceID() == "" || snapshot.Identity.ConversationID() == "" ||
			snapshot.Identity.SourceID() != snapshot.Analysis.SourceID || snapshot.Identity.ConversationID() != snapshot.Analysis.RunID {
			return "identity_mismatch", "The loaded analysis does not belong to its displayed conversation."
		}
	}
	for _, snapshot := range []ReworkDiagnosticSnapshot{baseline, current} {
		if snapshot.StartedAt.IsZero() || snapshot.EndedAt.IsZero() || snapshot.EndedAt.Before(snapshot.StartedAt) ||
			snapshot.StartedAt.Year() < 1 || snapshot.StartedAt.Year() > 9999 || snapshot.EndedAt.Year() < 1 || snapshot.EndedAt.Year() > 9999 {
			return "invalid_time", "A conversation has an invalid start or end time."
		}
	}
	if baseline.Identity.SourceID() != current.Identity.SourceID() {
		return "baseline_ineligible", "Baseline and current conversations must use the same source."
	}
	if baseline.Identity == current.Identity {
		return "baseline_ineligible", "Baseline and current identify the same conversation."
	}
	if baseline.EndedAt.After(current.StartedAt) {
		return "baseline_ineligible", "The baseline ends after the current conversation starts."
	}
	return "", ""
}

func reworkComparisonSummary(snapshot ReworkDiagnosticSnapshot) ReworkComparisonSummary {
	coverage := snapshot.Analysis.Report.Coverage
	projectionCoverage := "partial"
	if coverage.ActivityCoverage == "observed_projection_complete" {
		projectionCoverage = "complete"
	} else if coverage.ActivityCoverage == "" {
		projectionCoverage = "unknown"
	}
	return ReworkComparisonSummary{
		SourceID: snapshot.Identity.SourceID(), SessionID: snapshot.Identity.ConversationID(),
		StartedAt: snapshot.StartedAt, EndedAt: snapshot.EndedAt,
		Coverage: coverage, ProjectionCoverage: projectionCoverage, Harness: snapshot.Analysis.Harness,
	}
}

func reworkComparisonValues(snapshot ReworkDiagnosticSnapshot) [5]ReworkComparisonValue {
	report := snapshot.Analysis.Report
	return [5]ReworkComparisonValue{
		reworkRatio(float64(report.FirstPassSuccesses), float64(report.FirstPassEligibleValidations), "No eligible validation identities", "Inconsistent initial validation evidence"),
		reworkTokenShare(report.ReworkTokens, snapshot.Analysis.SessionTokens),
		reworkRatio(float64(report.ReworkDuration.Milliseconds()), float64(report.TotalAgentEffort.Milliseconds()), "Observed agent-active duration unavailable", "Inconsistent retry-cycle effort evidence"),
		reworkRatio(float64(report.ToolFailures), float64(report.ToolAttemptsWithOutcome), "No tool outcomes observed", "Inconsistent tool outcome evidence"),
		reworkRatio(float64(report.RecurringFailureLoops), float64(report.ValidationAttemptsWithOutcome), "No validation outcomes observed", "Inconsistent recurring-loop evidence"),
	}
}

func reworkTokenShare(rework, session canonical.TokenUsage) ReworkComparisonValue {
	value := ReworkComparisonValue{Availability: "unavailable"}
	if rework.TotalReported() {
		value.Numerator = new(float64(rework.Total()))
	}
	if session.TotalReported() {
		value.Denominator = new(float64(session.Total()))
	}
	if value.Denominator == nil || *value.Denominator <= 0 {
		value.Reason = "Session token total unavailable"
		return value
	}
	if value.Numerator == nil {
		value.Reason = "Rework token usage unavailable"
		return value
	}
	return reworkRatio(*value.Numerator, *value.Denominator, "Session token total unavailable", "Inconsistent rework token evidence")
}

func reworkRatio(numerator, denominator float64, missingReason, inconsistentReason string) ReworkComparisonValue {
	result := ReworkComparisonValue{Availability: "unavailable", Numerator: &numerator, Denominator: &denominator}
	if denominator <= 0 {
		result.Reason = missingReason
		return result
	}
	if numerator < 0 || numerator > denominator {
		result.Reason = inconsistentReason
		return result
	}
	result.Availability = "available"
	result.Value = new(numerator / denominator * 100)
	return result
}

func compareReworkValue(id, unit string, baseline, current ReworkComparisonValue) ReworkComparisonRow {
	row := ReworkComparisonRow{ID: id, Unit: unit, Availability: "unavailable", Baseline: baseline, Current: current}
	if baseline.Availability == "available" && current.Availability == "available" {
		row.Availability = "comparable"
		row.Delta = new(*current.Value - *baseline.Value)
	}
	return row
}
