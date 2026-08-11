package otel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/theoden9014/agentmetry/internal/ingest"
	source "github.com/theoden9014/agentmetry/sourceplugin"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
)

type recordingCommitter struct {
	err      error
	calls    int
	accepted ingest.AcceptedExport
}

func (committer *recordingCommitter) CommitExport(_ context.Context, accepted ingest.AcceptedExport) error {
	committer.calls++
	committer.accepted = accepted
	return committer.err
}

func TestTraceExportReturnsFailureWhenDurableCommitFails(t *testing.T) {
	committer := &recordingCommitter{err: errors.New("disk full")}
	receiver := newTraceReceiver(committer, NewNormalizer(source.NewRegistry()))
	request := ptraceotlp.NewExportRequestFromTraces(ptrace.NewTraces())

	_, err := receiver.Export(context.Background(), request)

	if err == nil {
		t.Fatal("Export succeeded before durable commit")
	}
	if committer.calls != 1 {
		t.Fatalf("commit calls = %d, want 1", committer.calls)
	}
}

func TestTraceExportCommitsCanonicalOTLPEnvelope(t *testing.T) {
	committer := &recordingCommitter{}
	receiver := newTraceReceiver(committer, NewNormalizer(source.NewRegistry()))
	traces := ptrace.NewTraces()
	traces.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty().SetName("example")
	request := ptraceotlp.NewExportRequestFromTraces(traces)

	if _, err := receiver.Export(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	if committer.accepted.Envelope.Signal != "trace" || committer.accepted.Envelope.Transport != ingest.TransportGRPC {
		t.Fatalf("unexpected envelope: %#v", committer.accepted.Envelope)
	}
	if len(committer.accepted.Envelope.Protobuf) == 0 || len(committer.accepted.Envelope.CanonicalJSON) == 0 {
		t.Fatalf("canonical payload was not retained: %#v", committer.accepted.Envelope)
	}
}

func TestHTTPTraceEndpointRejectsMalformedPayloadWithoutCommit(t *testing.T) {
	committer := &recordingCommitter{}
	receiver := NewReceiver(committer, source.NewRegistry())
	request := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader("bad protobuf"))
	request.Header.Set("Content-Type", "application/x-protobuf")
	response := httptest.NewRecorder()

	receiver.HTTPHandler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if committer.calls != 0 {
		t.Fatalf("commit calls = %d, want 0", committer.calls)
	}
}
