package sqlite_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/gen/agentmetry/v1/agentmetryv1connect"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
	"github.com/kotokumu/agentmetry/internal/transport/connectapi"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/http/httptest"
)

func TestTraceOverviewAndWindowUseFullMetadataBeforeBoundedBodies(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "overview.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	started := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	for _, size := range []int{12, 200, 1200, 5001} {
		traceID := fmt.Sprintf("%032x", size)
		spans := make([]canonical.Span, 0, size)
		for i := range size {
			spanID := fmt.Sprintf("%016x", i+1)
			parentID := ""
			if size == 5001 && i == 0 {
				parentID = fmt.Sprintf("%016x", size)
			}
			if size == 5001 && i == 1 {
				parentID = "ffffffffffffffff"
			}
			if size == 5001 && i == 5000 {
				parentID = "eeeeeeeeeeeeeeee"
			}
			end := started.Add(time.Duration(i+1) * time.Second)
			if size == 12 && i == 0 {
				end = started.Add(5 * time.Second)
			}
			status := "Ok"
			attributes := map[string]any{"success": true}
			if size == 12 && i == 4 {
				status = "Error"
				attributes = map[string]any{"success": false}
			}
			if size == 12 && i == 6 {
				status = ""
				attributes = map[string]any{"success": false}
			}
			spans = append(spans, canonical.Span{Source: "codex", TraceID: traceID, SpanID: spanID, ParentSpanID: parentID,
				Name: "gen_ai.tool.call", Kind: canonical.ActivityTool, ToolName: "exec_command", StartedAt: started.Add(time.Duration(i) * time.Second), EndedAt: end,
				Status: status, Content: "BODY_SENTINEL", Attributes: attributes, Agent: canonical.AgentContext{RunID: fmt.Sprintf("run-%d", size), AgentID: "main"}})
		}
		if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: spans}); err != nil {
			t.Fatal(err)
		}
		overview, err := database.GetTraceOverview(context.Background(), mustTraceID(t, traceID))
		if err != nil {
			t.Fatal(err)
		}
		wantReturned := min(size, query.TraceOverviewLimit)
		wantCoverage := "complete"
		if size > query.TraceOverviewLimit {
			wantCoverage = "partial"
		}
		if overview.TotalActivities != int64(size) || overview.ReturnedActivities != int64(wantReturned) || len(overview.Activities) != wantReturned || overview.Coverage != wantCoverage {
			t.Errorf("size %d overview = %#v", size, overview)
		}
		encoded, err := json.Marshal(overview)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "BODY_SENTINEL") {
			t.Errorf("size %d overview leaked body", size)
		}
		if size == 5001 && (overview.Activities[0].MissingParent || !overview.Activities[1].MissingParent) {
			t.Errorf("parent lookup did not use all retained native IDs: %#v", overview.Activities[:2])
		}
	}
	traceID := fmt.Sprintf("%032x", 12)
	rangeStart, rangeEnd := started.Add(4*time.Second), started.Add(5*time.Second)
	window := query.TraceWindow{StartedAt: &rangeStart, EndedAt: &rangeEnd, Kind: canonical.ActivityTool}
	result, err := database.GetTraceWindow(context.Background(), query.TraceWindowFilter{TraceID: mustTraceID(t, traceID), Window: window, Page: mustPage(t, 0, 2)})
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchingActivities != 4 || len(result.Trace.Activities) != 2 || !result.Trace.HasMore || result.Trace.ActivityCount != 12 {
		t.Errorf("overlap window = %#v", result)
	}
	if result.Trace.Activities[0].Content != "BODY_SENTINEL" {
		t.Errorf("selected detail body unavailable: %#v", result.Trace.Activities[0])
	}
	if result.Trace.Activities[0].MissingParent == nil || *result.Trace.Activities[0].MissingParent {
		t.Errorf("assessed root missing-parent detail = %#v", result.Trace.Activities[0].MissingParent)
	}
	failures, err := database.GetTraceWindow(context.Background(), query.TraceWindowFilter{TraceID: mustTraceID(t, traceID), Window: query.TraceWindow{ErrorsOnly: true}, Page: mustPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	if failures.MatchingActivities != 2 || len(failures.Trace.Activities) != 2 {
		t.Errorf("error window = %#v", failures)
	}
	for _, activity := range failures.Trace.Activities {
		if activity.Status != "error" {
			t.Errorf("error detail status = %q, want canonical error", activity.Status)
		}
	}
	offCap, err := database.GetTraceWindow(context.Background(), query.TraceWindowFilter{TraceID: mustTraceID(t, fmt.Sprintf("%032x", 5001)), Page: mustPage(t, 5000, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(offCap.Trace.Activities) != 1 || offCap.Trace.Activities[0].MissingParent == nil || !*offCap.Trace.Activities[0].MissingParent {
		t.Errorf("off-cap missing parent detail = %#v", offCap.Trace.Activities)
	}
	_, handler := connectapi.New(database, nil, time.Now)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := agentmetryv1connect.NewAgentmetryQueryServiceClient(server.Client(), server.URL)
	overviewWire, err := client.GetTraceOverview(context.Background(), connect.NewRequest(&v1.GetTraceOverviewRequest{TraceId: fmt.Sprintf("%032x", 5001)}))
	if err != nil {
		t.Fatal(err)
	}
	if overviewWire.Msg.TotalActivities != 5001 || overviewWire.Msg.ReturnedActivities != 5000 || overviewWire.Msg.Coverage != "partial" || len(overviewWire.Msg.Activities) != 5000 {
		t.Errorf("wire overview = %#v", overviewWire.Msg)
	}
	windowWire, err := client.GetTraceWindow(context.Background(), connect.NewRequest(&v1.GetTraceWindowRequest{TraceId: traceID, Window: &v1.TraceWindow{StartedAt: timestamppb.New(rangeStart), EndedAt: timestamppb.New(rangeEnd), Kind: "tool"}, Page: &v1.PageRequest{PageSize: 2}}))
	if err != nil {
		t.Fatal(err)
	}
	if windowWire.Msg.MatchingActivities != 4 || len(windowWire.Msg.Trace.Activities) != 2 || !windowWire.Msg.Trace.Page.HasMore || windowWire.Msg.Trace.TotalActivities != 12 {
		t.Errorf("wire window = %#v", windowWire.Msg)
	}
	if windowWire.Msg.Trace.Activities[0].Content != "BODY_SENTINEL" {
		t.Error("wire selected detail body unavailable")
	}
	if windowWire.Msg.Trace.Activities[0].MissingParent == nil || windowWire.Msg.Trace.Activities[0].GetMissingParent() {
		t.Errorf("wire assessed root missing-parent detail = %#v", windowWire.Msg.Trace.Activities[0].MissingParent)
	}
	failureWire, err := client.GetTraceWindow(context.Background(), connect.NewRequest(&v1.GetTraceWindowRequest{TraceId: traceID, Window: &v1.TraceWindow{ErrorsOnly: true}, Page: &v1.PageRequest{PageSize: 100}}))
	if err != nil {
		t.Fatal(err)
	}
	if failureWire.Msg.MatchingActivities != 2 || len(failureWire.Msg.Trace.Activities) != 2 {
		t.Errorf("wire error window = %#v", failureWire.Msg)
	}
	for _, activity := range failureWire.Msg.Trace.Activities {
		if activity.Status != "error" {
			t.Errorf("wire error detail status = %q, want canonical error", activity.Status)
		}
	}
	offCapWire, err := client.GetTraceWindow(context.Background(), connect.NewRequest(&v1.GetTraceWindowRequest{
		TraceId: fmt.Sprintf("%032x", 5001),
		Page:    &v1.PageRequest{PageSize: 1, PageToken: base64.RawURLEncoding.EncodeToString([]byte("agentmetry:v1:5000"))},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(offCapWire.Msg.Trace.Activities) != 1 || offCapWire.Msg.Trace.Activities[0].MissingParent == nil || !offCapWire.Msg.Trace.Activities[0].GetMissingParent() {
		t.Errorf("wire off-cap missing parent detail = %#v", offCapWire.Msg.Trace.Activities)
	}
	for name, request := range map[string]*v1.GetTraceWindowRequest{
		"malformed trace":  {TraceId: "bad"},
		"unsupported kind": {TraceId: traceID, Window: &v1.TraceWindow{Kind: "artifact"}},
		"incomplete range": {TraceId: traceID, Window: &v1.TraceWindow{StartedAt: timestamppb.New(rangeStart)}},
		"oversize page":    {TraceId: traceID, Page: &v1.PageRequest{PageSize: 101}},
	} {
		if _, err := client.GetTraceWindow(context.Background(), connect.NewRequest(request)); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s error = %v", name, err)
		}
	}
	if _, err := client.GetTraceOverview(context.Background(), connect.NewRequest(&v1.GetTraceOverviewRequest{TraceId: strings.Repeat("f", 32)})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("missing overview error = %v", err)
	}
}
