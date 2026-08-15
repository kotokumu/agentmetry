package ingest

import (
	"context"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/observation"
)

type Transport string

const (
	TransportGRPC         Transport = "grpc"
	TransportHTTPProtobuf Transport = "http/protobuf"
	TransportHTTPJSON     Transport = "http/json"
)

type Envelope struct {
	Signal     canonical.Signal
	Transport  Transport
	ReceivedAt time.Time
	Protobuf   []byte
}

func NewEnvelope(signal canonical.Signal, transport Transport, receivedAt time.Time, protobuf []byte) Envelope {
	return Envelope{
		Signal:     signal,
		Transport:  transport,
		ReceivedAt: receivedAt.UTC(),
		Protobuf:   append([]byte(nil), protobuf...),
	}
}

type AcceptedExport struct {
	Envelope           Envelope
	Journal            JournalMetadata
	Observations       []observation.Observation
	Projection         canonical.Batch
	NormalizationError string
}

// JournalMetadata is immutable receipt/normalization metadata owned by the
// replay journal. Reprojection may change derived rows but must not rewrite it.
type JournalMetadata struct {
	Source              string
	NormalizerVersion   int
	NormalizationStatus string
}

func DeriveJournalMetadata(observations []observation.Observation, projection canonical.Batch, normalizationError string) JournalMetadata {
	status := "projected"
	if normalizationError != "" {
		status = "failed"
	}
	metadata := JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: status}
	if len(observations) > 0 && observations[0].NormalizerVersion != 0 {
		metadata.NormalizerVersion = observations[0].NormalizerVersion
	}
	sources := make([]string, 0, len(observations)+len(projection.Spans)+len(projection.Logs)+len(projection.Metrics))
	for _, item := range observations {
		sources = append(sources, item.Source)
	}
	for _, item := range projection.Spans {
		sources = append(sources, item.Source)
	}
	for _, item := range projection.Logs {
		sources = append(sources, item.Source)
	}
	for _, item := range projection.Metrics {
		sources = append(sources, item.Source)
	}
	metadata.Source = consensusSource(sources)
	return metadata
}

func consensusSource(sources []string) string {
	source := ""
	for _, candidate := range sources {
		if candidate == "" {
			candidate = "unknown"
		}
		if source == "" {
			source = candidate
			continue
		}
		if candidate != source {
			return "mixed"
		}
	}
	if source == "" {
		return "unknown"
	}
	return source
}

type ExportCommitter interface {
	CommitExport(context.Context, AcceptedExport) error
}
