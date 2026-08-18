package canonical_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/kotokumu/agentmetry/internal/canonical"
)

func TestTokenUsageValidate(t *testing.T) {
	const maxInt64 = int64(^uint64(0) >> 1)

	tests := []struct {
		name    string
		usage   canonical.TokenUsage
		wantErr bool
	}{
		{
			name: "complete usage with breakdowns",
			usage: canonical.TokenUsage{
				Input: 12, Output: 8, CacheRead: 5, CacheWrite: 3, Reasoning: 7,
			},
		},
		{
			name: "reported zero values are valid",
			usage: canonical.TokenUsage{
				Presence: canonical.TokenPresence{
					Input: true, Output: true, CacheRead: true, CacheWrite: true, Reasoning: true,
				},
			},
		},
		{
			name:  "partial cache breakdown remains valid",
			usage: canonical.TokenUsage{Input: 10, CacheRead: 7},
		},
		{
			name:    "negative input is rejected",
			usage:   canonical.TokenUsage{Input: -1},
			wantErr: true,
		},
		{
			name:    "cache breakdown cannot exceed input",
			usage:   canonical.TokenUsage{Input: 10, CacheRead: 8, CacheWrite: 3},
			wantErr: true,
		},
		{
			name:    "reasoning breakdown cannot exceed output",
			usage:   canonical.TokenUsage{Output: 5, Reasoning: 6},
			wantErr: true,
		},
		{
			name:    "input and output total cannot overflow",
			usage:   canonical.TokenUsage{Input: maxInt64, Output: 1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.usage.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("TokenUsage.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(err, canonical.ErrInvalidTokenUsage) {
				t.Fatalf("TokenUsage.Validate() error = %v, want ErrInvalidTokenUsage", err)
			}
		})
	}
}

func TestTokenUsageJSONDistinguishesMissingFromReportedZero(t *testing.T) {
	missing, err := json.Marshal(canonical.TokenUsage{})
	if err != nil {
		t.Fatal(err)
	}
	var missingPayload map[string]any
	if err := json.Unmarshal(missing, &missingPayload); err != nil {
		t.Fatal(err)
	}
	if missingPayload["input"] != nil || missingPayload["total"] != nil {
		t.Fatalf("missing usage must be null: %s", missing)
	}

	reported := canonical.DeriveAgentContext(map[string]any{
		"gen_ai.usage.input_tokens":  int64(0),
		"gen_ai.usage.output_tokens": int64(0),
	}).Tokens
	reportedJSON, err := json.Marshal(reported)
	if err != nil {
		t.Fatal(err)
	}
	var reportedPayload map[string]any
	if err := json.Unmarshal(reportedJSON, &reportedPayload); err != nil {
		t.Fatal(err)
	}
	if reportedPayload["input"] != float64(0) || reportedPayload["output"] != float64(0) || reportedPayload["total"] != float64(0) {
		t.Fatalf("reported zero must remain numeric: %s", reportedJSON)
	}
}

func TestTokenUsageJSONRoundTripPreservesMissingAndReportedZero(t *testing.T) {
	before := canonical.TokenUsage{
		Output: 7,
		Presence: canonical.TokenPresence{
			CacheRead: true,
		},
	}
	payload, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	var after canonical.TokenUsage
	if err := json.Unmarshal(payload, &after); err != nil {
		t.Fatal(err)
	}
	if after.InputReported() || !after.OutputReported() || !after.CacheReadReported() {
		t.Fatalf("presence was not preserved: %#v", after)
	}
	if after.Output != 7 || after.CacheRead != 0 || after.TotalReported() {
		t.Fatalf("values were not preserved: %#v", after)
	}
}

func TestTokenUsageAggregateDoesNotClaimACompleteTotalWhenOneCallIsPartial(t *testing.T) {
	var aggregate canonical.TokenUsage
	aggregate.Add(canonical.TokenUsage{Output: 3})
	aggregate.Add(canonical.TokenUsage{Input: 10, Output: 2})

	payload, err := json.Marshal(aggregate)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(payload, &encoded); err != nil {
		t.Fatal(err)
	}
	if encoded["input"] != float64(10) || encoded["output"] != float64(5) || encoded["total"] != nil {
		t.Fatalf("partial aggregate was presented as complete: %s", payload)
	}
}

func TestDeriveAgentContextRecognizesTypedUsageWithoutMutatingAttributes(t *testing.T) {
	attributes := map[string]any{
		"gen_ai.agent.id":                "reviewer",
		"gen_ai.conversation.id":         "run-42",
		"gen_ai.request.model":           "example-model",
		"gen_ai.usage.input_tokens":      int64(120),
		"gen_ai.usage.output_tokens":     "30",
		"gen_ai.usage.cache_read_tokens": float64(8),
		"gen_ai.usage.reasoning_tokens":  int64(-1),
	}

	context := canonical.DeriveAgentContext(attributes)

	if context.AgentID != "reviewer" || context.RunID != "run-42" || context.Model != "example-model" {
		t.Fatalf("unexpected identity: %#v", context)
	}
	if context.Tokens.Input != 120 || context.Tokens.Output != 30 || context.Tokens.CacheRead != 8 {
		t.Fatalf("unexpected token usage: %#v", context.Tokens)
	}
	if context.Tokens.Reasoning != 0 {
		t.Fatalf("negative usage must not contribute: %#v", context.Tokens)
	}
	if attributes["gen_ai.usage.output_tokens"] != "30" {
		t.Fatal("derivation mutated its input")
	}
}

func TestDeriveAgentContextDoesNotUseAgentNameAsRuntimeID(t *testing.T) {
	context := canonical.DeriveAgentContext(map[string]any{
		"agent.name":              "Explore",
		"gen_ai.agent.definition": "Explore",
		"gen_ai.agent.type":       "Explore",
	})

	if context.AgentID != "" {
		t.Fatalf("AgentID = %q, want empty runtime identity", context.AgentID)
	}
	if context.AgentDefinition != "Explore" || context.AgentType != "Explore" {
		t.Fatalf("descriptive agent metadata was not retained: %#v", context)
	}
}

func TestDeriveCostUSDIgnoresCorroboratingUsageEvidence(t *testing.T) {
	cost := canonical.DeriveCostUSD(map[string]any{
		"gen_ai.usage.role":     "corroborating",
		"gen_ai.usage.cost_usd": 1.25,
	})
	if cost != nil {
		t.Fatalf("corroborating cost = %v, want absent canonical contribution", *cost)
	}
}

func TestTokenUsageTotalDoesNotDoubleCountCacheOrReasoningBreakdowns(t *testing.T) {
	usage := canonical.TokenUsage{
		Input:      10,
		Output:     5,
		CacheRead:  4,
		CacheWrite: 3,
		Reasoning:  2,
	}

	if got, want := usage.Total(), int64(15); got != want {
		t.Fatalf("Total() = %d, want %d", got, want)
	}
}

func TestDeriveAgentContextRecognizesDottedUsageKeys(t *testing.T) {
	attributes := map[string]any{
		"gen_ai.usage.input_tokens":             int64(100),
		"gen_ai.usage.output_tokens":            int64(20),
		"gen_ai.usage.cache_read.input_tokens":  int64(70),
		"gen_ai.usage.cache_write.input_tokens": int64(4),
		"gen_ai.usage.reasoning_tokens":         int64(12),
	}

	usage := canonical.DeriveAgentContext(attributes).Tokens

	if usage.CacheRead != 70 || usage.CacheWrite != 4 || usage.Reasoning != 12 || usage.Total() != 120 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}
