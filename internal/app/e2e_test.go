//go:build integration

package app_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	_ "modernc.org/sqlite"

	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/gen/agentmetry/v1/agentmetryv1connect"
	"github.com/kotokumu/agentmetry/internal/app"
	"github.com/kotokumu/agentmetry/internal/query"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
	webassets "github.com/kotokumu/agentmetry/web"
)

func TestOTLPToSQLiteDashboardAndMCPEndToEnd(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "agentmetry.db")
	database, err := store.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	services := app.NewServices(database, webassets.FS(), func() time.Time { return now })
	otlpServer := httptest.NewServer(services.OTLPHTTPHandler)
	t.Cleanup(otlpServer.Close)
	dashboardServer := httptest.NewServer(services.Dashboard)
	t.Cleanup(dashboardServer.Close)

	postOTLP(t, otlpServer.URL+"/v1/traces", traceFixture(now))
	postOTLP(t, otlpServer.URL+"/v1/logs", logFixture(now))
	postOTLP(t, otlpServer.URL+"/v1/metrics", metricFixture(now))

	overview := getOverview(t, dashboardServer.URL)
	assertOverview(t, overview)
	assertTrace(t, dashboardServer.URL)
	assertMCPOverview(t, dashboardServer.URL)
	assertMCPAgentContext(t, dashboardServer.URL)
	assertConnectQueryContract(t, dashboardServer.URL)
	assertDashboard(t, dashboardServer.URL)

	journal, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	var exportCount, observationCount int
	if err := journal.QueryRow("SELECT COUNT(*) FROM otlp_exports").Scan(&exportCount); err != nil {
		t.Fatal(err)
	}
	if err := journal.QueryRow("SELECT COUNT(*) FROM observations").Scan(&observationCount); err != nil {
		t.Fatal(err)
	}
	if exportCount != 3 || observationCount != 6 {
		t.Fatalf("canonical storage counts = exports:%d observations:%d, want exports:3 observations:6", exportCount, observationCount)
	}
}

func TestMCPAgentSelfAnalysisFlow(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "agentmetry.db")
	database, err := store.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	services := app.NewServices(database, webassets.FS(), func() time.Time { return now })
	otlpServer := httptest.NewServer(services.OTLPHTTPHandler)
	t.Cleanup(otlpServer.Close)
	mcpServer := httptest.NewServer(services.Dashboard)
	t.Cleanup(mcpServer.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "agentmetry-integration-test", Version: "v1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: mcpServer.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })

	postOTLP(t, otlpServer.URL+"/v1/traces", traceFixture(now))
	postOTLP(t, otlpServer.URL+"/v1/logs", logFixture(now))
	postOTLP(t, otlpServer.URL+"/v1/metrics", metricFixture(now))

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	toolNames := make(map[string]bool, len(tools.Tools))
	for _, tool := range tools.Tools {
		toolNames[tool.Name] = true
	}
	for _, required := range []string{"get_agent_context", "get_source_capabilities", "list_runs", "get_run_context"} {
		if !toolNames[required] {
			t.Fatalf("tools/list omitted %q", required)
		}
	}

	contextResult := callMCPToolWithSession(t, session, "get_agent_context", map[string]any{})
	t.Logf("get_agent_context: %s", compactJSON(contextResult))
	callerIdentity, _ := contextResult["callerIdentity"].(map[string]any)
	if contextResult["contractVersion"] != "v1" || callerIdentity["available"] != false {
		t.Fatalf("unexpected agent context contract: %s", compactJSON(contextResult))
	}
	capabilitiesResult := callMCPToolWithSession(t, session, "get_source_capabilities", map[string]any{})
	t.Logf("get_source_capabilities: %s", compactJSON(capabilitiesResult))
	capabilities, _ := capabilitiesResult["capabilities"].([]any)
	if len(capabilities) != 7 {
		t.Fatalf("source capabilities = %d, want 7: %s", len(capabilities), compactJSON(capabilitiesResult))
	}

	runsResult := callMCPToolWithSession(t, session, "list_runs", map[string]any{"range": "24h"})
	t.Logf("list_runs: %s", compactJSON(runsResult))
	runs := structuredRuns(t, runsResult)
	if len(runs) != 2 {
		t.Fatalf("list_runs returned %d runs, want 2", len(runs))
	}
	expected := map[string]struct {
		activityCount int
		inputTokens   int
		outputTokens  int
		bottlenecks   int
	}{
		"child-session":  {activityCount: 1, inputTokens: 5, outputTokens: 1},
		"parent-session": {activityCount: 3, inputTokens: 10, outputTokens: 2, bottlenecks: 1},
	}

	for _, run := range runs {
		want, ok := expected[run.RunID]
		if !ok {
			t.Fatalf("unexpected run %q", run.RunID)
		}
		args := map[string]any{"source": run.Source, "runId": run.RunID}
		contextResult := callMCPToolWithSession(t, session, "get_run_context", args)
		contextValue := requireObject(t, contextResult, "context")
		if contextValue["sourceId"] != run.Source || contextValue["runId"] != run.RunID {
			t.Fatalf("unexpected run context: %s", compactJSON(contextResult))
		}

		summaryResult := callMCPToolWithSession(t, session, "get_run_summary", args)
		summaryRun := requireObject(t, summaryResult, "run")
		if numberField(t, summaryRun, "activityCount") != want.activityCount {
			t.Fatalf("unexpected run summary: %s", compactJSON(summaryResult))
		}

		tokensResult := callMCPToolWithSession(t, session, "get_token_usage", args)
		total := requireObject(t, tokensResult, "total")
		if tokensResult["sourceId"] != run.Source || tokensResult["runId"] != run.RunID || numberField(t, total, "input") != want.inputTokens || numberField(t, total, "output") != want.outputTokens {
			t.Fatalf("unexpected token usage: %s", compactJSON(tokensResult))
		}

		bottlenecks := callMCPToolWithSession(t, session, "find_bottlenecks", args)
		if len(requireArray(t, bottlenecks, "findings")) != want.bottlenecks {
			t.Fatalf("unexpected bottlenecks: %s", compactJSON(bottlenecks))
		}
		coordination := callMCPToolWithSession(t, session, "find_coordination_risks", args)
		if len(requireArray(t, coordination, "findings")) != 0 {
			t.Fatalf("unexpected coordination findings: %s", compactJSON(coordination))
		}
		timeline := callMCPToolWithSession(t, session, "get_run_timeline", map[string]any{
			"source": run.Source, "runId": run.RunID, "pageSize": 100,
		})
		t.Logf("get_run_timeline(%s/%s): %s", run.Source, run.RunID, compactJSON(timeline))
		activities := requireArray(t, timeline, "activities")
		if timeline["source"] != run.Source || timeline["sessionId"] != run.RunID || len(activities) != want.activityCount {
			t.Fatalf("unexpected timeline for %s/%s: %s", run.Source, run.RunID, compactJSON(timeline))
		}
	}

	compare := callMCPToolWithSession(t, session, "compare_runs", map[string]any{
		"runs": []map[string]string{{"source": runs[0].Source, "runId": runs[0].RunID}, {"source": runs[1].Source, "runId": runs[1].RunID}},
	})
	t.Logf("compare_runs: %s", compactJSON(compare))
	comparedRuns := requireArray(t, compare, "runs")
	dimensions := requireArray(t, compare, "dimensions")
	if len(comparedRuns) != 2 || len(dimensions) != 5 {
		t.Fatalf("unexpected comparison: %s", compactJSON(compare))
	}
}

func requireObject(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()
	object, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("%q is not an object: %s", key, compactJSON(value))
	}
	return object
}

func requireArray(t *testing.T, value map[string]any, key string) []any {
	t.Helper()
	items, ok := value[key].([]any)
	if !ok {
		t.Fatalf("%q is not an array: %s", key, compactJSON(value))
	}
	return items
}

func numberField(t *testing.T, value map[string]any, key string) int {
	t.Helper()
	number, ok := value[key].(float64)
	if !ok {
		t.Fatalf("%q is not a number: %s", key, compactJSON(value))
	}
	return int(number)
}

type mcpRunReference struct {
	Source string
	RunID  string
}

func callMCPToolWithSession(t *testing.T, session *mcp.ClientSession, name string, arguments map[string]any) map[string]any {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("MCP %s returned a tool error: %s", name, compactJSON(result.Content))
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var structured map[string]any
	if err := json.Unmarshal(encoded, &structured); err != nil {
		t.Fatal(err)
	}
	if structured == nil {
		t.Fatalf("MCP %s omitted structured content", name)
	}
	return structured
}

func structuredRuns(t *testing.T, result map[string]any) []mcpRunReference {
	t.Helper()
	overview, ok := result["overview"].(map[string]any)
	if !ok {
		t.Fatalf("list_runs omitted overview: %s", compactJSON(result))
	}
	sessions, ok := overview["sessions"].([]any)
	if !ok {
		t.Fatalf("list_runs omitted sessions: %s", compactJSON(result))
	}
	runs := make([]mcpRunReference, 0, len(sessions))
	for _, raw := range sessions {
		session, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("list_runs returned malformed session: %s", compactJSON(raw))
		}
		source, _ := session["sourceId"].(string)
		runID, _ := session["id"].(string)
		if source == "" || runID == "" {
			t.Fatalf("list_runs returned incomplete source-qualified session: %s", compactJSON(session))
		}
		runs = append(runs, mcpRunReference{Source: source, RunID: runID})
	}
	return runs
}

func compactJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("<json error: %v>", err)
	}
	return string(encoded)
}

func assertConnectQueryContract(t *testing.T, baseURL string) {
	t.Helper()
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(http.DefaultClient, baseURL)
	dashboard, err := client.GetDashboard(context.Background(), connect.NewRequest(&v1.GetDashboardRequest{
		Filter: &v1.TimeFilter{Range: v1.TimeRange_TIME_RANGE_ONE_DAY},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Msg.GetDashboard().GetRunCount() != 2 {
		t.Fatalf("unexpected Connect dashboard: %#v", dashboard.Msg.GetDashboard())
	}
	sessions, err := client.ListSessions(context.Background(), connect.NewRequest(&v1.ListSessionsRequest{
		Filter: &v1.TimeFilter{Range: v1.TimeRange_TIME_RANGE_ONE_DAY},
		Page:   &v1.PageRequest{PageSize: 10},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions.Msg.GetSessions()) != 2 || sessions.Msg.GetSessions()[0].GetAgentCount() == 0 {
		t.Fatalf("unexpected Connect sessions: %#v", sessions.Msg.GetSessions())
	}
	first := sessions.Msg.GetSessions()[0]
	detail, err := client.GetSession(context.Background(), connect.NewRequest(&v1.GetSessionRequest{SourceId: first.GetSourceId(), SessionId: first.GetId()}))
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Msg.GetSession().GetAgents()) == 0 {
		t.Fatalf("session detail omitted agent topology: %#v", detail.Msg.GetSession())
	}
	activities, err := client.ListSessionActivities(context.Background(), connect.NewRequest(&v1.ListSessionActivitiesRequest{
		SourceId: first.GetSourceId(), SessionId: first.GetId(), Page: &v1.PageRequest{PageSize: 1},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if activities.Msg.GetTotal() != first.GetActivityCount() || len(activities.Msg.GetActivities()) > 1 {
		t.Fatalf("unexpected bounded Connect activities: %#v", activities.Msg)
	}
}

type protoMarshaler interface {
	MarshalProto() ([]byte, error)
}

func postOTLP(t *testing.T, endpoint string, request protoMarshaler) {
	t.Helper()
	payload, err := request.MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(endpoint, "application/x-protobuf", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("POST %s returned %s: %s", endpoint, response.Status, body)
	}
}

func getOverview(t *testing.T, baseURL string) query.Overview {
	t.Helper()
	response, err := http.Get(baseURL + "/api/v1/overview?range=24h")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope struct {
		Version  string         `json:"version"`
		Overview query.Overview `json:"overview"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || envelope.Version != "v1" {
		t.Fatalf("unexpected overview response: status=%s version=%q", response.Status, envelope.Version)
	}
	return envelope.Overview
}

func assertOverview(t *testing.T, overview query.Overview) {
	t.Helper()
	if overview.SignalCounts != (query.SignalCounts{Traces: 1, Logs: 4, Metrics: 1}) {
		t.Fatalf("unexpected signal counts: %#v", overview.SignalCounts)
	}
	if len(overview.Sessions) != 2 || overview.AgentCount != 2 {
		t.Fatalf("unexpected sessions or agents: %#v", overview)
	}
	byID := make(map[string]query.Session, len(overview.Sessions))
	for _, session := range overview.Sessions {
		byID[session.ID] = session
	}
	parent, child := byID["parent-session"], byID["child-session"]
	if parent.Tokens.Total() != 12 || parent.CostUSD == nil || child.Tokens.Total() != 6 {
		t.Fatalf("unexpected conversation summaries: parent=%#v child=%#v", parent, child)
	}
	if len(parent.TraceIDs) != 1 || len(child.TraceIDs) != 1 || parent.TraceIDs[0] != traceID.String() || child.TraceIDs[0] != traceID.String() {
		t.Fatalf("unexpected trace correlation: parent=%#v child=%#v", parent.TraceIDs, child.TraceIDs)
	}
	var foundDelegation, foundChild bool
	for _, activity := range parent.Activities {
		if activity.Kind == "delegation" {
			foundDelegation = activity.TargetAgentType == "explorer" && strings.Contains(activity.Content, "Instruction content unavailable")
		}
	}
	for _, activity := range child.Activities {
		if activity.RunID == "child-session" && activity.Kind == "response" {
			foundChild = activity.Model == "gpt-child" && activity.Tokens.Total() == 6
		}
	}
	if !foundDelegation || !foundChild {
		t.Fatalf("missing delegation or child response: parent=%#v child=%#v", parent.Activities, child.Activities)
	}
}

func assertTrace(t *testing.T, baseURL string) {
	t.Helper()
	response, err := http.Get(baseURL + "/api/v1/traces/" + traceID.String())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope struct {
		Version string      `json:"version"`
		Trace   query.Trace `json:"trace"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || envelope.Version != "v1" {
		t.Fatalf("unexpected trace response: status=%s version=%q", response.Status, envelope.Version)
	}
	if envelope.Trace.TraceID != traceID.String() || envelope.Trace.RootSpanCount != 1 || len(envelope.Trace.Conversations) != 2 {
		t.Fatalf("unexpected trace correlation: %#v", envelope.Trace)
	}
}

func assertMCPOverview(t *testing.T, baseURL string) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_agent_sessions","arguments":{"range":"24h"}}}`
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload struct {
		Result struct {
			StructuredContent struct {
				Overview struct {
					RunCount   int64 `json:"runCount"`
					AgentCount int64 `json:"agentCount"`
				} `json:"overview"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || payload.Result.StructuredContent.Overview.RunCount != 2 || payload.Result.StructuredContent.Overview.AgentCount != 2 {
		t.Fatalf("unexpected MCP overview: status=%s payload=%#v", response.Status, payload)
	}
}

func assertMCPAgentContext(t *testing.T, baseURL string) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_agent_context","arguments":{}}}`
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var payload struct {
		Result struct {
			StructuredContent struct {
				CallerIdentity struct {
					Available bool `json:"available"`
				} `json:"callerIdentity"`
				RunIdentity struct {
					LatestIsImplicit bool `json:"latestIsImplicit"`
				} `json:"runIdentity"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || payload.Result.StructuredContent.CallerIdentity.Available || payload.Result.StructuredContent.RunIdentity.LatestIsImplicit {
		t.Fatalf("MCP caller context must require explicit identity: status=%s payload=%#v", response.Status, payload)
	}
}

func assertDashboard(t *testing.T, baseURL string) {
	t.Helper()
	response, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("<am-app>")) {
		t.Fatalf("dashboard was not served: status=%s", response.Status)
	}
}

var (
	traceID  = pcommon.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	rootSpan = pcommon.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
)

func traceFixture(now time.Time) ptraceotlp.ExportRequest {
	data := ptrace.NewTraces()
	resource := data.ResourceSpans().AppendEmpty()
	resource.Resource().Attributes().PutStr("gen_ai.conversation.id", "parent-session")
	span := resource.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetTraceID(traceID)
	span.SetSpanID(rootSpan)
	span.SetName("gen_ai.response.completed")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(now.Add(-5 * time.Second)))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(-4 * time.Second)))
	span.Attributes().PutStr("gen_ai.agent.id", "main")
	span.Attributes().PutStr("gen_ai.agent.type", "root")
	span.Attributes().PutStr("gen_ai.request.model", "gpt-parent")
	span.Attributes().PutInt("gen_ai.usage.input_tokens", 10)
	span.Attributes().PutInt("gen_ai.usage.output_tokens", 2)
	span.Attributes().PutDouble("cost_usd", 0.01)
	return ptraceotlp.NewExportRequestFromTraces(data)
}

func logFixture(now time.Time) plogotlp.ExportRequest {
	data := plog.NewLogs()
	records := data.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords()
	appendLog(records, now.Add(-6*time.Second), "parent-session", traceID, "gen_ai.user_prompt", map[string]string{"prompt": "Delegate the task"})
	appendLog(records, now.Add(-3*time.Second), "parent-session", traceID, "gen_ai.tool.result", map[string]string{
		"gen_ai.tool.name":         "spawn_agent",
		"gen_ai.agent.target.id":   "/root/inspect",
		"gen_ai.agent.target.type": "explorer",
		"content":                  "Instruction content unavailable in source telemetry",
	})
	appendLog(records, now.Add(-2*time.Second), "child-session", traceID, "gen_ai.session.start", nil)
	appendLog(records, now.Add(-time.Second), "child-session", pcommon.TraceID{}, "gen_ai.response.completed", map[string]string{
		"gen_ai.request.model":       "gpt-child",
		"gen_ai.usage.input_tokens":  "5",
		"gen_ai.usage.output_tokens": "1",
	})
	return plogotlp.NewExportRequestFromLogs(data)
}

func appendLog(records plog.LogRecordSlice, observedAt time.Time, sessionID string, eventTraceID pcommon.TraceID, eventName string, attributes map[string]string) {
	record := records.AppendEmpty()
	record.SetTimestamp(pcommon.NewTimestampFromTime(observedAt))
	record.SetTraceID(eventTraceID)
	record.SetEventName(eventName)
	record.Attributes().PutStr("gen_ai.conversation.id", sessionID)
	for key, value := range attributes {
		record.Attributes().PutStr(key, value)
	}
}

func metricFixture(now time.Time) pmetricotlp.ExportRequest {
	data := pmetric.NewMetrics()
	metric := data.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	metric.SetName("gen_ai.session.active")
	point := metric.SetEmptyGauge().DataPoints().AppendEmpty()
	point.SetTimestamp(pcommon.NewTimestampFromTime(now))
	point.SetIntValue(1)
	point.Attributes().PutStr("gen_ai.conversation.id", "parent-session")
	return pmetricotlp.NewExportRequestFromMetrics(data)
}
