// Package planusage models authoritative subscription and rate-limit windows.
// It is intentionally separate from OTLP model traffic measurements.
package planusage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type Snapshot struct {
	Source                string          `json:"source"`
	AccountID             string          `json:"accountId,omitempty"`
	Plan                  string          `json:"plan,omitempty"`
	WindowID              string          `json:"windowId"`
	WindowDurationMinutes int64           `json:"windowDurationMinutes,omitempty"`
	UsedPercent           float64         `json:"usedPercent"`
	ResetsAt              *time.Time      `json:"resetsAt,omitempty"`
	CapturedAt            time.Time       `json:"capturedAt"`
	Authority             string          `json:"authority"`
	Raw                   json.RawMessage `json:"-"`
}

func (snapshot Snapshot) Validate() error {
	if snapshot.Source == "" {
		return fmt.Errorf("plan usage source is required")
	}
	if snapshot.WindowID == "" {
		return fmt.Errorf("plan usage window is required")
	}
	if snapshot.UsedPercent < 0 || snapshot.UsedPercent > 100 {
		return fmt.Errorf("plan usage percentage must be between 0 and 100")
	}
	if snapshot.Authority == "" {
		return fmt.Errorf("plan usage authority is required")
	}
	if snapshot.CapturedAt.IsZero() {
		return fmt.Errorf("plan usage capture time is required")
	}
	return nil
}

type Writer interface {
	PutPlanUsage(context.Context, Snapshot) error
}

type Reader interface {
	LatestPlanUsage(context.Context) ([]Snapshot, error)
}

type Parser interface {
	ID() string
	Parse([]byte, time.Time) ([]Snapshot, error)
}
