package otel

import (
	"fmt"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/ingest"
	source "github.com/theoden9014/agentmetry/sourceplugin"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
)

// ReplayExport rebuilds current semantic projections from authoritative OTLP
// protobuf. It has no storage or acknowledgement side effects.
func ReplayExport(signal canonical.Signal, transport ingest.Transport, receivedAt time.Time, protobuf []byte, profiles source.Registry) (ingest.AcceptedExport, error) {
	normalizer := NewNormalizer(profiles)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(signal, transport, receivedAt, protobuf)}
	var err error
	switch signal {
	case canonical.SignalTrace:
		request := ptraceotlp.NewExportRequest()
		if err = request.UnmarshalProto(protobuf); err == nil {
			accepted.Projection, err = normalizer.NormalizeTraces(request.Traces())
		}
		if err == nil {
			accepted.Observations, err = BuildTraceObservations(request.Traces(), accepted.Projection)
		}
	case canonical.SignalLog:
		request := plogotlp.NewExportRequest()
		if err = request.UnmarshalProto(protobuf); err == nil {
			accepted.Projection, err = normalizer.NormalizeLogs(request.Logs())
		}
		if err == nil {
			accepted.Observations, err = BuildLogObservations(request.Logs(), accepted.Projection)
		}
	case canonical.SignalMetric:
		request := pmetricotlp.NewExportRequest()
		if err = request.UnmarshalProto(protobuf); err == nil {
			accepted.Projection, err = normalizer.NormalizeMetrics(request.Metrics())
		}
		if err == nil {
			accepted.Observations, err = normalizer.BuildMetricObservations(request.Metrics())
		}
	default:
		err = fmt.Errorf("unsupported OTLP signal %q", signal)
	}
	if err != nil {
		return ingest.AcceptedExport{}, fmt.Errorf("replay %s export: %w", signal, err)
	}
	accepted.Journal = ingest.DeriveJournalMetadata(accepted.Observations, accepted.Projection, "")
	return accepted, nil
}
