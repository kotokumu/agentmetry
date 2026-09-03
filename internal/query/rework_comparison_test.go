package query

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"testing"
	"time"
)

func TestCompareReworkSnapshots(t *testing.T) {
	type args struct {
		baseline ReworkDiagnosticSnapshot
		current  ReworkDiagnosticSnapshot
	}
	tests := []struct {
		name string
		args args
		want ReworkComparison
	}{
		{
			name: "five raw metrics retain every operand coverage and harness",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID: "codex",
						RunID:    "before",
						SessionTokens: canonical.TokenUsage{
							Input:  800,
							Output: 200,
						},
						Harness: unreportedHarnessContext{
							counts: HarnessEvidenceCounts{
								EligibleRecords:   8,
								UnreportedRecords: 8,
							},
						},
						Report: ReworkReport{
							Cycles:                       []ReworkCycle{{Evidence: []Evidence{{Activity: "private evidence sentinel"}}}},
							FailureEpisodes:              []RecurringFailureEpisode{{AgentID: "private episode sentinel"}},
							FirstPassSuccesses:           1,
							FirstPassEligibleValidations: 4,
							ReworkTokens: canonical.TokenUsage{
								Input:  300,
								Output: 100,
							},
							ReworkDuration:                400 * time.Millisecond,
							TotalAgentEffort:              time.Second,
							ToolFailures:                  2,
							ToolAttemptsWithOutcome:       4,
							RecurringFailureLoops:         2,
							ValidationAttemptsWithOutcome: 4,
							Coverage: ReworkCoverage{
								ActivityCoverage: "observed_projection_complete",
								CanonicalEvents:  8,
							},
						},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID: "codex",
						RunID:    "after",
						SessionTokens: canonical.TokenUsage{
							Input:  700,
							Output: 300,
						},
						Report: ReworkReport{
							FirstPassSuccesses:           3,
							FirstPassEligibleValidations: 4,
							ReworkTokens: canonical.TokenUsage{
								Input:  150,
								Output: 50,
							},
							ReworkDuration:                100 * time.Millisecond,
							TotalAgentEffort:              time.Second,
							ToolFailures:                  1,
							ToolAttemptsWithOutcome:       4,
							RecurringFailureLoops:         1,
							ValidationAttemptsWithOutcome: 5,
							Coverage: ReworkCoverage{
								ActivityCoverage: "partial_page",
								CanonicalEvents:  6,
							},
						},
					},
				},
			},
			want: ReworkComparison{
				Status: "ready",
				Baseline: ReworkComparisonSummary{
					SourceID:  "codex",
					SessionID: "before",
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Coverage: ReworkCoverage{
						ActivityCoverage: "observed_projection_complete",
						CanonicalEvents:  8,
					},
					ProjectionCoverage: "complete",
					Harness: unreportedHarnessContext{
						counts: HarnessEvidenceCounts{
							EligibleRecords:   8,
							UnreportedRecords: 8,
						},
					},
				},
				Current: ReworkComparisonSummary{
					SourceID:  "codex",
					SessionID: "after",
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Coverage: ReworkCoverage{
						ActivityCoverage: "partial_page",
						CanonicalEvents:  6,
					},
					ProjectionCoverage: "partial",
				},
				Rows: []ReworkComparisonRow{
					{
						ID:           "initial_validation_success_proxy",
						Unit:         "percent",
						Availability: "comparable",
						Baseline: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(1)),
							Denominator:  new(float64(4)),
							Value:        new(float64(25)),
						},
						Current: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(3)),
							Denominator:  new(float64(4)),
							Value:        new(float64(75)),
						},
						Delta: new(float64(50)),
					},
					{
						ID:           "rework_token_share",
						Unit:         "percent",
						Availability: "comparable",
						Baseline: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(400)),
							Denominator:  new(float64(1000)),
							Value:        new(float64(40)),
						},
						Current: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(200)),
							Denominator:  new(float64(1000)),
							Value:        new(float64(20)),
						},
						Delta: new(float64(-20)),
					},
					{
						ID:           "retry_cycle_effort_share",
						Unit:         "percent",
						Availability: "comparable",
						Baseline: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(400)),
							Denominator:  new(float64(1000)),
							Value:        new(float64(40)),
						},
						Current: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(100)),
							Denominator:  new(float64(1000)),
							Value:        new(float64(10)),
						},
						Delta: new(float64(-30)),
					},
					{
						ID:           "tool_failure_rate",
						Unit:         "percent",
						Availability: "comparable",
						Baseline: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(2)),
							Denominator:  new(float64(4)),
							Value:        new(float64(50)),
						},
						Current: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(1)),
							Denominator:  new(float64(4)),
							Value:        new(float64(25)),
						},
						Delta: new(float64(-25)),
					},
					{
						ID:           "recurring_loops_per_100_validations",
						Unit:         "per100",
						Availability: "comparable",
						Baseline: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(2)),
							Denominator:  new(float64(4)),
							Value:        new(float64(50)),
						},
						Current: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(1)),
							Denominator:  new(float64(5)),
							Value:        new(float64(20)),
						},
						Delta: new(float64(-30)),
					},
				},
			},
		},
		{
			name: "raw sub-decimal delta is not rounded",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "before",
						SessionTokens: canonical.TokenUsage{},
						Report: ReworkReport{
							FirstPassSuccesses:           5000,
							FirstPassEligibleValidations: 10000,
						},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "after",
						SessionTokens: canonical.TokenUsage{},
						Report: ReworkReport{
							FirstPassSuccesses:           5004,
							FirstPassEligibleValidations: 10000,
						},
					},
				},
			},
			want: ReworkComparison{
				Status: "ready",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(10, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
				Rows: []ReworkComparisonRow{
					{
						ID:           "initial_validation_success_proxy",
						Unit:         "percent",
						Availability: "comparable",
						Baseline: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(5000)),
							Denominator:  new(float64(10000)),
							Value:        new(float64(50)),
						},
						Current: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(5004)),
							Denominator:  new(float64(10000)),
							Value:        new(float64(50.04)),
						},
						Delta: new(float64(0.04)),
					},
					{
						ID:           "rework_token_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Session token total unavailable",
							Numerator:    nil,
							Denominator:  nil,
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Session token total unavailable",
							Numerator:    nil,
							Denominator:  nil,
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "retry_cycle_effort_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Observed agent-active duration unavailable",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Observed agent-active duration unavailable",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "tool_failure_rate",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No tool outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No tool outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "recurring_loops_per_100_validations",
						Unit:         "per100",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No validation outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No validation outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
				},
			},
		},
		{
			name: "missing token numerator differs from reported zero",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID: "codex",
						RunID:    "before",
						SessionTokens: canonical.TokenUsage{
							Input: 100,
							Presence: canonical.TokenPresence{
								Output: true,
							},
						},
						Report: ReworkReport{},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID: "codex",
						RunID:    "after",
						SessionTokens: canonical.TokenUsage{
							Input: 100,
							Presence: canonical.TokenPresence{
								Output: true,
							},
						},
						Report: ReworkReport{
							ReworkTokens: canonical.TokenUsage{
								Presence: canonical.TokenPresence{
									Input:  true,
									Output: true,
								},
							},
						},
					},
				},
			},
			want: ReworkComparison{
				Status: "ready",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(10, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
				Rows: []ReworkComparisonRow{
					{
						ID:           "initial_validation_success_proxy",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No eligible validation identities",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No eligible validation identities",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "rework_token_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Rework token usage unavailable",
							Numerator:    nil,
							Denominator:  new(float64(100)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(100)),
							Value:        new(float64(0)),
						},
						Delta: nil,
					},
					{
						ID:           "retry_cycle_effort_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Observed agent-active duration unavailable",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Observed agent-active duration unavailable",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "tool_failure_rate",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No tool outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No tool outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "recurring_loops_per_100_validations",
						Unit:         "per100",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No validation outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No validation outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
				},
			},
		},
		{
			name: "zero and unreported token denominators retain available operands",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID: "codex",
						RunID:    "before",
						SessionTokens: canonical.TokenUsage{
							Presence: canonical.TokenPresence{
								Input:  true,
								Output: true,
							},
						},
						Report: ReworkReport{
							ReworkTokens: canonical.TokenUsage{
								Input: 1,
								Presence: canonical.TokenPresence{
									Output: true,
								},
							},
						},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "after",
						SessionTokens: canonical.TokenUsage{},
						Report: ReworkReport{
							ReworkTokens: canonical.TokenUsage{
								Input: 1,
								Presence: canonical.TokenPresence{
									Output: true,
								},
							},
						},
					},
				},
			},
			want: ReworkComparison{
				Status: "ready",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(10, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
				Rows: []ReworkComparisonRow{
					{
						ID:           "initial_validation_success_proxy",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No eligible validation identities",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No eligible validation identities",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "rework_token_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Session token total unavailable",
							Numerator:    new(float64(1)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Session token total unavailable",
							Numerator:    new(float64(1)),
							Denominator:  nil,
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "retry_cycle_effort_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Observed agent-active duration unavailable",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Observed agent-active duration unavailable",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "tool_failure_rate",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No tool outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No tool outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "recurring_loops_per_100_validations",
						Unit:         "per100",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No validation outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No validation outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
				},
			},
		},
		{
			name: "inconsistent positive and negative evidence is unavailable",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID: "codex",
						RunID:    "before",
						SessionTokens: canonical.TokenUsage{
							Input: 100,
							Presence: canonical.TokenPresence{
								Output: true,
							},
						},
						Report: ReworkReport{
							FirstPassSuccesses:           5,
							FirstPassEligibleValidations: 4,
							ReworkTokens: canonical.TokenUsage{
								Input: 120,
								Presence: canonical.TokenPresence{
									Output: true,
								},
							},
							ReworkDuration:                2 * time.Second,
							TotalAgentEffort:              time.Second,
							ToolFailures:                  5,
							ToolAttemptsWithOutcome:       4,
							RecurringFailureLoops:         5,
							ValidationAttemptsWithOutcome: 4,
						},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID: "codex",
						RunID:    "after",
						SessionTokens: canonical.TokenUsage{
							Input: 100,
							Presence: canonical.TokenPresence{
								Output: true,
							},
						},
						Report: ReworkReport{
							FirstPassSuccesses:           -1,
							FirstPassEligibleValidations: 4,
							ReworkTokens: canonical.TokenUsage{
								Input: -1,
								Presence: canonical.TokenPresence{
									Output: true,
								},
							},
							ReworkDuration:                -time.Millisecond,
							TotalAgentEffort:              time.Second,
							ToolFailures:                  -1,
							ToolAttemptsWithOutcome:       4,
							RecurringFailureLoops:         -1,
							ValidationAttemptsWithOutcome: 4,
						},
					},
				},
			},
			want: ReworkComparison{
				Status: "ready",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(10, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
				Rows: []ReworkComparisonRow{
					{
						ID:           "initial_validation_success_proxy",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Inconsistent initial validation evidence",
							Numerator:    new(float64(5)),
							Denominator:  new(float64(4)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Inconsistent initial validation evidence",
							Numerator:    new(float64(-1)),
							Denominator:  new(float64(4)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "rework_token_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Inconsistent rework token evidence",
							Numerator:    new(float64(120)),
							Denominator:  new(float64(100)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Inconsistent rework token evidence",
							Numerator:    new(float64(-1)),
							Denominator:  new(float64(100)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "retry_cycle_effort_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Inconsistent retry-cycle effort evidence",
							Numerator:    new(float64(2000)),
							Denominator:  new(float64(1000)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Inconsistent retry-cycle effort evidence",
							Numerator:    new(float64(-1)),
							Denominator:  new(float64(1000)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "tool_failure_rate",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Inconsistent tool outcome evidence",
							Numerator:    new(float64(5)),
							Denominator:  new(float64(4)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Inconsistent tool outcome evidence",
							Numerator:    new(float64(-1)),
							Denominator:  new(float64(4)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "recurring_loops_per_100_validations",
						Unit:         "per100",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Inconsistent recurring-loop evidence",
							Numerator:    new(float64(5)),
							Denominator:  new(float64(4)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Inconsistent recurring-loop evidence",
							Numerator:    new(float64(-1)),
							Denominator:  new(float64(4)),
							Value:        nil,
						},
						Delta: nil,
					},
				},
			},
		},
		{
			name: "nonterminating ratios retain raw precision",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "before",
						SessionTokens: canonical.TokenUsage{},
						Report: ReworkReport{
							FirstPassSuccesses:           1,
							FirstPassEligibleValidations: 3,
						},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "after",
						SessionTokens: canonical.TokenUsage{},
						Report: ReworkReport{
							FirstPassSuccesses:           2,
							FirstPassEligibleValidations: 3,
						},
					},
				},
			},
			want: ReworkComparison{
				Status: "ready",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(10, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
				Rows: []ReworkComparisonRow{
					{
						ID:           "initial_validation_success_proxy",
						Unit:         "percent",
						Availability: "comparable",
						Baseline: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(1)),
							Denominator:  new(float64(3)),
							Value:        new(float64(33.33333333333333)),
						},
						Current: ReworkComparisonValue{
							Availability: "available",
							Numerator:    new(float64(2)),
							Denominator:  new(float64(3)),
							Value:        new(float64(66.66666666666666)),
						},
						Delta: new(float64(33.33333333333333)),
					},
					{
						ID:           "rework_token_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Session token total unavailable",
							Numerator:    nil,
							Denominator:  nil,
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Session token total unavailable",
							Numerator:    nil,
							Denominator:  nil,
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "retry_cycle_effort_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Observed agent-active duration unavailable",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Observed agent-active duration unavailable",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "tool_failure_rate",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No tool outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No tool outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "recurring_loops_per_100_validations",
						Unit:         "per100",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No validation outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No validation outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
				},
			},
		},
		{
			name: "zero-duration conversations touching at one instant remain eligible",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "before",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "after",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
			},
			want: ReworkComparison{
				Status: "ready",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Rows: []ReworkComparisonRow{
					{
						ID:           "initial_validation_success_proxy",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No eligible validation identities",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No eligible validation identities",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "rework_token_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Session token total unavailable",
							Numerator:    nil,
							Denominator:  nil,
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Session token total unavailable",
							Numerator:    nil,
							Denominator:  nil,
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "retry_cycle_effort_share",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Observed agent-active duration unavailable",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "Observed agent-active duration unavailable",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "tool_failure_rate",
						Unit:         "percent",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No tool outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No tool outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
					{
						ID:           "recurring_loops_per_100_validations",
						Unit:         "per100",
						Availability: "unavailable",
						Baseline: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No validation outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Current: ReworkComparisonValue{
							Availability: "unavailable",
							Reason:       "No validation outcomes observed",
							Numerator:    new(float64(0)),
							Denominator:  new(float64(0)),
							Value:        nil,
						},
						Delta: nil,
					},
				},
			},
		},
		{
			name: "same canonical conversation is ineligible",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "before",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "before",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
			},
			want: ReworkComparison{
				Status: "invalid",
				Code:   "baseline_ineligible",
				Reason: "Baseline and current identify the same conversation.",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(10, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
			},
		},
		{
			name: "cross-source identity collision is ineligible",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "shared",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "shared",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "claude-code",
						conversationID: "shared",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "claude-code",
						RunID:         "shared",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
			},
			want: ReworkComparison{
				Status: "invalid",
				Code:   "baseline_ineligible",
				Reason: "Baseline and current conversations must use the same source.",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "shared",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "claude-code",
					SessionID:          "shared",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
			},
		},
		{
			name: "temporally overlapping conversations are ineligible",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "before",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(19, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "after",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
			},
			want: ReworkComparison{
				Status: "invalid",
				Code:   "baseline_ineligible",
				Reason: "The baseline ends after the current conversation starts.",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(10, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Unix(19, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
			},
		},
		{
			name: "reversed baseline interval is invalid",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(10, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "before",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "after",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
			},
			want: ReworkComparison{
				Status: "invalid",
				Code:   "invalid_time",
				Reason: "A conversation has an invalid start or end time.",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(10, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
			},
		},
		{
			name: "missing current start time is invalid",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "before",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Time{},
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "after",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
			},
			want: ReworkComparison{
				Status: "invalid",
				Code:   "invalid_time",
				Reason: "A conversation has an invalid start or end time.",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(10, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Time{},
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
			},
		},
		{
			name: "out-of-range timestamp is invalid",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "before",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "after",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
			},
			want: ReworkComparison{
				Status: "invalid",
				Code:   "invalid_time",
				Reason: "A conversation has an invalid start or end time.",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(10, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
					ProjectionCoverage: "unknown",
				},
			},
		},
		{
			name: "analysis identity must match its canonical snapshot",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "before",
					},
					StartedAt: time.Unix(10, 0),
					EndedAt:   time.Unix(20, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "stale",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "after",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
			},
			want: ReworkComparison{
				Status: "invalid",
				Code:   "identity_mismatch",
				Reason: "The loaded analysis does not belong to its displayed conversation.",
				Baseline: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "before",
					StartedAt:          time.Unix(10, 0),
					EndedAt:            time.Unix(20, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
			},
		},
		{
			name: "empty source-qualified identity is invalid",
			args: args{
				baseline: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "",
						conversationID: "",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "",
						RunID:         "",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
				current: ReworkDiagnosticSnapshot{
					Identity: ConversationIdentity{
						sourceID:       "codex",
						conversationID: "after",
					},
					StartedAt: time.Unix(20, 0),
					EndedAt:   time.Unix(30, 0),
					Analysis: SessionRework{
						SourceID:      "codex",
						RunID:         "after",
						SessionTokens: canonical.TokenUsage{},
						Report:        ReworkReport{},
					},
				},
			},
			want: ReworkComparison{
				Status: "invalid",
				Code:   "identity_mismatch",
				Reason: "The loaded analysis does not belong to its displayed conversation.",
				Baseline: ReworkComparisonSummary{
					SourceID:           "",
					SessionID:          "",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
				Current: ReworkComparisonSummary{
					SourceID:           "codex",
					SessionID:          "after",
					StartedAt:          time.Unix(20, 0),
					EndedAt:            time.Unix(30, 0),
					ProjectionCoverage: "unknown",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareReworkSnapshots(tt.args.baseline, tt.args.current)
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(unreportedHarnessContext{}), cmpopts.EquateApprox(1e-12, 1e-12)); diff != "" {
				t.Errorf("CompareReworkSnapshots() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
