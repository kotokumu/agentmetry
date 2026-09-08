package billing_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/kotokumu/agentmetry/internal/billing"
)

func TestPlanRateManifestUpdateBuildsCompleteAppendOnlyChain(t *testing.T) {
	base := manifestRate("2026-01-01T00:00:00Z", 1)
	middle := manifestRate("2026-02-01T00:00:00Z", 2)
	latest := manifestRate("2026-03-01T00:00:00Z", 3)
	evaluatedAt := mustManifestTime(t, "2026-04-01T00:00:00Z")

	plan, err := billing.PlanRateManifestUpdate([]billing.Rate{base}, []billing.Rate{latest, middle}, evaluatedAt)
	if err != nil {
		t.Fatal(err)
	}
	wantClosures := []billing.RateClosure{
		{RateID: base.ID(), EffectiveTo: middle.EffectiveFrom},
	}
	if diff := cmp.Diff(wantClosures, plan.Closures); diff != "" {
		t.Fatalf("closures mismatch (-want +got):\n%s", diff)
	}
	middle = withAppliedAt(middle, evaluatedAt)
	middle.EffectiveTo = &latest.EffectiveFrom
	if diff := cmp.Diff([]billing.Rate{middle, withAppliedAt(latest, evaluatedAt)}, plan.Inserts); diff != "" {
		t.Fatalf("inserts mismatch (-want +got):\n%s", diff)
	}
}

func TestPlanRateManifestUpdateIsIdempotent(t *testing.T) {
	rate := manifestRate("2026-01-01T00:00:00Z", 1)
	stored := withAppliedAt(rate, mustManifestTime(t, "2026-01-03T00:00:00Z"))
	plan, err := billing.PlanRateManifestUpdate([]billing.Rate{stored}, []billing.Rate{rate}, mustManifestTime(t, "2026-02-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Changed() {
		t.Fatalf("idempotent manifest produced changes: %#v", plan)
	}
}

func TestPlanRateManifestUpdateRejectsHistoryRewriteAndUntrustedEvidence(t *testing.T) {
	existing := manifestRate("2026-02-01T00:00:00Z", 2)
	evaluatedAt := mustManifestTime(t, "2026-04-01T00:00:00Z")
	for _, test := range []struct {
		name string
		rate billing.Rate
		want string
	}{
		{name: "history rewrite", rate: manifestRate("2026-01-01T00:00:00Z", 1), want: "historical model rate insertion"},
		{name: "future evidence", rate: withRetrievedAt(manifestRate("2026-03-01T00:00:00Z", 3), evaluatedAt.Add(time.Second)), want: "evidence time is invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := billing.PlanRateManifestUpdate([]billing.Rate{existing}, []billing.Rate{test.rate}, evaluatedAt)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func manifestRate(effective string, input int64) billing.Rate {
	parsed, err := time.Parse(time.RFC3339Nano, effective)
	if err != nil {
		panic(err)
	}
	return billing.Rate{
		Provider:      "provider",
		Model:         "model",
		Mode:          "standard",
		EffectiveFrom: parsed,
		Pricing:       billing.Pricing{InputMicroUSDPerMillion: input},
		SourceURL:     "https://example.test/pricing",
		EvidenceID:    "evidence",
		RetrievedAt:   parsed,
	}
}

func withAppliedAt(rate billing.Rate, appliedAt time.Time) billing.Rate {
	rate.AppliedAt = appliedAt
	return rate
}

func withRetrievedAt(rate billing.Rate, retrievedAt time.Time) billing.Rate {
	rate.RetrievedAt = retrievedAt
	return rate
}

func mustManifestTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
