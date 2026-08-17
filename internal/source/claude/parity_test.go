package claude_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/kotokumu/agentmetry/internal/canonical"
	adapter "github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/source/claude"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
	source "github.com/kotokumu/agentmetry/sourceplugin"
)

func TestClaudeTelemetryBuildsDashboardParityProjection(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	normalizer := adapter.NewNormalizer(source.NewRegistry(claude.New()))

	logs, err := normalizer.NormalizeLogs(claudeLogs(now))
	if err != nil {
		t.Fatal(err)
	}
	traces, err := normalizer.NormalizeTraces(claudeTraces(now))
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := normalizer.NormalizeMetrics(claudeMetrics(now))
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
	if err := database.CommitBatch(context.Background(), metrics); err != nil {
		t.Fatal(err)
	}

	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 1 || overview.Sessions[0].ID != "claude-session" {
		t.Fatalf("sessions = %#v", overview.Sessions)
	}
	if overview.Tokens != (canonical.TokenUsage{Input: 174, Output: 20, CacheRead: 70, CacheWrite: 4}) {
		t.Fatalf("token usage = %#v", overview.Tokens)
	}
	if overview.SignalCounts != (query.SignalCounts{Traces: 2, Logs: 3, Metrics: 4}) {
		t.Fatalf("signal counts = %#v", overview.SignalCounts)
	}

	var prompt, request, tracedRequest, response, tool bool
	for _, activity := range overview.Sessions[0].Activities {
		switch {
		case activity.Name == "gen_ai.user_prompt":
			prompt = activity.Kind == canonical.ActivityPrompt && activity.Content == "Inspect the repository" && activity.PromptID == "prompt-1" && activity.RelatedTraceID != ""
		case activity.Name == "gen_ai.model.request":
			request = activity.Kind == canonical.ActivityResponse && activity.Model == "claude-example" && activity.Tokens.Input == 174 && activity.PromptID == "prompt-1" && activity.UsageID == "client-request-1" && activity.RelatedTraceID != ""
		case activity.Name == "gen_ai.model.request.trace":
			tracedRequest = activity.Kind == canonical.ActivityResponse && activity.AgentID == "agent-7" && activity.TraceID != "" && activity.UsageID == "client-request-1" && activity.Tokens.Total() == 0
		case activity.Name == "gen_ai.response.completed":
			response = activity.Kind == canonical.ActivityResponse && activity.Content == "Inspection complete"
		case activity.Name == "gen_ai.tool":
			tool = activity.Kind == canonical.ActivityTool && activity.AgentID == "agent-7" && activity.AgentType == "Explore" && activity.ToolName == "Read"
		}
	}
	if !prompt || !request || !tracedRequest || !response || !tool {
		t.Fatalf("missing Claude dashboard activities: %#v", overview.Sessions[0].Activities)
	}
}

func claudeLogs(now time.Time) plog.Logs {
	data := plog.NewLogs()
	resource := data.ResourceLogs().AppendEmpty()
	resource.Resource().Attributes().PutStr("service.name", "claude-code")
	resource.Resource().Attributes().PutStr("session.id", "claude-session")
	records := resource.ScopeLogs().AppendEmpty().LogRecords()

	prompt := records.AppendEmpty()
	prompt.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(-4 * time.Second)))
	prompt.SetEventName("claude_code.user_prompt")
	prompt.Attributes().PutStr("prompt.id", "prompt-1")
	prompt.Attributes().PutStr("prompt", "Inspect the repository")

	request := records.AppendEmpty()
	request.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(-3 * time.Second)))
	request.SetEventName("claude_code.api_request")
	request.Attributes().PutStr("prompt.id", "prompt-1")
	request.Attributes().PutStr("client_request_id", "client-request-1")
	request.Attributes().PutStr("request_id", "request-1")
	request.Attributes().PutStr("model", "claude-example")
	request.Attributes().PutInt("input_tokens", 100)
	request.Attributes().PutInt("output_tokens", 20)
	request.Attributes().PutInt("cache_read_tokens", 70)
	request.Attributes().PutInt("cache_creation_tokens", 4)
	request.Attributes().PutInt("cost_usd_micros", 12500)

	response := records.AppendEmpty()
	response.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(-2 * time.Second)))
	response.SetEventName("claude_code.assistant_response")
	response.Attributes().PutStr("prompt.id", "prompt-1")
	response.Attributes().PutStr("request_id", "request-1")
	response.Attributes().PutStr("model", "claude-example")
	response.Attributes().PutStr("response", "Inspection complete")

	return data
}

func claudeTraces(now time.Time) ptrace.Traces {
	data := ptrace.NewTraces()
	resource := data.ResourceSpans().AppendEmpty()
	resource.Resource().Attributes().PutStr("service.name", "claude-code")
	resource.Resource().Attributes().PutStr("session.id", "claude-session")
	spans := resource.ScopeSpans().AppendEmpty().Spans()
	request := spans.AppendEmpty()
	request.SetName("claude_code.llm_request")
	request.SetTraceID(pcommon.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	request.SetSpanID(pcommon.SpanID{8, 7, 6, 5, 4, 3, 2, 1})
	request.SetStartTimestamp(pcommon.NewTimestampFromTime(now.Add(-2 * time.Second)))
	request.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(-time.Second)))
	request.Attributes().PutStr("agent_id", "agent-7")
	request.Attributes().PutStr("parent_agent_id", "agent-1")
	request.Attributes().PutStr("agent.name", "Explore")
	request.Attributes().PutStr("gen_ai.request.model", "claude-example")
	request.Attributes().PutStr("client_request_id", "client-request-1")
	request.Attributes().PutStr("prompt.id", "prompt-1")
	request.Attributes().PutInt("input_tokens", 100)
	request.Attributes().PutInt("output_tokens", 20)

	span := spans.AppendEmpty()
	span.SetName("claude_code.tool")
	span.SetTraceID(pcommon.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	span.SetSpanID(pcommon.SpanID{1, 2, 3, 4, 5, 6, 7, 8})
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(now.Add(-time.Second)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(now))
	span.Attributes().PutStr("agent_id", "agent-7")
	span.Attributes().PutStr("parent_agent_id", "agent-1")
	span.Attributes().PutStr("subagent_type", "Explore")
	span.Attributes().PutStr("tool_name", "Read")
	span.Attributes().PutStr("tool_use_id", "tool-1")
	span.Attributes().PutStr("file_path", "main.go")
	return data
}

func claudeMetrics(now time.Time) pmetric.Metrics {
	data := pmetric.NewMetrics()
	resource := data.ResourceMetrics().AppendEmpty()
	resource.Resource().Attributes().PutStr("service.name", "claude-code")
	resource.Resource().Attributes().PutStr("session.id", "claude-session")
	metrics := resource.ScopeMetrics().AppendEmpty().Metrics()
	for _, usage := range []struct {
		kind  string
		value int64
	}{
		{kind: "input", value: 100},
		{kind: "output", value: 20},
		{kind: "cacheRead", value: 70},
		{kind: "cacheCreation", value: 4},
	} {
		metric := metrics.AppendEmpty()
		metric.SetName("claude_code.token.usage")
		point := metric.SetEmptySum().DataPoints().AppendEmpty()
		point.SetTimestamp(pcommon.NewTimestampFromTime(now))
		point.SetIntValue(usage.value)
		point.Attributes().PutStr("type", usage.kind)
		point.Attributes().PutStr("model", "claude-example")
		point.Attributes().PutStr("query_source", "subagent")
	}
	return data
}
