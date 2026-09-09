package otel

import (
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	source "github.com/kotokumu/agentmetry/sourceplugin"
)

func TestLogObservationPrefersSemanticEventNameOverNativeCallsite(t *testing.T) {
	logs := plog.NewLogs()
	record := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	record.SetEventName("event otel/src/events/session_telemetry.rs:1012")
	record.Attributes().PutStr("event.name", "codex.sse_event")
	projection := canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{{Source: "codex"}}}

	observations, err := BuildLogObservations(logs, projection)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].SourceEventName != "codex.sse_event" {
		t.Fatalf("source event = %q, want codex.sse_event", observations[0].SourceEventName)
	}
}

func TestLogObservationPricingTimePrefersProducerThenOTLPTimestamp(t *testing.T) {
	logs := plog.NewLogs()
	records := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords()
	producer := records.AppendEmpty()
	producer.SetEventName("gen_ai.response.completed")
	producer.SetTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC)))
	producer.Attributes().PutStr("event.timestamp", "2026-09-09T01:00:00Z")
	otlp := records.AppendEmpty()
	otlp.SetEventName("gen_ai.response.completed")
	otlp.SetTimestamp(pcommon.NewTimestampFromTime(time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)))

	projection, err := NewNormalizer(source.NewRegistry()).NormalizeLogs(logs)
	if err != nil {
		t.Fatal(err)
	}
	observations, err := BuildLogObservations(logs, projection)
	if err != nil {
		t.Fatal(err)
	}
	if got := observations[0].OccurredAt; !got.Equal(time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("producer occurred_at = %s", got)
	}
	if got := observations[1].OccurredAt; !got.Equal(time.Date(2026, 9, 9, 3, 0, 0, 0, time.UTC)) {
		t.Fatalf("OTLP occurred_at = %s", got)
	}
}

func TestTraceObservationsProjectSemanticMetadataWithoutDuplicatingPayload(t *testing.T) {
	traces := ptrace.NewTraces()
	resource := traces.ResourceSpans().AppendEmpty()
	resource.SetSchemaUrl("https://example.test/resource-schema")
	resource.Resource().Attributes().PutStr("service.name", "example-agent")
	scope := resource.ScopeSpans().AppendEmpty()
	scope.SetSchemaUrl("https://example.test/scope-schema")
	scope.Scope().SetName("example-scope")
	span := scope.Spans().AppendEmpty()
	span.SetName("gen_ai.response.completed")
	span.Events().AppendEmpty().SetName("tool-finished")
	span.Links().AppendEmpty().Attributes().PutEmptyBytes("binary").FromRaw([]byte{0, 1, 2})

	projection, err := NewNormalizer(source.NewRegistry()).NormalizeTraces(traces)
	if err != nil {
		t.Fatal(err)
	}
	observations, err := BuildTraceObservations(traces, projection)
	if err != nil {
		t.Fatal(err)
	}

	if len(observations) != 1 || observations[0].Source != source.Unknown || observations[0].SourceEventName != "gen_ai.response.completed" {
		t.Fatalf("unexpected observations: %#v", observations)
	}
}

func TestTraceObservationsSkipIncidentalRuntimeSpans(t *testing.T) {
	traces := ptrace.NewTraces()
	spans := traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans()
	spans.AppendEmpty().SetName("handle_responses")
	spans.AppendEmpty().SetName("gen_ai.response.completed")

	projection, err := NewNormalizer(source.NewRegistry()).NormalizeTraces(traces)
	if err != nil {
		t.Fatal(err)
	}
	observations, err := BuildTraceObservations(traces, projection)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].Ordinal != 1 || observations[0].SourceEventName != "gen_ai.response.completed" {
		t.Fatalf("unexpected semantic observations: %#v", observations)
	}
}

func TestMetricObservationsProjectMetadataWithoutDuplicatingPayload(t *testing.T) {
	metrics := pmetric.NewMetrics()
	resource := metrics.ResourceMetrics().AppendEmpty()
	scope := resource.ScopeMetrics().AppendEmpty()
	metric := scope.Metrics().AppendEmpty()
	metric.SetName("gen_ai.usage.tokens")
	histogram := metric.SetEmptyHistogram()
	histogram.SetAggregationTemporality(pmetric.AggregationTemporalityCumulative)
	point := histogram.DataPoints().AppendEmpty()
	point.SetCount(3)
	point.SetSum(42)
	point.BucketCounts().FromRaw([]uint64{1, 2})
	point.ExplicitBounds().FromRaw([]float64{10})
	point.Exemplars().AppendEmpty().SetDoubleValue(7)

	observations, err := NewNormalizer(source.NewRegistry()).BuildMetricObservations(metrics)
	if err != nil {
		t.Fatal(err)
	}

	if len(observations) != 1 || observations[0].SourceEventName != "gen_ai.usage.tokens" {
		t.Fatalf("unexpected observations: %#v", observations)
	}
}
