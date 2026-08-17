package otel

import (
	"fmt"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/observation"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

const normalizerVersion = 2

func BuildTraceObservations(traces ptrace.Traces, projection canonical.Batch) ([]observation.Observation, error) {
	observations := make([]observation.Observation, 0, len(projection.Spans))
	ordinal := 0
	for resourceIndex := 0; resourceIndex < traces.ResourceSpans().Len(); resourceIndex++ {
		resource := traces.ResourceSpans().At(resourceIndex)
		for scopeIndex := 0; scopeIndex < resource.ScopeSpans().Len(); scopeIndex++ {
			scope := resource.ScopeSpans().At(scopeIndex)
			for recordIndex := 0; recordIndex < scope.Spans().Len(); recordIndex++ {
				if ordinal >= len(projection.Spans) {
					return nil, fmt.Errorf("trace projection has fewer records than its OTLP export")
				}
				span := scope.Spans().At(recordIndex)
				projected := projection.Spans[ordinal]
				observationOrdinal := ordinal
				ordinal++
				if !canonical.IsSemanticSpan(projected) {
					continue
				}
				observations = append(observations, observation.Observation{
					Ordinal: observationOrdinal, Signal: canonical.SignalTrace, Kind: projected.Kind,
					Source: projected.Source, SourceEventName: span.Name(),
					OccurredAt: projected.StartedAt, ObservedAt: projected.EndedAt,
					TraceID: projected.TraceID, SpanID: projected.SpanID, ParentSpanID: projected.ParentSpanID,
					SessionID: projected.Agent.RunID, AgentID: projected.Agent.AgentID,
					AgentDefinition: projected.Agent.AgentDefinition,
					AgentType:       projected.Agent.AgentType, ParentAgentID: projected.Agent.ParentAgentID,
					Model: projected.Agent.Model, Usage: projected.Agent.Tokens,
					NormalizerVersion: normalizerVersion,
				})
			}
		}
	}
	if ordinal != len(projection.Spans) {
		return nil, fmt.Errorf("trace projection has %d records for %d OTLP spans", len(projection.Spans), ordinal)
	}
	return observations, nil
}

func BuildLogObservations(logs plog.Logs, projection canonical.Batch) ([]observation.Observation, error) {
	observations := make([]observation.Observation, 0, len(projection.Logs))
	ordinal := 0
	for resourceIndex := 0; resourceIndex < logs.ResourceLogs().Len(); resourceIndex++ {
		resource := logs.ResourceLogs().At(resourceIndex)
		for scopeIndex := 0; scopeIndex < resource.ScopeLogs().Len(); scopeIndex++ {
			scope := resource.ScopeLogs().At(scopeIndex)
			for recordIndex := 0; recordIndex < scope.LogRecords().Len(); recordIndex++ {
				if ordinal >= len(projection.Logs) {
					return nil, fmt.Errorf("log projection has fewer records than its OTLP export")
				}
				record := scope.LogRecords().At(recordIndex)
				sourceEventName := record.EventName()
				if sourceEventName == "" {
					if value, ok := record.Attributes().AsRaw()["event.name"].(string); ok {
						sourceEventName = value
					}
				}
				projected := projection.Logs[ordinal]
				observedAt := record.ObservedTimestamp().AsTime()
				if record.ObservedTimestamp() == 0 {
					observedAt = projected.ObservedAt
				}
				observations = append(observations, observation.Observation{
					Ordinal: ordinal, Signal: canonical.SignalLog, Kind: projected.Kind,
					Source: projected.Source, SourceEventName: sourceEventName,
					OccurredAt: projected.ObservedAt, ObservedAt: observedAt,
					TraceID: projected.TraceID, SpanID: projected.SpanID,
					SessionID: projected.Agent.RunID, AgentID: projected.Agent.AgentID,
					AgentDefinition: projected.Agent.AgentDefinition,
					AgentType:       projected.Agent.AgentType, ParentAgentID: projected.Agent.ParentAgentID,
					Model: projected.Agent.Model, Usage: projected.Agent.Tokens,
					NormalizerVersion: normalizerVersion,
				})
				ordinal++
			}
		}
	}
	if ordinal != len(projection.Logs) {
		return nil, fmt.Errorf("log projection has %d records for %d OTLP logs", len(projection.Logs), ordinal)
	}
	return observations, nil
}

// BuildMetricObservations creates query metadata for each OTLP metric. Complete
// points, temporality, buckets, and exemplars remain in the protobuf journal.
func (normalizer Normalizer) BuildMetricObservations(metrics pmetric.Metrics) ([]observation.Observation, error) {
	observations := make([]observation.Observation, 0)
	ordinal := 0
	for resourceIndex := 0; resourceIndex < metrics.ResourceMetrics().Len(); resourceIndex++ {
		resource := metrics.ResourceMetrics().At(resourceIndex)
		resourceAttributes := resource.Resource().Attributes().AsRaw()
		for scopeIndex := 0; scopeIndex < resource.ScopeMetrics().Len(); scopeIndex++ {
			scope := resource.ScopeMetrics().At(scopeIndex)
			for metricIndex := 0; metricIndex < scope.Metrics().Len(); metricIndex++ {
				metric := scope.Metrics().At(metricIndex)
				profiled := normalizer.profile(metric.Name(), resourceAttributes)
				agent := canonical.DeriveAgentContext(resourceAttributes)
				observations = append(observations, observation.Observation{
					Ordinal: ordinal, Signal: canonical.SignalMetric, Kind: canonical.ActivityUnknown,
					Source: profiled.Source, SourceEventName: metric.Name(),
					OccurredAt: metricTime(metric), ObservedAt: metricTime(metric),
					SessionID: agent.RunID, AgentID: agent.AgentID, AgentDefinition: agent.AgentDefinition, AgentType: agent.AgentType,
					ParentAgentID: agent.ParentAgentID, Model: agent.Model, Usage: agent.Tokens,
					NormalizerVersion: normalizerVersion,
				})
				ordinal++
			}
		}
	}
	return observations, nil
}

func metricTime(metric pmetric.Metric) time.Time {
	var timestamp time.Time
	update := func(value time.Time) {
		if value.After(timestamp) {
			timestamp = value
		}
	}
	switch metric.Type() {
	case pmetric.MetricTypeGauge:
		for index := 0; index < metric.Gauge().DataPoints().Len(); index++ {
			update(metric.Gauge().DataPoints().At(index).Timestamp().AsTime())
		}
	case pmetric.MetricTypeSum:
		for index := 0; index < metric.Sum().DataPoints().Len(); index++ {
			update(metric.Sum().DataPoints().At(index).Timestamp().AsTime())
		}
	case pmetric.MetricTypeHistogram:
		for index := 0; index < metric.Histogram().DataPoints().Len(); index++ {
			update(metric.Histogram().DataPoints().At(index).Timestamp().AsTime())
		}
	case pmetric.MetricTypeExponentialHistogram:
		for index := 0; index < metric.ExponentialHistogram().DataPoints().Len(); index++ {
			update(metric.ExponentialHistogram().DataPoints().At(index).Timestamp().AsTime())
		}
	case pmetric.MetricTypeSummary:
		for index := 0; index < metric.Summary().DataPoints().Len(); index++ {
			update(metric.Summary().DataPoints().At(index).Timestamp().AsTime())
		}
	}
	return timestamp
}
