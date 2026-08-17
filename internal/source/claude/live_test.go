//go:build providerlive

package claude_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/source/claude"
	source "github.com/kotokumu/agentmetry/sourceplugin"
)

func TestClaudeCLIUsageMatchesCanonicalProjection(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Fatal("providerlive requires ANTHROPIC_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "claude", "--bare", "--no-session-persistence", "--tools", "", "--output-format", "json", "--max-budget-usd", "0.05", "-p", "Reply with the single word: ok")
	command.Dir = t.TempDir()
	var stderr bytes.Buffer
	command.Stderr = &stderr
	payload, err := command.Output()
	if err != nil {
		t.Fatalf("claude CLI failed: %v\nstderr:\n%s", err, stderr.String())
	}
	providerUsage, err := claude.ParseCLIResult(payload)
	if err != nil {
		t.Fatalf("parse Claude usage: %v\nstdout:\n%s", err, payload)
	}

	attributes := map[string]any{
		"service.name":                "claude-code",
		"input_tokens":                providerUsage.InputTokens,
		"output_tokens":               providerUsage.OutputTokens,
		"cache_read_input_tokens":     providerUsage.CacheReadInputTokens,
		"cache_creation_input_tokens": providerUsage.CacheCreationInputTokens,
	}
	if providerUsage.TotalCostUSD != nil {
		attributes["cost_usd"] = *providerUsage.TotalCostUSD
	}
	actual := canonical.DeriveAgentContext(claude.New().Normalize(source.Event{
		Name:       "claude_code.api_request",
		Attributes: attributes,
	}).Attributes).Tokens
	want := canonical.TokenUsage{
		Input:      providerUsage.InputTokens + providerUsage.CacheReadInputTokens + providerUsage.CacheCreationInputTokens,
		Output:     providerUsage.OutputTokens,
		CacheRead:  providerUsage.CacheReadInputTokens,
		CacheWrite: providerUsage.CacheCreationInputTokens,
	}
	if diff := cmp.Diff(want, actual); diff != "" {
		t.Fatalf("Claude provider usage projection mismatch (-want +got):\n%s", diff)
	}
	if providerUsage.TotalCostUSD == nil {
		t.Fatal("Claude CLI did not report total_cost_usd")
	}
	if cost := canonical.DeriveCostUSD(attributes); cost == nil || *cost != *providerUsage.TotalCostUSD {
		t.Fatalf("Claude provider cost was not preserved: got=%v want=%v", canonical.DeriveCostUSD(attributes), *providerUsage.TotalCostUSD)
	}
}
