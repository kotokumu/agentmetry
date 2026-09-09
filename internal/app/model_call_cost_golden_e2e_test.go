//go:build integration

package app_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/gen/agentmetry/v1/agentmetryv1connect"
	"github.com/kotokumu/agentmetry/internal/app"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
	webassets "github.com/kotokumu/agentmetry/web"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
)

const goldenSessionID = "00000000-0000-4000-8000-000000000001"
const goldenTraceID = "00000000000000000000000000000001"

func TestAnonymizedProviderGoldenFixturesEndToEnd(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	now := time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC)
	services := app.NewServices(database, webassets.FS(), func() time.Time { return now })
	otlpServer := httptest.NewServer(services.OTLPHTTPHandler)
	t.Cleanup(otlpServer.Close)
	dashboardServer := httptest.NewServer(services.Dashboard)
	t.Cleanup(dashboardServer.Close)

	claude := readGoldenLogFixture(t, "claude-api-request.json", "api_request", "")
	codex := readGoldenLogFixture(t, "codex-response-completed.json", "codex.sse_event", "event otel/src/events/session_telemetry.rs:")
	postGoldenOTLPJSON(t, otlpServer.URL+"/v1/logs", claude)
	postGoldenOTLPJSON(t, otlpServer.URL+"/v1/logs", codex)

	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(http.DefaultClient, dashboardServer.URL)
	assertGoldenSessionCost(t, client, "claude", 8_683, v1.CallCostBasis_CALL_COST_BASIS_PROVIDER_REPORTED)
	assertGoldenSessionCost(t, client, "codex", 1_587, v1.CallCostBasis_CALL_COST_BASIS_RATE_CARD_ESTIMATE)

	trace, err := client.GetTrace(context.Background(), connect.NewRequest(&v1.GetTraceRequest{TraceId: goldenTraceID}))
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenAggregateCost(t, "trace", trace.Msg.GetCostSummary(), 2)

	dashboard, err := client.GetDashboard(context.Background(), connect.NewRequest(&v1.GetDashboardRequest{
		Filter: &v1.TimeFilter{Range: v1.TimeRange_TIME_RANGE_ONE_DAY},
	}))
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenAggregateCost(t, "dashboard", dashboard.Msg.GetDashboard().GetCostSummary(), 2)
}

func assertGoldenAggregateCost(t *testing.T, label string, cost *v1.CostSummary, wantCalls int64) {
	t.Helper()
	if cost.GetAmountMicroUsd() != 10_270 || cost.GetBasis() != v1.CostSummaryBasis_COST_SUMMARY_BASIS_MIXED || cost.GetCoverage() != v1.CostCoverage_COST_COVERAGE_COMPLETE || cost.GetEligibleCalls() != wantCalls || cost.GetPricedCalls() != wantCalls {
		t.Fatalf("golden %s cost = %#v", label, cost)
	}
}

func assertGoldenSessionCost(t *testing.T, client agentmetryv1connect.AgentmetryQueryServiceClient, source string, wantAmount int64, wantBasis v1.CallCostBasis) {
	t.Helper()
	response, err := client.GetSession(context.Background(), connect.NewRequest(&v1.GetSessionRequest{SourceId: source, SessionId: goldenSessionID}))
	if err != nil {
		t.Fatal(err)
	}
	cost := response.Msg.GetSession().GetCostSummary()
	if cost.GetAmountMicroUsd() != wantAmount || cost.GetCoverage() != v1.CostCoverage_COST_COVERAGE_COMPLETE || cost.GetEligibleCalls() != 1 || cost.GetPricedCalls() != 1 {
		t.Fatalf("%s golden session cost = %#v", source, cost)
	}
	activities, err := client.ListSessionActivities(context.Background(), connect.NewRequest(&v1.ListSessionActivitiesRequest{
		SourceId: source, SessionId: goldenSessionID, Page: &v1.PageRequest{PageSize: 10},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(activities.Msg.GetActivities()) != 1 {
		t.Fatalf("%s golden activities = %d, want 1", source, len(activities.Msg.GetActivities()))
	}
	callCost := activities.Msg.GetActivities()[0].GetModelCallCost()
	if callCost == nil || callCost.GetAmountMicroUsd() != wantAmount || callCost.GetBasis() != wantBasis {
		t.Fatalf("%s golden call cost = %#v", source, callCost)
	}
}

func readGoldenLogFixture(t *testing.T, name, semanticEvent, nativeEventPrefix string) []byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("testdata", "otlp", name))
	if err != nil {
		t.Fatal(err)
	}
	assertGoldenFixtureAnonymized(t, payload)
	request := plogotlp.NewExportRequest()
	if err := request.UnmarshalJSON(payload); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	resources := request.Logs().ResourceLogs()
	if resources.Len() != 1 || resources.At(0).ScopeLogs().Len() != 1 || resources.At(0).ScopeLogs().At(0).LogRecords().Len() != 1 {
		t.Fatalf("%s must contain exactly one captured log record", name)
	}
	record := resources.At(0).ScopeLogs().At(0).LogRecords().At(0)
	event, ok := record.Attributes().Get("event.name")
	semanticEventName := ""
	if ok && event.Type() == pcommon.ValueTypeStr {
		semanticEventName = event.Str()
	}
	if semanticEventName != semanticEvent || !strings.HasPrefix(record.EventName(), nativeEventPrefix) {
		t.Fatalf("%s event identity = native %q semantic %q", name, record.EventName(), semanticEventName)
	}
	if record.Body().Type() != pcommon.ValueTypeStr || record.Body().Str() != "[REDACTED]" {
		t.Fatalf("%s body was not redacted", name)
	}
	if record.TraceID().String() != goldenTraceID || record.SpanID().String() != "0000000000000001" {
		t.Fatalf("%s trace identity was not replaced", name)
	}
	return payload
}

func assertGoldenFixtureAnonymized(t *testing.T, payload []byte) {
	t.Helper()
	withoutReservedEmail := bytes.ReplaceAll(payload, []byte("fixture-user@example.invalid"), nil)
	for _, forbidden := range [][]byte{
		[]byte("/Users/"), []byte("/home/"), []byte("/private/var/"), []byte(`C:\\Users\\`),
		[]byte("kaneko"), []byte("@"), []byte("Bearer "), []byte("sk-"), []byte("ghp_"),
		[]byte("github_pat_"), []byte("PRIVATE KEY"), []byte("Telemetry fixture capture"), []byte("Release Please"),
	} {
		if bytes.Contains(withoutReservedEmail, forbidden) {
			t.Fatalf("golden fixture contains forbidden non-anonymized content %q", forbidden)
		}
	}
}

func postGoldenOTLPJSON(t *testing.T, endpoint string, payload []byte) {
	t.Helper()
	response, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("POST %s returned %s: %s", endpoint, response.Status, body)
	}
}
