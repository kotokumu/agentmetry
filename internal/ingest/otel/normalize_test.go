package otel_test

import (
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/kotokumu/agentmetry/internal/canonical"
	adapter "github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/source/codex"
	source "github.com/kotokumu/agentmetry/sourceplugin"
)

type prefixSourcePlugin struct {
	id     string
	prefix string
}

func (plugin prefixSourcePlugin) ID() string { return plugin.id }

func (plugin prefixSourcePlugin) Match(event source.Event) bool {
	return strings.HasPrefix(event.Name, plugin.prefix)
}

func (plugin prefixSourcePlugin) Normalize(event source.Event) source.Event {
	event.Name = "gen_ai.telemetry.event"
	return event
}

func TestNormalizeLogsProfilesMixedSourcesPerRecord(t *testing.T) {
	logs := plog.NewLogs()
	first := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	first.SetEventName("alpha.event")
	second := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	second.SetEventName("beta.event")
	normalizer := adapter.NewNormalizer(source.NewRegistry(
		prefixSourcePlugin{id: "alpha", prefix: "alpha."},
		prefixSourcePlugin{id: "beta", prefix: "beta."},
	))

	batch, err := normalizer.NormalizeLogs(logs)
	if err != nil {
		t.Fatal(err)
	}

	if len(batch.Logs) != 2 || batch.Logs[0].Source != "alpha" || batch.Logs[1].Source != "beta" {
		t.Fatalf("mixed source profiling failed: %#v", batch.Logs)
	}
}

func TestNormalizeTracesMergesResourceAndSpanAttributes(t *testing.T) {
	traces := ptrace.NewTraces()
	resourceSpans := traces.ResourceSpans().AppendEmpty()
	resourceSpans.Resource().Attributes().PutStr("gen_ai.conversation.id", "session-1")
	resourceSpans.Resource().Attributes().PutStr("gen_ai.agent.type", "explorer")
	span := resourceSpans.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetTraceID(pcommon.TraceID{1})
	span.SetSpanID(pcommon.SpanID{2})
	span.SetName("gen_ai.tool.call")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(time.Unix(10, 0)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(time.Unix(11, 0)))
	span.Attributes().PutStr("gen_ai.agent.id", "agent-7")
	span.Attributes().PutStr("gen_ai.request.model", "gpt-example")
	span.Attributes().PutInt("gen_ai.usage.input_tokens", 42)

	batch, err := adapter.NewNormalizer(source.NewRegistry()).NormalizeTraces(traces)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Signal != canonical.SignalTrace || len(batch.Spans) != 1 {
		t.Fatalf("unexpected batch: %#v", batch)
	}
	got := batch.Spans[0]
	if got.TraceID != "01000000000000000000000000000000" || got.SpanID != "0200000000000000" {
		t.Fatalf("unexpected trace identity: %#v", got)
	}
	if got.Agent.RunID != "session-1" || got.Agent.AgentID != "agent-7" || got.Agent.AgentType != "explorer" {
		t.Fatalf("unexpected agent context: %#v", got.Agent)
	}
	if got.Agent.Tokens.Input != 42 {
		t.Fatalf("unexpected tokens: %#v", got.Agent.Tokens)
	}
}

func TestNormalizeLogsUsesEventNameAndBody(t *testing.T) {
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	resourceLogs.Resource().Attributes().PutStr("gen_ai.conversation.id", "session-1")
	record := resourceLogs.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	record.SetEventName("tool.operation")
	record.SetTimestamp(pcommon.NewTimestampFromTime(time.Unix(20, 0)))
	record.Body().SetStr("delegation complete")
	record.Attributes().PutStr("gen_ai.tool.name", "spawn_agent")
	record.Attributes().PutStr("gen_ai.agent.target.id", "agent-7")

	batch, err := adapter.NewNormalizer(source.NewRegistry()).NormalizeLogs(logs)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Logs) != 1 {
		t.Fatalf("unexpected batch: %#v", batch)
	}
	got := batch.Logs[0]
	if got.Name != "tool.operation" || got.Body != "delegation complete" || got.Agent.RunID != "session-1" {
		t.Fatalf("unexpected log: %#v", got)
	}
	if got.Kind != canonical.ActivityDelegation || got.TargetAgentID != "agent-7" {
		t.Fatalf("delegation was not classified: %#v", got)
	}
}

func TestNormalizeCodexSpawnCreatesSessionLink(t *testing.T) {
	logs := plog.NewLogs()
	record := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	record.SetEventName("codex.agent_communication")
	record.SetTimestamp(pcommon.NewTimestampFromTime(time.Unix(20, 0)))
	record.Attributes().PutStr("kind", "spawn")
	record.Attributes().PutStr("state", "send")
	record.Attributes().PutStr("sender_thread_id", "parent")
	record.Attributes().PutStr("receiver_thread_id", "child")

	batch, err := adapter.NewNormalizer(source.NewRegistry(codex.New())).NormalizeLogs(logs)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.SessionLinks) != 1 || batch.SessionLinks[0].Source != "codex" || batch.SessionLinks[0].ParentSessionID != "parent" || batch.SessionLinks[0].ChildSessionID != "child" {
		t.Fatalf("unexpected session links: %#v", batch.SessionLinks)
	}
}

func TestNormalizeMetricsKeepsGaugeAndSumPoints(t *testing.T) {
	metrics := pmetric.NewMetrics()
	resourceMetrics := metrics.ResourceMetrics().AppendEmpty()
	resourceMetrics.Resource().Attributes().PutStr("gen_ai.conversation.id", "session-1")
	scopeMetrics := resourceMetrics.ScopeMetrics().AppendEmpty()
	gauge := scopeMetrics.Metrics().AppendEmpty()
	gauge.SetName("gen_ai.usage.tokens")
	point := gauge.SetEmptyGauge().DataPoints().AppendEmpty()
	point.SetTimestamp(pcommon.NewTimestampFromTime(time.Unix(30, 0)))
	point.SetIntValue(9)

	batch, err := adapter.NewNormalizer(source.NewRegistry()).NormalizeMetrics(metrics)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Metrics) != 1 || batch.Metrics[0].Value != 9 || batch.Metrics[0].Kind != "Gauge" {
		t.Fatalf("unexpected metrics: %#v", batch.Metrics)
	}
}

func TestNormalizeTracesRejectsInconsistentTokenBreakdowns(t *testing.T) {
	traces := ptrace.NewTraces()
	span := traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("gen_ai.model.request")
	span.Attributes().PutInt("gen_ai.usage.input_tokens", 10)
	span.Attributes().PutInt("gen_ai.usage.cache_read.input_tokens", 8)
	span.Attributes().PutInt("gen_ai.usage.cache_write.input_tokens", 3)

	if _, err := adapter.NewNormalizer(source.NewRegistry()).NormalizeTraces(traces); err == nil {
		t.Fatal("NormalizeTraces() error = nil, want inconsistent token usage error")
	}
}

func TestNormalizeLogsRejectsInconsistentReasoningBreakdown(t *testing.T) {
	logs := plog.NewLogs()
	record := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	record.SetEventName("gen_ai.response.completed")
	record.Attributes().PutInt("gen_ai.usage.output_tokens", 5)
	record.Attributes().PutInt("gen_ai.usage.reasoning_tokens", 6)

	if _, err := adapter.NewNormalizer(source.NewRegistry()).NormalizeLogs(logs); err == nil {
		t.Fatal("NormalizeLogs() error = nil, want inconsistent token usage error")
	}
}

func TestNormalizeMetricsRejectsInconsistentTokenBreakdowns(t *testing.T) {
	metrics := pmetric.NewMetrics()
	metric := metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	metric.SetName("gen_ai.usage.tokens")
	point := metric.SetEmptyGauge().DataPoints().AppendEmpty()
	point.SetIntValue(1)
	point.Attributes().PutInt("gen_ai.usage.input_tokens", 10)
	point.Attributes().PutInt("gen_ai.usage.cache_read.input_tokens", 11)

	if _, err := adapter.NewNormalizer(source.NewRegistry()).NormalizeMetrics(metrics); err == nil {
		t.Fatal("NormalizeMetrics() error = nil, want inconsistent token usage error")
	}
}
