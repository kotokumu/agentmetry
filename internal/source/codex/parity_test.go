package codex_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/kotokumu/agentmetry/internal/canonical"
	adapter "github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/source/codex"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
	source "github.com/kotokumu/agentmetry/sourceplugin"
)

func TestCodexNativeUsageBuildsOneObservedDashboardTotal(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	normalizer := adapter.NewNormalizer(source.NewRegistry(codex.New()))

	logs, err := normalizer.NormalizeLogs(codexUsageLogs(now))
	if err != nil {
		t.Fatal(err)
	}
	traces, err := normalizer.NormalizeTraces(codexUsageTraces(now))
	if err != nil {
		t.Fatal(err)
	}

	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.CommitBatch(context.Background(), logs); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(context.Background(), traces); err != nil {
		t.Fatal(err)
	}

	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 1 {
		t.Fatalf("sessions = %#v", overview.Sessions)
	}
	want := canonical.TokenUsage{Input: 32161, Output: 47, CacheRead: 1920, Reasoning: 41}
	if overview.Tokens != want {
		t.Fatalf("token usage = %#v, want %#v", overview.Tokens, want)
	}
	if len(overview.Sessions[0].Activities) != 3 {
		// The prompt event is part of the conversation evidence as well.
		t.Fatalf("activities = %#v", overview.Sessions[0].Activities)
	}
	contributing := 0
	var prompt, usage, tracedUsage bool
	for _, activity := range overview.Sessions[0].Activities {
		if activity.Kind == canonical.ActivityPrompt {
			prompt = activity.PromptID == "turn-1" && activity.RelatedTraceID != ""
		}
		if activity.Name == "gen_ai.response.completed" {
			usage = activity.UsageID == "response-1" && activity.PromptID == "turn-1" && activity.RelatedTraceID != ""
		}
		if activity.Signal == canonical.SignalTrace {
			tracedUsage = activity.UsageID == "response-1" && activity.TraceID != ""
		}
		if activity.ContributesToTotal {
			contributing++
			if activity.Tokens != want {
				t.Fatalf("authoritative activity usage = %#v, want %#v", activity.Tokens, want)
			}
		}
	}
	if contributing != 1 {
		t.Fatalf("contributing activities = %d, want one authoritative call", contributing)
	}
	if !prompt || !usage || !tracedUsage {
		t.Fatalf("missing Codex prompt/usage correlation: %#v", overview.Sessions[0].Activities)
	}
}

func codexUsageLogs(now time.Time) plog.Logs {
	data := plog.NewLogs()
	resource := data.ResourceLogs().AppendEmpty()
	resource.Resource().Attributes().PutStr("service.name", "codex")
	resource.Resource().Attributes().PutStr("conversation.id", "thread-1")
	record := resource.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	prompt := resource.ScopeLogs().At(0).LogRecords().AppendEmpty()
	prompt.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(-time.Second)))
	prompt.SetEventName("codex.user_prompt")
	prompt.Attributes().PutStr("turn_id", "turn-1")
	prompt.Attributes().PutStr("prompt", "Review the repository")

	record = resource.ScopeLogs().At(0).LogRecords().AppendEmpty()
	record.SetTimestamp(pcommon.NewTimestampFromTime(now))
	record.SetEventName("codex.sse_event")
	record.Attributes().PutStr("event.kind", "response.completed")
	record.Attributes().PutStr("response.id", "response-1")
	record.Attributes().PutStr("turn_id", "turn-1")
	record.Attributes().PutInt("input_tokens", 32161)
	record.Attributes().PutInt("cached_input_tokens", 1920)
	record.Attributes().PutInt("output_tokens", 47)
	record.Attributes().PutInt("reasoning_output_tokens", 41)
	return data
}

func codexUsageTraces(now time.Time) ptrace.Traces {
	data := ptrace.NewTraces()
	resource := data.ResourceSpans().AppendEmpty()
	resource.Resource().Attributes().PutStr("service.name", "codex")
	resource.Resource().Attributes().PutStr("conversation.id", "thread-1")
	span := resource.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("codex.llm_request")
	span.SetTraceID(pcommon.TraceID{1})
	span.SetSpanID(pcommon.SpanID{2})
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(now.Add(-time.Second)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(now))
	span.Attributes().PutStr("response.id", "response-1")
	span.Attributes().PutStr("turn_id", "turn-1")
	span.Attributes().PutInt("input_tokens", 32161)
	span.Attributes().PutInt("cached_input_tokens", 1920)
	span.Attributes().PutInt("output_tokens", 47)
	span.Attributes().PutInt("reasoning_output_tokens", 41)
	return data
}
