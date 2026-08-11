package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recordingHTTPClient struct {
	requests []*http.Request
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
