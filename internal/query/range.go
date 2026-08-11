package query

import (
	"fmt"
	"time"
)

func FilterForRange(now time.Time, value string) (OverviewFilter, error) {
	var duration time.Duration
	switch value {
	case "", "24h":
		duration = 24 * time.Hour
	case "1h":
		duration = time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	default:
		return OverviewFilter{}, fmt.Errorf("unsupported range %q", value)
	}
	return OverviewFilter{
		Since:         now.UTC().Add(-duration),
		ActivityLimit: 100,
	}, nil
}
