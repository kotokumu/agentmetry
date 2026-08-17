package otel

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/theoden9014/agentmetry/internal/harness"
	"github.com/theoden9014/agentmetry/internal/ingest"
	source "github.com/theoden9014/agentmetry/sourceplugin"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	grpcmetadata "google.golang.org/grpc/metadata"
)

type recordingCommitter struct {
	err      error
	calls    int
	accepted ingest.AcceptedExport
}

func Test_httpHarnessEvidence(t *testing.T) {
	type args struct {
		header http.Header
	}
	tests := []struct {
		name string
		args args
		want harness.ReceiptEvidence
	}{
		{
			name: "reported metadata",
			args: args{header: http.Header{
				"X-Agentmetry-Harness-Scope":       []string{"project-7f2a"},
				"X-Agentmetry-Harness-Fingerprint": []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"},
				"X-Agentmetry-Harness-Label":       []string{"AGENTS v2"},
				"Authorization":                    []string{"secret"},
			}},
			want: harness.ReceiptEvidence{State: harness.ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", Label: "AGENTS v2"},
		},
		{
			name: "duplicate metadata is invalid",
			args: args{header: http.Header{
				"X-Agentmetry-Harness-Scope":       []string{"project-7f2a", "project-7f2a"},
				"X-Agentmetry-Harness-Fingerprint": []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"},
			}},
			want: harness.ReceiptEvidence{State: harness.ReceiptInvalid},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := httpHarnessEvidence(tt.args.header); !cmp.Equal(tt.want, got) {
				t.Errorf("httpHarnessEvidence() = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got))
			}
		})
	}
}

func Test_grpcHarnessEvidence(t *testing.T) {
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name string
		args args
		want harness.ReceiptEvidence
	}{
		{
			name: "reported metadata",
			args: args{ctx: grpcmetadata.NewIncomingContext(context.Background(), grpcmetadata.MD{
				harnessScopeMetadata:       []string{"project-7f2a"},
				harnessFingerprintMetadata: []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"},
				harnessLabelMetadata:       []string{"AGENTS v2"},
				"authorization":            []string{"secret"},
			})},
			want: harness.ReceiptEvidence{State: harness.ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", Label: "AGENTS v2"},
		},
		{
			name: "duplicate metadata is invalid",
			args: args{ctx: grpcmetadata.NewIncomingContext(context.Background(), grpcmetadata.MD{
				harnessScopeMetadata:       []string{"project-7f2a"},
				harnessFingerprintMetadata: []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", "sha256:dfbc1de58f3b905c7b0c0fd79361699336b5f9da617b1db8f35c76673f95b29d"},
			})},
			want: harness.ReceiptEvidence{State: harness.ReceiptInvalid},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := grpcHarnessEvidence(tt.args.ctx); !cmp.Equal(tt.want, got) {
				t.Errorf("grpcHarnessEvidence() = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got))
			}
		})
	}
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
	ctx := grpcmetadata.NewIncomingContext(context.Background(), grpcmetadata.MD{
		harnessScopeMetadata:       []string{"project-7f2a"},
		harnessFingerprintMetadata: []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"},
	})

	if _, err := receiver.Export(ctx, request); err != nil {
		t.Fatal(err)
	}

	if committer.accepted.Envelope.Signal != "trace" || committer.accepted.Envelope.Transport != ingest.TransportGRPC {
		t.Fatalf("unexpected envelope: %#v", committer.accepted.Envelope)
	}
	if len(committer.accepted.Envelope.Protobuf) == 0 {
		t.Fatalf("canonical payload was not retained: %#v", committer.accepted.Envelope)
	}
	if committer.accepted.Journal.Harness.State != harness.ReceiptReported || committer.accepted.Journal.Harness.Scope != "project-7f2a" {
		t.Fatalf("harness receipt was not attached: %#v", committer.accepted.Journal.Harness)
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

func TestHTTPTraceEndpointAttachesOnlyValidatedHarnessReceipt(t *testing.T) {
	exportRequest := ptraceotlp.NewExportRequestFromTraces(ptrace.NewTraces())
	payload, err := exportRequest.MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		headers http.Header
		want    harness.ReceiptEvidence
	}{
		{
			name: "reported",
			headers: http.Header{
				"X-Agentmetry-Harness-Scope":       []string{"project-7f2a"},
				"X-Agentmetry-Harness-Fingerprint": []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"},
				"X-Agentmetry-Harness-Label":       []string{"AGENTS v2"},
				"Authorization":                    []string{"Bearer must-not-be-retained"},
				"X-Unrelated-Secret":               []string{"must-not-be-retained"},
			},
			want: harness.ReceiptEvidence{State: harness.ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", Label: "AGENTS v2"},
		},
		{
			name: "invalid",
			headers: http.Header{
				"X-Agentmetry-Harness-Scope": []string{"project-7f2a"},
			},
			want: harness.ReceiptEvidence{State: harness.ReceiptInvalid},
		},
		{
			name: "unrelated headers remain unreported",
			headers: http.Header{
				"Authorization":      []string{"Bearer must-not-be-retained"},
				"X-Unrelated-Secret": []string{"must-not-be-retained"},
			},
			want: harness.ReceiptEvidence{State: harness.ReceiptUnreported},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			committer := &recordingCommitter{}
			receiver := NewReceiver(committer, source.NewRegistry())
			request := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(payload))
			request.Header = tt.headers.Clone()
			request.Header.Set("Content-Type", "application/x-protobuf")
			response := httptest.NewRecorder()

			receiver.HTTPHandler().ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
			}
			if committer.calls != 1 || !cmp.Equal(committer.accepted.Journal.Harness, tt.want) {
				t.Fatalf("committed harness receipt = %#v, want %#v", committer.accepted.Journal.Harness, tt.want)
			}
		})
	}
}
