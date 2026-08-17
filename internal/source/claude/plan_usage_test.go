package claude_test

import (
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/source/claude"
)

func TestPlanUsageParserReadsStatusLineWindows(t *testing.T) {
	capturedAt := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	snapshots, err := claude.NewPlanUsageParser().Parse([]byte(`{
  "rate_limits": {
    "five_hour": {"used_percentage": 25.5, "resets_at": 1786462800},
    "seven_day": {"used_percentage": 70, "resets_at": 1786894800}
  }
}`), capturedAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].WindowDurationMinutes != 300 || snapshots[1].WindowDurationMinutes != 10080 {
		t.Fatalf("snapshots = %#v", snapshots)
	}
	if snapshots[0].UsedPercent != 25.5 || snapshots[0].Authority != "status_line" || snapshots[0].ResetsAt == nil {
		t.Fatalf("five-hour snapshot = %#v", snapshots[0])
	}
}

func TestPlanUsageParserRejectsAWindowWithoutUsagePercentage(t *testing.T) {
	_, err := claude.NewPlanUsageParser().Parse([]byte(`{
  "rate_limits": {"five_hour": {"resets_at": 1786462800}}
}`), time.Now())
	if err == nil {
		t.Fatal("missing used_percentage was accepted as zero")
	}
}
