//go:build providerlive

package codex_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/source/codex"
	source "github.com/theoden9014/agentmetry/sourceplugin"
)

func TestCodexCLIUsageMatchesCanonicalProjection(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Fatal("providerlive requires OPENAI_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "codex", "exec", "--json", "--ephemeral", "--ignore-user-config", "--sandbox", "read-only", "--skip-git-repo-check", "Reply with the single word: ok")
	command.Dir = t.TempDir()
	var stderr bytes.Buffer
	command.Stderr = &stderr
	payload, err := command.Output()
	if err != nil {
		t.Fatalf("Codex CLI failed: %v\nstderr:\n%s", err, stderr.String())
	}
	providerUsage, err := codex.ParseExecJSON(strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("parse Codex usage: %v\nstdout:\n%s", err, payload)
	}

	actual := canonical.DeriveAgentContext(codex.New().Normalize(source.Event{
		Name: "codex.sse_event",
		Attributes: map[string]any{
			"event.kind":              "response.completed",
			"input_tokens":            providerUsage.InputTokens,
			"cached_input_tokens":     providerUsage.CachedInputTokens,
			"output_tokens":           providerUsage.OutputTokens,
			"reasoning_output_tokens": providerUsage.ReasoningOutputTokens,
		},
	}).Attributes).Tokens
	want := canonical.TokenUsage{
		Input:     providerUsage.InputTokens,
		Output:    providerUsage.OutputTokens,
		CacheRead: providerUsage.CachedInputTokens,
		Reasoning: providerUsage.ReasoningOutputTokens,
	}
	if diff := cmp.Diff(want, actual); diff != "" {
		t.Fatalf("Codex provider usage projection mismatch (-want +got):\n%s", diff)
	}
	if actual.Input < actual.CacheRead {
		t.Fatalf("Codex cached input exceeds total input: %#v", actual)
	}
	if actual.Reasoning > actual.Output {
		t.Fatalf("Codex reasoning output exceeds output: %#v", actual)
	}
}
