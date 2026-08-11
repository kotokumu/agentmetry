package codex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/theoden9014/agentmetry/internal/planusage"
)

type PlanUsageParser struct{}

func NewPlanUsageParser() PlanUsageParser { return PlanUsageParser{} }
func (PlanUsageParser) ID() string        { return "codex" }

func (PlanUsageParser) Parse(payload []byte, capturedAt time.Time) ([]planusage.Snapshot, error) {
	var envelope struct {
		Result appServerResult `json:"result"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("decode app server plan usage: %w", err)
	}
	result := envelope.Result
	windows := map[string]*appServerWindow{
		"primary":   result.RateLimits.Primary,
		"secondary": result.RateLimits.Secondary,
	}
	for id, window := range result.RateLimitsByLimitID {
		windows[id] = window
	}
	ids := make([]string, 0, len(windows))
	for id, window := range windows {
		if window != nil {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := windows[ids[i]], windows[ids[j]]
		if left.WindowDurationMinutes == right.WindowDurationMinutes {
			return ids[i] < ids[j]
		}
		return left.WindowDurationMinutes < right.WindowDurationMinutes
	})
	var snapshots []planusage.Snapshot
	for _, id := range ids {
		window := windows[id]
		if window.UsedPercent == nil {
			return nil, fmt.Errorf("plan usage window %q is missing usedPercent", id)
		}
		reset, err := parseAppServerReset(window.ResetsAt)
		if err != nil {
			return nil, err
		}
		snapshot := planusage.Snapshot{
			Source: "codex", Plan: result.PlanType, WindowID: id,
			WindowDurationMinutes: window.WindowDurationMinutes, UsedPercent: *window.UsedPercent,
			ResetsAt: reset, CapturedAt: capturedAt.UTC(), Authority: "account_api",
			Raw: append([]byte(nil), payload...),
		}
		if err := snapshot.Validate(); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	if len(snapshots) == 0 {
		return nil, fmt.Errorf("app server response does not contain rate-limit windows")
	}
	return snapshots, nil
}

type appServerResult struct {
	PlanType   string `json:"planType"`
	RateLimits struct {
		Primary   *appServerWindow `json:"primary"`
		Secondary *appServerWindow `json:"secondary"`
	} `json:"rateLimits"`
	RateLimitsByLimitID map[string]*appServerWindow `json:"rateLimitsByLimitId"`
}

type appServerWindow struct {
	UsedPercent           *float64        `json:"usedPercent"`
	WindowDurationMinutes int64           `json:"windowDurationMins"`
	ResetsAt              json.RawMessage `json:"resetsAt"`
}

func parseAppServerReset(raw json.RawMessage) (*time.Time, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		seconds, err := strconv.ParseInt(number.String(), 10, 64)
		if err == nil {
			value := time.Unix(seconds, 0).UTC()
			return &value, nil
		}
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		value, err := time.Parse(time.RFC3339, text)
		if err == nil {
			return &value, nil
		}
	}
	return nil, fmt.Errorf("unsupported app server reset time %s", raw)
}
