package otel_test

import (
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/theoden9014/agentmetry/internal/canonical"
	adapter "github.com/theoden9014/agentmetry/internal/ingest/otel"
	source "github.com/theoden9014/agentmetry/sourceplugin"
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
