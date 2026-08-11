package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
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
	Signal        canonical.Signal
	Transport     Transport
	ReceivedAt    time.Time
	Protobuf      []byte
	CanonicalJSON json.RawMessage
	SHA256        [sha256.Size]byte
}

func NewEnvelope(signal canonical.Signal, transport Transport, receivedAt time.Time, protobuf []byte, canonicalJSON json.RawMessage) Envelope {
	return Envelope{
		Signal:        signal,
		Transport:     transport,
		ReceivedAt:    receivedAt.UTC(),
		Protobuf:      append([]byte(nil), protobuf...),
		CanonicalJSON: append(json.RawMessage(nil), canonicalJSON...),
		SHA256:        sha256.Sum256(protobuf),
	}
}

type AcceptedExport struct {
	Envelope           Envelope
	Observations       []observation.Observation
	Projection         canonical.Batch
	NormalizationError string
}

type ExportCommitter interface {
	CommitExport(context.Context, AcceptedExport) error
}
