package otel

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpcmetadata "google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/harness"
	"github.com/theoden9014/agentmetry/internal/ingest"
	source "github.com/theoden9014/agentmetry/sourceplugin"
)

const maxRequestBytes = 32 << 20

const (
	harnessScopeMetadata       = "x-agentmetry-harness-scope"
	harnessFingerprintMetadata = "x-agentmetry-harness-fingerprint"
	harnessLabelMetadata       = "x-agentmetry-harness-label"
)

type Receiver struct {
	committer  ingest.ExportCommitter
	normalizer Normalizer
	traces     *traceReceiver
	logs       *logReceiver
	metrics    *metricReceiver
}

func NewReceiver(committer ingest.ExportCommitter, profiles source.Registry) *Receiver {
	normalizer := NewNormalizer(profiles)
	return &Receiver{
		committer:  committer,
		normalizer: normalizer,
		traces:     newTraceReceiver(committer, normalizer),
		logs:       newLogReceiver(committer, normalizer),
		metrics:    newMetricReceiver(committer, normalizer),
	}
}

func (receiver *Receiver) RegisterGRPC(server *grpc.Server) {
	ptraceotlp.RegisterGRPCServer(server, receiver.traces)
	plogotlp.RegisterGRPCServer(server, receiver.logs)
	pmetricotlp.RegisterGRPCServer(server, receiver.metrics)
}

func (receiver *Receiver) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/traces", receiver.exportTracesHTTP)
	mux.HandleFunc("POST /v1/logs", receiver.exportLogsHTTP)
	mux.HandleFunc("POST /v1/metrics", receiver.exportMetricsHTTP)
	return mux
}

type traceReceiver struct {
	ptraceotlp.UnimplementedGRPCServer
	committer  ingest.ExportCommitter
	normalizer Normalizer
}

func newTraceReceiver(committer ingest.ExportCommitter, normalizer Normalizer) *traceReceiver {
	return &traceReceiver{committer: committer, normalizer: normalizer}
}

func (receiver *traceReceiver) Export(ctx context.Context, request ptraceotlp.ExportRequest) (ptraceotlp.ExportResponse, error) {
	if err := receiver.accept(ctx, request, ingest.TransportGRPC, grpcHarnessEvidence(ctx)); err != nil {
		return ptraceotlp.NewExportResponse(), status.Errorf(codes.Unavailable, "commit traces: %v", err)
	}
	return ptraceotlp.NewExportResponse(), nil
}

func (receiver *traceReceiver) accept(ctx context.Context, request ptraceotlp.ExportRequest, transport ingest.Transport, receipt harness.ReceiptEvidence) error {
	protobuf, err := request.MarshalProto()
	if err != nil {
		return fmt.Errorf("marshal canonical trace protobuf: %w", err)
	}
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalTrace, transport, time.Now(), protobuf)}
	accepted.Projection, err = receiver.normalizer.NormalizeTraces(request.Traces())
	if err == nil {
		accepted.Observations, err = BuildTraceObservations(request.Traces(), accepted.Projection)
	}
	if err != nil {
		accepted.Projection = canonical.Batch{Signal: canonical.SignalTrace}
		accepted.Observations = nil
		accepted.NormalizationError = err.Error()
	}
	accepted.Journal = ingest.DeriveJournalMetadata(accepted.Observations, accepted.Projection, accepted.NormalizationError)
	accepted.Journal.Harness = receipt
	return receiver.committer.CommitExport(ctx, accepted)
}

type logReceiver struct {
	plogotlp.UnimplementedGRPCServer
	committer  ingest.ExportCommitter
	normalizer Normalizer
}

func newLogReceiver(committer ingest.ExportCommitter, normalizer Normalizer) *logReceiver {
	return &logReceiver{committer: committer, normalizer: normalizer}
}

func (receiver *logReceiver) Export(ctx context.Context, request plogotlp.ExportRequest) (plogotlp.ExportResponse, error) {
	if err := receiver.accept(ctx, request, ingest.TransportGRPC, grpcHarnessEvidence(ctx)); err != nil {
		return plogotlp.NewExportResponse(), status.Errorf(codes.Unavailable, "commit logs: %v", err)
	}
	return plogotlp.NewExportResponse(), nil
}

func (receiver *logReceiver) accept(ctx context.Context, request plogotlp.ExportRequest, transport ingest.Transport, receipt harness.ReceiptEvidence) error {
	protobuf, err := request.MarshalProto()
	if err != nil {
		return fmt.Errorf("marshal canonical log protobuf: %w", err)
	}
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, transport, time.Now(), protobuf)}
	accepted.Projection, err = receiver.normalizer.NormalizeLogs(request.Logs())
	if err == nil {
		accepted.Observations, err = BuildLogObservations(request.Logs(), accepted.Projection)
	}
	if err != nil {
		accepted.Projection = canonical.Batch{Signal: canonical.SignalLog}
		accepted.Observations = nil
		accepted.NormalizationError = err.Error()
	}
	accepted.Journal = ingest.DeriveJournalMetadata(accepted.Observations, accepted.Projection, accepted.NormalizationError)
	accepted.Journal.Harness = receipt
	return receiver.committer.CommitExport(ctx, accepted)
}

type metricReceiver struct {
	pmetricotlp.UnimplementedGRPCServer
	committer  ingest.ExportCommitter
	normalizer Normalizer
}

func newMetricReceiver(committer ingest.ExportCommitter, normalizer Normalizer) *metricReceiver {
	return &metricReceiver{committer: committer, normalizer: normalizer}
}

func (receiver *metricReceiver) Export(ctx context.Context, request pmetricotlp.ExportRequest) (pmetricotlp.ExportResponse, error) {
	if err := receiver.accept(ctx, request, ingest.TransportGRPC, grpcHarnessEvidence(ctx)); err != nil {
		return pmetricotlp.NewExportResponse(), status.Errorf(codes.Unavailable, "commit metrics: %v", err)
	}
	return pmetricotlp.NewExportResponse(), nil
}

func (receiver *metricReceiver) accept(ctx context.Context, request pmetricotlp.ExportRequest, transport ingest.Transport, receipt harness.ReceiptEvidence) error {
	protobuf, err := request.MarshalProto()
	if err != nil {
		return fmt.Errorf("marshal canonical metric protobuf: %w", err)
	}
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalMetric, transport, time.Now(), protobuf)}
	accepted.Projection, err = receiver.normalizer.NormalizeMetrics(request.Metrics())
	if err == nil {
		accepted.Observations, err = receiver.normalizer.BuildMetricObservations(request.Metrics())
	}
	if err != nil {
		accepted.Projection = canonical.Batch{Signal: canonical.SignalMetric}
		accepted.Observations = nil
		accepted.NormalizationError = err.Error()
	}
	accepted.Journal = ingest.DeriveJournalMetadata(accepted.Observations, accepted.Projection, accepted.NormalizationError)
	accepted.Journal.Harness = receipt
	return receiver.committer.CommitExport(ctx, accepted)
}

func (receiver *Receiver) exportTracesHTTP(response http.ResponseWriter, request *http.Request) {
	exportRequest := ptraceotlp.NewExportRequest()
	if err := decodeOTLP(request, exportRequest.UnmarshalProto, exportRequest.UnmarshalJSON); err != nil {
		writeOTLPError(response, http.StatusBadRequest, err)
		return
	}
	if err := receiver.traces.accept(request.Context(), exportRequest, httpTransport(request), httpHarnessEvidence(request.Header)); err != nil {
		writeOTLPError(response, http.StatusServiceUnavailable, err)
		return
	}
	exportResponse := ptraceotlp.NewExportResponse()
	writeOTLPResponse(response, request, exportResponse.MarshalProto, exportResponse.MarshalJSON)
}

func (receiver *Receiver) exportLogsHTTP(response http.ResponseWriter, request *http.Request) {
	exportRequest := plogotlp.NewExportRequest()
	if err := decodeOTLP(request, exportRequest.UnmarshalProto, exportRequest.UnmarshalJSON); err != nil {
		writeOTLPError(response, http.StatusBadRequest, err)
		return
	}
	if err := receiver.logs.accept(request.Context(), exportRequest, httpTransport(request), httpHarnessEvidence(request.Header)); err != nil {
		writeOTLPError(response, http.StatusServiceUnavailable, err)
		return
	}
	exportResponse := plogotlp.NewExportResponse()
	writeOTLPResponse(response, request, exportResponse.MarshalProto, exportResponse.MarshalJSON)
}

func (receiver *Receiver) exportMetricsHTTP(response http.ResponseWriter, request *http.Request) {
	exportRequest := pmetricotlp.NewExportRequest()
	if err := decodeOTLP(request, exportRequest.UnmarshalProto, exportRequest.UnmarshalJSON); err != nil {
		writeOTLPError(response, http.StatusBadRequest, err)
		return
	}
	if err := receiver.metrics.accept(request.Context(), exportRequest, httpTransport(request), httpHarnessEvidence(request.Header)); err != nil {
		writeOTLPError(response, http.StatusServiceUnavailable, err)
		return
	}
	exportResponse := pmetricotlp.NewExportResponse()
	writeOTLPResponse(response, request, exportResponse.MarshalProto, exportResponse.MarshalJSON)
}

func httpHarnessEvidence(header http.Header) harness.ReceiptEvidence {
	return harness.ParseMetadata(harness.MetadataValues{
		Scope:       header.Values(harnessScopeMetadata),
		Fingerprint: header.Values(harnessFingerprintMetadata),
		Label:       header.Values(harnessLabelMetadata),
	})
}

func grpcHarnessEvidence(ctx context.Context) harness.ReceiptEvidence {
	metadata, _ := grpcmetadata.FromIncomingContext(ctx)
	return harness.ParseMetadata(harness.MetadataValues{
		Scope:       metadata.Get(harnessScopeMetadata),
		Fingerprint: metadata.Get(harnessFingerprintMetadata),
		Label:       metadata.Get(harnessLabelMetadata),
	})
}

func httpTransport(request *http.Request) ingest.Transport {
	if strings.Contains(strings.ToLower(request.Header.Get("Content-Type")), "json") {
		return ingest.TransportHTTPJSON
	}
	return ingest.TransportHTTPProtobuf
}

func decodeOTLP(request *http.Request, unmarshalProto, unmarshalJSON func([]byte) error) error {
	payload, err := io.ReadAll(io.LimitReader(request.Body, maxRequestBytes+1))
	if err != nil {
		return fmt.Errorf("read OTLP request: %w", err)
	}
	if len(payload) > maxRequestBytes {
		return fmt.Errorf("OTLP request exceeds %d bytes", maxRequestBytes)
	}
	if strings.Contains(strings.ToLower(request.Header.Get("Content-Type")), "json") {
		if err := unmarshalJSON(payload); err != nil {
			return fmt.Errorf("decode OTLP JSON: %w", err)
		}
		return nil
	}
	if err := unmarshalProto(payload); err != nil {
		return fmt.Errorf("decode OTLP protobuf: %w", err)
	}
	return nil
}

func writeOTLPResponse(response http.ResponseWriter, request *http.Request, marshalProto, marshalJSON func() ([]byte, error)) {
	contentType := "application/x-protobuf"
	marshal := marshalProto
	if strings.Contains(strings.ToLower(request.Header.Get("Content-Type")), "json") {
		contentType = "application/json"
		marshal = marshalJSON
	}
	payload, err := marshal()
	if err != nil {
		writeOTLPError(response, http.StatusInternalServerError, err)
		return
	}
	response.Header().Set("Content-Type", contentType)
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(payload)
}

func writeOTLPError(response http.ResponseWriter, statusCode int, err error) {
	http.Error(response, err.Error(), statusCode)
}
