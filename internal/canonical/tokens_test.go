package canonical_test

import (
	"encoding/json"
	"testing"

	"github.com/theoden9014/agentmetry/internal/canonical"
)

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
