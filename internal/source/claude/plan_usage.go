package claude

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/theoden9014/agentmetry/internal/planusage"
)

type PlanUsageParser struct{}

func NewPlanUsageParser() PlanUsageParser { return PlanUsageParser{} }
func (PlanUsageParser) ID() string        { return "claude" }

func (PlanUsageParser) Parse(payload []byte, capturedAt time.Time) ([]planusage.Snapshot, error) {
	var input struct {
		RateLimits struct {
			FiveHour *statusLineWindow `json:"five_hour"`
			SevenDay *statusLineWindow `json:"seven_day"`
		} `json:"rate_limits"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		return nil, fmt.Errorf("decode status line plan usage: %w", err)
	}
	definitions := []struct {
		id      string
		minutes int64
		window  *statusLineWindow
	}{
		{id: "five_hour", minutes: 300, window: input.RateLimits.FiveHour},
		{id: "seven_day", minutes: 10080, window: input.RateLimits.SevenDay},
	}
	var snapshots []planusage.Snapshot
	for _, definition := range definitions {
		if definition.window == nil {
			continue
		}
		if definition.window.UsedPercent == nil {
			return nil, fmt.Errorf("plan usage window %q is missing used_percentage", definition.id)
		}
		reset, err := parseResetTime(definition.window.ResetsAt)
		if err != nil {
			return nil, err
		}
		snapshot := planusage.Snapshot{
			Source: "claude", WindowID: definition.id, WindowDurationMinutes: definition.minutes,
			UsedPercent: *definition.window.UsedPercent, ResetsAt: reset,
			CapturedAt: capturedAt.UTC(), Authority: "status_line", Raw: append([]byte(nil), payload...),
		}
		if err := snapshot.Validate(); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	if len(snapshots) == 0 {
		return nil, fmt.Errorf("status line payload does not contain plan usage windows")
	}
	return snapshots, nil
}

type statusLineWindow struct {
	UsedPercent *float64        `json:"used_percentage"`
	ResetsAt    json.RawMessage `json:"resets_at"`
}

func parseResetTime(raw json.RawMessage) (*time.Time, error) {
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
		if seconds, err := strconv.ParseInt(text, 10, 64); err == nil {
			value := time.Unix(seconds, 0).UTC()
			return &value, nil
		}
		value, err := time.Parse(time.RFC3339, text)
		if err == nil {
			return &value, nil
		}
	}
	return nil, fmt.Errorf("unsupported status line reset time %s", raw)
}
