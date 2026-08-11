package planusage_test

import (
	"testing"
	"time"

	"github.com/theoden9014/agentmetry/internal/planusage"
)

func TestSnapshotValidation(t *testing.T) {
	valid := planusage.Snapshot{
		Source: "example", WindowID: "weekly", WindowDurationMinutes: 10080,
		UsedPercent: 25, CapturedAt: time.Now(), Authority: "account_api",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid snapshot was rejected: %v", err)
	}
	invalid := valid
	invalid.UsedPercent = 101
	if err := invalid.Validate(); err == nil {
		t.Fatal("percentage above 100 was accepted")
	}
}
