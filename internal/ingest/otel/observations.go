package otel

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/observation"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
)

const normalizerVersion = 1

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
				payload, err := marshalSingleSpan(resource, scope, span)
				if err != nil {
					return nil, err
				}
				attributes, err := marshalAttributeLayers(resource.Resource().Attributes().AsRaw(), scope.Scope().Attributes().AsRaw(), span.Attributes().AsRaw())
				if err != nil {
					return nil, err
				}
				observations = append(observations, observation.Observation{
					Ordinal: ordinal, Signal: canonical.SignalTrace, Kind: projected.Kind,
					Source: projected.Source, SourceEventName: span.Name(),
					OccurredAt: projected.StartedAt, ObservedAt: projected.EndedAt,
					TraceID: projected.TraceID, SpanID: projected.SpanID, ParentSpanID: projected.ParentSpanID,
					SessionID: projected.Agent.RunID, AgentID: projected.Agent.AgentID,
					AgentDefinition: projected.Agent.AgentDefinition,
					AgentType:       projected.Agent.AgentType, ParentAgentID: projected.Agent.ParentAgentID,
					Model: projected.Agent.Model, Usage: projected.Agent.Tokens,
					Payload: payload, SourceAttributes: attributes, NormalizerVersion: normalizerVersion,
				})
				ordinal++
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
				payload, err := marshalSingleLog(resource, scope, record)
				if err != nil {
					return nil, err
				}
				attributes, err := marshalAttributeLayers(resource.Resource().Attributes().AsRaw(), scope.Scope().Attributes().AsRaw(), record.Attributes().AsRaw())
				if err != nil {
					return nil, err
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
					Payload: payload, SourceAttributes: attributes, NormalizerVersion: normalizerVersion,
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

// BuildMetricObservations keeps one complete observation per OTLP metric. The
// payload retains all data points, temporality, buckets, and exemplars.
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
				payload, err := marshalSingleMetric(resource, scope, metric)
				if err != nil {
					return nil, err
				}
				attributes, err := marshalAttributeLayers(resourceAttributes, scope.Scope().Attributes().AsRaw(), nil)
				if err != nil {
					return nil, err
				}
				agent := canonical.DeriveAgentContext(resourceAttributes)
				observations = append(observations, observation.Observation{
					Ordinal: ordinal, Signal: canonical.SignalMetric, Kind: canonical.ActivityUnknown,
					Source: profiled.Source, SourceEventName: metric.Name(),
					OccurredAt: metricTime(metric), ObservedAt: metricTime(metric),
					SessionID: agent.RunID, AgentID: agent.AgentID, AgentDefinition: agent.AgentDefinition, AgentType: agent.AgentType,
					ParentAgentID: agent.ParentAgentID, Model: agent.Model, Usage: agent.Tokens,
					Payload: payload, SourceAttributes: attributes, NormalizerVersion: normalizerVersion,
				})
				ordinal++
			}
		}
	}
	return observations, nil
}

func marshalSingleSpan(resource ptrace.ResourceSpans, scope ptrace.ScopeSpans, span ptrace.Span) (json.RawMessage, error) {
	single := ptrace.NewTraces()
	destinationResource := single.ResourceSpans().AppendEmpty()
	resource.Resource().CopyTo(destinationResource.Resource())
	destinationResource.SetSchemaUrl(resource.SchemaUrl())
	destinationScope := destinationResource.ScopeSpans().AppendEmpty()
	scope.Scope().CopyTo(destinationScope.Scope())
	destinationScope.SetSchemaUrl(scope.SchemaUrl())
	span.CopyTo(destinationScope.Spans().AppendEmpty())
	payload, err := ptraceotlp.NewExportRequestFromTraces(single).MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal canonical span JSON: %w", err)
	}
	return payload, nil
}

func marshalSingleLog(resource plog.ResourceLogs, scope plog.ScopeLogs, record plog.LogRecord) (json.RawMessage, error) {
	single := plog.NewLogs()
	destinationResource := single.ResourceLogs().AppendEmpty()
	resource.Resource().CopyTo(destinationResource.Resource())
	destinationResource.SetSchemaUrl(resource.SchemaUrl())
	destinationScope := destinationResource.ScopeLogs().AppendEmpty()
	scope.Scope().CopyTo(destinationScope.Scope())
	destinationScope.SetSchemaUrl(scope.SchemaUrl())
	record.CopyTo(destinationScope.LogRecords().AppendEmpty())
	payload, err := plogotlp.NewExportRequestFromLogs(single).MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal canonical log JSON: %w", err)
	}
	return payload, nil
}

func marshalSingleMetric(resource pmetric.ResourceMetrics, scope pmetric.ScopeMetrics, metric pmetric.Metric) (json.RawMessage, error) {
	single := pmetric.NewMetrics()
	destinationResource := single.ResourceMetrics().AppendEmpty()
	resource.Resource().CopyTo(destinationResource.Resource())
	destinationResource.SetSchemaUrl(resource.SchemaUrl())
	destinationScope := destinationResource.ScopeMetrics().AppendEmpty()
	scope.Scope().CopyTo(destinationScope.Scope())
	destinationScope.SetSchemaUrl(scope.SchemaUrl())
	metric.CopyTo(destinationScope.Metrics().AppendEmpty())
	payload, err := pmetricotlp.NewExportRequestFromMetrics(single).MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal canonical metric JSON: %w", err)
	}
	return payload, nil
}

func marshalAttributeLayers(resource, scope, record map[string]any) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{"resource": resource, "scope": scope, "record": record})
	if err != nil {
		return nil, fmt.Errorf("marshal source attributes: %w", err)
	}
	return payload, nil
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
