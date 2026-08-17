package codex_test

import (
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/source/codex"
)

func TestPlanUsageParserReadsAppServerRateLimits(t *testing.T) {
	capturedAt := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	snapshots, err := codex.NewPlanUsageParser().Parse([]byte(`{
  "result": {
    "planType": "pro",
    "rateLimits": {
      "primary": {"usedPercent": 4, "windowDurationMins": 10080, "resetsAt": 1786894800},
      "secondary": {"usedPercent": 12, "windowDurationMins": 300, "resetsAt": 1786462800}
    }
  }
}`), capturedAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].Plan != "pro" || snapshots[0].Authority != "account_api" {
		t.Fatalf("snapshots = %#v", snapshots)
	}
}

func TestPlanUsageParserRejectsAWindowWithoutUsagePercentage(t *testing.T) {
	_, err := codex.NewPlanUsageParser().Parse([]byte(`{
  "result": {"rateLimits": {"primary": {"windowDurationMins": 300}}}
}`), time.Now())
	if err == nil {
		t.Fatal("missing usedPercent was accepted as zero")
	}
}
