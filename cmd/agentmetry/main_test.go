package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingHTTPClient struct {
	requests []*http.Request
}

func Test_runHarnessFingerprint(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	type args struct {
		arguments []string
		root      string
	}
	tests := []struct {
		name       string
		args       args
		wantOutput string
		wantErr    bool
	}{
		{
			name:       "writes deterministic JSON",
			args:       args{arguments: []string{"--scope", "project-7f2a", "--label", "AGENTS v2", "--file", "AGENTS.md"}, root: root},
			wantOutput: "{\"scope\":\"project-7f2a\",\"fingerprint\":\"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db\",\"label\":\"AGENTS v2\"}\n",
		},
		{name: "rejects missing file flags", args: args{arguments: []string{"--scope", "project-7f2a"}, root: t.TempDir()}, wantErr: true},
		{name: "rejects positional arguments", args: args{arguments: []string{"--scope", "project-7f2a", "unexpected"}, root: t.TempDir()}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := &bytes.Buffer{}
			if err := runHarnessFingerprint(tt.args.arguments, tt.args.root, output); (err != nil) != tt.wantErr {
				t.Fatalf("runHarnessFingerprint() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if gotOutput := output.String(); gotOutput != tt.wantOutput {
				t.Errorf("runHarnessFingerprint() = %v, want %v", gotOutput, tt.wantOutput)
			}
		})
	}
}

func (client *recordingHTTPClient) Do(request *http.Request) (*http.Response, error) {
	client.requests = append(client.requests, request)
	return &http.Response{
		StatusCode: http.StatusAccepted,
		Status:     "202 Accepted",
		Body:       io.NopCloser(strings.NewReader(`{"status":"accepted"}`)),
	}, nil
}

func TestPlanUsageImportParsesPostsAndRendersAStatusLine(t *testing.T) {
	client := &recordingHTTPClient{}
	var output bytes.Buffer
	input := strings.NewReader(`{
  "rate_limits": {
    "five_hour": {"used_percentage": 25, "resets_at": 1786438800},
    "seven_day": {"used_percentage": 60, "resets_at": 1787043600}
  }
}`)

	err := runPlanUsageImport(
		[]string{"--source=claude", "--endpoint=http://agentmetry.test/api/v1/plan-usage"},
		input,
		&output,
		client,
	)

	if err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(client.requests))
	}
	if got := output.String(); !strings.Contains(got, "5h 25.0% used") || !strings.Contains(got, "168h 60.0% used") {
		t.Fatalf("unexpected status line: %q", got)
	}
}

type fakeGRPCServerLifecycle struct {
	blockGraceful bool
	graceful      atomic.Bool
	stopped       atomic.Bool
	release       chan struct{}
	stopOnce      sync.Once
}

func newFakeGRPCServerLifecycle(blockGraceful bool) *fakeGRPCServerLifecycle {
	return &fakeGRPCServerLifecycle{blockGraceful: blockGraceful, release: make(chan struct{})}
}

func (server *fakeGRPCServerLifecycle) GracefulStop() {
	server.graceful.Store(true)
	if server.blockGraceful {
		<-server.release
	}
}

func (server *fakeGRPCServerLifecycle) Stop() {
	server.stopped.Store(true)
	server.stopOnce.Do(func() { close(server.release) })
}

type fakeHTTPServerLifecycle struct {
	shutdownError error
	blockShutdown bool
	shutdown      atomic.Bool
	closed        atomic.Bool
	release       chan struct{}
	closeOnce     sync.Once
}

func newFakeHTTPServerLifecycle(blockShutdown bool, shutdownError error) *fakeHTTPServerLifecycle {
	return &fakeHTTPServerLifecycle{
		shutdownError: shutdownError,
		blockShutdown: blockShutdown,
		release:       make(chan struct{}),
	}
}

func (server *fakeHTTPServerLifecycle) Shutdown(ctx context.Context) error {
	server.shutdown.Store(true)
	if server.blockShutdown {
		select {
		case <-server.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return server.shutdownError
}

func (server *fakeHTTPServerLifecycle) Close() error {
	server.closed.Store(true)
	server.closeOnce.Do(func() { close(server.release) })
	return nil
}

type fakeServiceLifecycle struct {
	closed         atomic.Bool
	waitForContext bool
}

func (services *fakeServiceLifecycle) Close(ctx context.Context) error {
	services.closed.Store(true)
	if services.waitForContext {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func TestShutdownServingStopsEveryOwnerAfterOneShutdownError(t *testing.T) {
	wantErr := errors.New("OTLP shutdown failed")
	grpcServer := newFakeGRPCServerLifecycle(false)
	otlpServer := newFakeHTTPServerLifecycle(false, wantErr)
	dashboardServer := newFakeHTTPServerLifecycle(false, nil)
	services := &fakeServiceLifecycle{}

	err := shutdownServing(context.Background(), grpcServer, otlpServer, dashboardServer, services)

	if !errors.Is(err, wantErr) {
		t.Fatalf("shutdownServing error = %v, want %v", err, wantErr)
	}
	if !grpcServer.graceful.Load() || !otlpServer.shutdown.Load() || !dashboardServer.shutdown.Load() || !services.closed.Load() {
		t.Fatal("shutdownServing did not stop every runtime owner")
	}
}

func TestShutdownServingForcesTransportsAndCancelsServicesAtDeadline(t *testing.T) {
	grpcServer := newFakeGRPCServerLifecycle(true)
	otlpServer := newFakeHTTPServerLifecycle(true, nil)
	dashboardServer := newFakeHTTPServerLifecycle(true, nil)
	services := &fakeServiceLifecycle{waitForContext: true}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := shutdownServing(ctx, grpcServer, otlpServer, dashboardServer, services)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdownServing error = %v, want deadline exceeded", err)
	}
	if !grpcServer.stopped.Load() || !otlpServer.closed.Load() || !dashboardServer.closed.Load() || !services.closed.Load() {
		t.Fatal("shutdownServing did not force every runtime owner to stop")
	}
}
