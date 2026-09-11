package otel

import (
	"fmt"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
	source "github.com/kotokumu/agentmetry/sourceplugin"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
)

// ReplayExport rebuilds current semantic projections from authoritative OTLP
// protobuf. It has no storage or acknowledgement side effects.
func ReplayExport(signal canonical.Signal, transport ingest.Transport, receivedAt time.Time, protobuf []byte, profiles source.Registry) (ingest.AcceptedExport, error) {
	normalizer := NewNormalizer(profiles)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(signal, transport, receivedAt, protobuf)}
	var normalizationErr error
	switch signal {
	case canonical.SignalTrace:
		request := ptraceotlp.NewExportRequest()
		if err := request.UnmarshalProto(protobuf); err != nil {
			return ingest.AcceptedExport{}, fmt.Errorf("decode replay %s export: %w", signal, err)
		}
		accepted.Projection, normalizationErr = normalizer.NormalizeTraces(request.Traces())
		if normalizationErr == nil {
			accepted.Observations, normalizationErr = BuildTraceObservations(request.Traces(), accepted.Projection)
		}
	case canonical.SignalLog:
		request := plogotlp.NewExportRequest()
		if err := request.UnmarshalProto(protobuf); err != nil {
			return ingest.AcceptedExport{}, fmt.Errorf("decode replay %s export: %w", signal, err)
		}
		accepted.Projection, normalizationErr = normalizer.NormalizeLogs(request.Logs())
		if normalizationErr == nil {
			accepted.Observations, normalizationErr = BuildLogObservations(request.Logs(), accepted.Projection)
		}
	case canonical.SignalMetric:
		request := pmetricotlp.NewExportRequest()
		if err := request.UnmarshalProto(protobuf); err != nil {
			return ingest.AcceptedExport{}, fmt.Errorf("decode replay %s export: %w", signal, err)
		}
		accepted.Projection, normalizationErr = normalizer.NormalizeMetrics(request.Metrics())
		if normalizationErr == nil {
			accepted.Observations, normalizationErr = normalizer.BuildMetricObservations(request.Metrics())
		}
	default:
		return ingest.AcceptedExport{}, fmt.Errorf("unsupported OTLP signal %q", signal)
	}
	if normalizationErr != nil {
		accepted.Observations = nil
		accepted.Projection = canonical.Batch{}
		accepted.NormalizationError = normalizationErr.Error()
	}
	accepted.Journal = ingest.DeriveJournalMetadata(accepted.Observations, accepted.Projection, accepted.NormalizationError)
	return accepted, nil
}
