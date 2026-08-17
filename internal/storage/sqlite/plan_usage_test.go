package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/planusage"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
)

func TestPlanUsageKeepsHistoryAndReturnsLatestWindowSnapshot(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	base := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	for _, snapshot := range []planusage.Snapshot{
		{Source: "example", AccountID: "account-1", WindowID: "weekly", WindowDurationMinutes: 10080, UsedPercent: 10, CapturedAt: base, Authority: "account_api"},
		{Source: "example", AccountID: "account-1", WindowID: "weekly", WindowDurationMinutes: 10080, UsedPercent: 25, CapturedAt: base.Add(time.Minute), Authority: "account_api"},
		{Source: "example", AccountID: "account-1", WindowID: "five-hour", WindowDurationMinutes: 300, UsedPercent: 50, CapturedAt: base, Authority: "account_api"},
	} {
		if err := database.PutPlanUsage(context.Background(), snapshot); err != nil {
			t.Fatal(err)
		}
	}
	latest, err := database.LatestPlanUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(latest) != 2 || latest[0].UsedPercent == 10 || latest[1].UsedPercent == 10 {
		t.Fatalf("latest snapshots = %#v", latest)
	}
}
