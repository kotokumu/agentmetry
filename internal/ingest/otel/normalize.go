package otel

import (
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"github.com/theoden9014/agentmetry/internal/canonical"
	source "github.com/theoden9014/agentmetry/sourceplugin"
)

type Normalizer struct {
	profiles source.Registry
}

func NewNormalizer(profiles source.Registry) Normalizer {
	return Normalizer{profiles: profiles}
}

func (normalizer Normalizer) NormalizeTraces(traces ptrace.Traces) (canonical.Batch, error) {
	batch := canonical.Batch{Signal: canonical.SignalTrace}
	for resourceIndex := 0; resourceIndex < traces.ResourceSpans().Len(); resourceIndex++ {
		resourceSpans := traces.ResourceSpans().At(resourceIndex)
		resourceAttributes := resourceSpans.Resource().Attributes().AsRaw()
		for scopeIndex := 0; scopeIndex < resourceSpans.ScopeSpans().Len(); scopeIndex++ {
			spans := resourceSpans.ScopeSpans().At(scopeIndex).Spans()
			for spanIndex := 0; spanIndex < spans.Len(); spanIndex++ {
				span := spans.At(spanIndex)
				profiled := normalizer.profiles.Profile(source.Event{Name: span.Name(), Attributes: mergeAttributes(resourceAttributes, span.Attributes())})
				attributes := profiled.Attributes
				kind, toolName, targetAgentID, content := canonical.DeriveActivity(profiled.Name, attributes)
				batch.Spans = append(batch.Spans, canonical.Span{
					Source:          profiled.Source,
					TraceID:         span.TraceID().String(),
					SpanID:          span.SpanID().String(),
					ParentSpanID:    span.ParentSpanID().String(),
					Name:            profiled.Name,
					StartedAt:       span.StartTimestamp().AsTime(),
					EndedAt:         span.EndTimestamp().AsTime(),
					Status:          span.Status().Code().String(),
					Kind:            kind,
					ToolName:        toolName,
					TargetAgentID:   targetAgentID,
					TargetAgentType: canonical.DeriveTargetAgentType(attributes),
					Content:         content,
					CostUSD:         canonical.DeriveCostUSD(attributes),
					Attributes:      attributes,
					Agent:           canonical.DeriveAgentContext(attributes),
				})
			}
		}
	}
	return batch, nil
}

func (normalizer Normalizer) NormalizeLogs(logs plog.Logs) (canonical.Batch, error) {
	batch := canonical.Batch{Signal: canonical.SignalLog}
	for resourceIndex := 0; resourceIndex < logs.ResourceLogs().Len(); resourceIndex++ {
		resourceLogs := logs.ResourceLogs().At(resourceIndex)
		resourceAttributes := resourceLogs.Resource().Attributes().AsRaw()
		for scopeIndex := 0; scopeIndex < resourceLogs.ScopeLogs().Len(); scopeIndex++ {
			records := resourceLogs.ScopeLogs().At(scopeIndex).LogRecords()
			for recordIndex := 0; recordIndex < records.Len(); recordIndex++ {
				record := records.At(recordIndex)
				attributes := mergeAttributes(resourceAttributes, record.Attributes())
				name := record.EventName()
				if name == "" {
					name = firstAttributeString(attributes, "event.name", "otel.name")
				}
				profiled := normalizer.profiles.Profile(source.Event{Name: name, Attributes: attributes})
				name, attributes = profiled.Name, profiled.Attributes
				kind, toolName, targetAgentID, content := canonical.DeriveActivity(name, attributes)
				body := record.Body().AsString()
				if content != "" {
					body = content
				}
				observedAt := record.Timestamp().AsTime()
				if record.Timestamp() == 0 {
					observedAt = record.ObservedTimestamp().AsTime()
				}
				batch.Logs = append(batch.Logs, canonical.Log{
					Source:          profiled.Source,
					ObservedAt:      observedAt,
					Severity:        record.SeverityText(),
					Name:            name,
					Body:            body,
					TraceID:         record.TraceID().String(),
					SpanID:          record.SpanID().String(),
					Kind:            kind,
					ToolName:        toolName,
					TargetAgentID:   targetAgentID,
					TargetAgentType: canonical.DeriveTargetAgentType(attributes),
					CostUSD:         canonical.DeriveCostUSD(attributes),
					Attributes:      attributes,
					Agent:           canonical.DeriveAgentContext(attributes),
				})
			}
		}
	}
	return batch, nil
}

func (normalizer Normalizer) NormalizeMetrics(metrics pmetric.Metrics) (canonical.Batch, error) {
	batch := canonical.Batch{Signal: canonical.SignalMetric}
	for resourceIndex := 0; resourceIndex < metrics.ResourceMetrics().Len(); resourceIndex++ {
		resourceMetrics := metrics.ResourceMetrics().At(resourceIndex)
		resourceAttributes := resourceMetrics.Resource().Attributes().AsRaw()
		for scopeIndex := 0; scopeIndex < resourceMetrics.ScopeMetrics().Len(); scopeIndex++ {
			metricSlice := resourceMetrics.ScopeMetrics().At(scopeIndex).Metrics()
			for metricIndex := 0; metricIndex < metricSlice.Len(); metricIndex++ {
				metric := metricSlice.At(metricIndex)
				switch metric.Type() {
				case pmetric.MetricTypeGauge:
					normalizer.appendNumberPoints(&batch, resourceAttributes, metric.Name(), metric.Type().String(), metric.Gauge().DataPoints())
				case pmetric.MetricTypeSum:
					normalizer.appendNumberPoints(&batch, resourceAttributes, metric.Name(), metric.Type().String(), metric.Sum().DataPoints())
				case pmetric.MetricTypeHistogram:
					points := metric.Histogram().DataPoints()
					for pointIndex := 0; pointIndex < points.Len(); pointIndex++ {
						point := points.At(pointIndex)
						value := float64(point.Count())
						if point.HasSum() {
							value = point.Sum()
						}
						normalizer.appendMetricPoint(&batch, resourceAttributes, point.Attributes(), metric.Name(), metric.Type().String(), point.Timestamp().AsTime(), value)
					}
				default:
					continue
				}
			}
		}
	}
	return batch, nil
}

func (normalizer Normalizer) appendNumberPoints(batch *canonical.Batch, resourceAttributes map[string]any, name, kind string, points pmetric.NumberDataPointSlice) {
	for pointIndex := 0; pointIndex < points.Len(); pointIndex++ {
		point := points.At(pointIndex)
		var value float64
		switch point.ValueType() {
		case pmetric.NumberDataPointValueTypeInt:
			value = float64(point.IntValue())
		case pmetric.NumberDataPointValueTypeDouble:
			value = point.DoubleValue()
		default:
			continue
		}
		normalizer.appendMetricPoint(batch, resourceAttributes, point.Attributes(), name, kind, point.Timestamp().AsTime(), value)
	}
}

func (normalizer Normalizer) appendMetricPoint(batch *canonical.Batch, resourceAttributes map[string]any, pointAttributes pcommon.Map, name, kind string, observedAt time.Time, value float64) {
	attributes := mergeAttributes(resourceAttributes, pointAttributes)
	profiled := normalizer.profiles.Profile(source.Event{Name: name, Attributes: attributes})
	batch.Metrics = append(batch.Metrics, canonical.MetricPoint{
		Source:     profiled.Source,
		ObservedAt: observedAt,
		Name:       profiled.Name,
		Kind:       kind,
		Value:      value,
		CostUSD:    canonical.DeriveCostUSD(profiled.Attributes),
		Attributes: profiled.Attributes,
		Agent:      canonical.DeriveAgentContext(profiled.Attributes),
	})
}

func (normalizer Normalizer) profile(name string, attributes map[string]any) source.ProfiledEvent {
	return normalizer.profiles.Profile(source.Event{Name: name, Attributes: attributes})
}

func mergeAttributes(resource map[string]any, local pcommon.Map) map[string]any {
	merged := make(map[string]any, len(resource)+local.Len())
	for key, value := range resource {
		merged[key] = value
	}
	for key, value := range local.AsRaw() {
		merged[key] = value
	}
	return merged
}

func firstAttributeString(attributes map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := attributes[key].(string); ok {
			return value
		}
	}
	return ""
}
