package query

import (
	"fmt"
	"math"
)

type SessionConditions struct {
	ObservedFailure bool     `json:"observedFailure,omitempty"`
	MinDurationMS   *float64 `json:"minDurationMs,omitempty"`
	MaxDurationMS   *float64 `json:"maxDurationMs,omitempty"`
	Model           string   `json:"model,omitempty"`
	Tool            string   `json:"tool,omitempty"`
}

func (conditions SessionConditions) Empty() bool {
	return !conditions.ObservedFailure && conditions.MinDurationMS == nil && conditions.MaxDurationMS == nil && conditions.Model == "" && conditions.Tool == ""
}

func ValidateSessionConditions(conditions SessionConditions) error {
	for name, value := range map[string]*float64{"minDurationMs": conditions.MinDurationMS, "maxDurationMs": conditions.MaxDurationMS} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return fmt.Errorf("%s must be finite and nonnegative", name)
		}
	}
	if conditions.MinDurationMS != nil && conditions.MaxDurationMS != nil && *conditions.MinDurationMS > *conditions.MaxDurationMS {
		return fmt.Errorf("minDurationMs must not exceed maxDurationMs")
	}
	if len(conditions.Model) > 200 || len(conditions.Tool) > 200 {
		return fmt.Errorf("model and tool must not exceed 200 bytes")
	}
	return nil
}

// ActivityHasObservedFailure shares diagnostic outcome interpretation. Missing
// outcome evidence is not failure.
func ActivityHasObservedFailure(activity Activity) bool {
	success := observedSuccess(activity)
	return success != nil && !*success
}
