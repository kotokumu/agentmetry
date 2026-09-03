package connectapi

import (
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/internal/query"
)

func sessionConditions(input *v1.SessionConditions) query.SessionConditions {
	if input == nil {
		return query.SessionConditions{}
	}
	return query.SessionConditions{ObservedFailure: input.GetObservedFailure(), MinDurationMS: input.MinDurationMs,
		MaxDurationMS: input.MaxDurationMs, Model: input.GetModel(), Tool: input.GetTool()}
}

func mapSessionConditions(input *query.SessionConditions) *v1.SessionConditions {
	if input == nil {
		return nil
	}
	return &v1.SessionConditions{ObservedFailure: input.ObservedFailure, MinDurationMs: input.MinDurationMS,
		MaxDurationMs: input.MaxDurationMS, Model: input.Model, Tool: input.Tool}
}
