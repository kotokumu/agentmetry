package otel

import (
	"bytes"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	source "github.com/theoden9014/agentmetry/sourceplugin"
)

func TestTraceObservationsRetainEventsLinksScopesAndSchemaURLs(t *testing.T) {
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

	if len(observations) != 1 || observations[0].Source != source.Unknown || !json.Valid(observations[0].Payload) {
		t.Fatalf("unexpected observations: %#v", observations)
	}
	for _, expected := range [][]byte{
		[]byte("resource-schema"), []byte("scope-schema"), []byte("example-scope"),
		[]byte("tool-finished"), []byte("binary"),
	} {
		if !bytes.Contains(observations[0].Payload, expected) {
			t.Fatalf("payload does not contain %q: %s", expected, observations[0].Payload)
		}
	}
}

func TestMetricObservationsRetainTemporalityBucketsAndExemplars(t *testing.T) {
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

	if len(observations) != 1 || !json.Valid(observations[0].Payload) {
		t.Fatalf("unexpected observations: %#v", observations)
	}
	for _, expected := range [][]byte{[]byte(`"aggregationTemporality":2`), []byte("bucketCounts"), []byte("exemplars")} {
		if !bytes.Contains(observations[0].Payload, expected) {
			t.Fatalf("payload does not contain %q: %s", expected, observations[0].Payload)
		}
	}
}
