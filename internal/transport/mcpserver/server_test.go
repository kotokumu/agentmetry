package mcpserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/product"
	"github.com/kotokumu/agentmetry/internal/query"
)

type readerStub struct {
	dashboardFilter query.DashboardFilter
	sessionFilter   query.SessionListFilter
	sessionPage     query.SessionPage
	activityFilter  query.ActivityPageFilter
	activityPage    query.ActivityPage
	traceFilter     query.TraceFilter
	trace           query.Trace
	traceErr        error
	summary         query.Session
	summaryErr      error
}

func testService(reader *readerStub, now Clock) *Service {
	return &Service{dashboardReader: reader, sessionReader: reader, summaryReader: reader, activityReader: reader, traceReader: reader, now: now}
}

func TestImplementationInfoUsesReleasedProductMetadata(t *testing.T) {
	info := implementationInfo()

	if info.Name != product.ID || info.Title != product.Name || info.Version != product.Version {
		t.Fatalf("unexpected MCP implementation metadata: %#v", info)
	}
}

func (reader *readerStub) GetDashboard(_ context.Context, filter query.DashboardFilter) (query.Overview, error) {
	reader.dashboardFilter = filter
	return query.Overview{SignalCounts: query.SignalCounts{Traces: 4}}, nil
}

func (reader *readerStub) ListSessions(_ context.Context, filter query.SessionListFilter) (query.SessionPage, error) {
	reader.sessionFilter = filter
	return reader.sessionPage, nil
}

func (reader *readerStub) GetSessionSummary(_ context.Context, _ query.ConversationIdentity) (query.Session, error) {
	return reader.summary, reader.summaryErr
}

func (reader *readerStub) ListSessionActivities(_ context.Context, filter query.ActivityPageFilter) (query.ActivityPage, error) {
	reader.activityFilter = filter
	return reader.activityPage, nil
}

func (reader *readerStub) GetTrace(_ context.Context, filter query.TraceFilter) (query.Trace, error) {
	reader.traceFilter = filter
	return reader.trace, reader.traceErr
}

func TestGetOverviewUsesSharedRangeAndQueryContract(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	reader := &readerStub{}
	service := testService(reader, func() time.Time { return now })

	_, output, err := service.getOverview(context.Background(), nil, OverviewInput{Range: "7d", Source: "claude", Search: "review"})

	if err != nil {
		t.Fatal(err)
	}
	if !reader.dashboardFilter.Since.Equal(now.Add(-7 * 24 * time.Hour)) {
		t.Fatalf("unexpected filter: %#v", reader.dashboardFilter)
	}
	if reader.dashboardFilter.SourceID != "claude" || reader.dashboardFilter.Search != "review" {
		t.Fatalf("dashboard filters were not forwarded: %#v", reader.dashboardFilter)
	}
	if reader.sessionFilter.SourceID != "claude" || reader.sessionFilter.Search != "review" || reader.sessionFilter.Page.Size() != 100 {
		t.Fatalf("session filters were not forwarded: %#v", reader.sessionFilter)
	}
	if output.Overview.SignalCounts.Traces != 4 {
		t.Fatalf("unexpected output: %#v", output)
	}
	conditions := query.SessionConditions{ObservedFailure: true, MinDurationMS: new(10.25), MaxDurationMS: new(200.5), Model: "fixture-model", Tool: "exec_command"}
	reader.sessionPage.AppliedConditions = &conditions
	_, filtered, err := service.getOverview(context.Background(), nil, OverviewInput{Range: "7d", Source: "codex", Search: "needle", Conditions: conditions})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(conditions, reader.sessionFilter.Conditions); diff != "" {
		t.Errorf("conditions not forwarded: %s", diff)
	}
	if diff := cmp.Diff(&conditions, filtered.AppliedConditions); diff != "" {
		t.Errorf("applied conditions mismatch: %s", diff)
	}
	reader.sessionPage.AppliedConditions = nil
	_, unacknowledged, err := service.getOverview(context.Background(), nil, OverviewInput{Conditions: conditions})
	if err != nil {
		t.Fatal(err)
	}
	if unacknowledged.AppliedConditions != nil {
		t.Error("adapter invented condition acknowledgement")
	}
	_, _, err = service.getOverview(context.Background(), nil, OverviewInput{Conditions: query.SessionConditions{MinDurationMS: new(2.0), MaxDurationMS: new(1.0)}})
	if err == nil {
		t.Error("inverted condition range succeeded")
	}
}

func TestGetTraceReturnsCompleteTraceAndPreservesTypedUsage(t *testing.T) {
	const traceID = "11111111111111111111111111111111"
	reader := &readerStub{trace: query.Trace{
		TraceID: traceID,
		Activities: []query.Activity{{
			TraceID: traceID,
			Tokens:  canonical.TokenUsage{Input: 12, Presence: canonical.TokenPresence{Output: true}},
		}},
	}}
	service := testService(reader, time.Now)

	_, output, err := service.getTrace(context.Background(), nil, TraceInput{TraceID: traceID})

	if err != nil {
		t.Fatal(err)
	}
	if reader.traceFilter.TraceID.String() != traceID || reader.traceFilter.Page.Size() != 100 {
		t.Fatalf("unexpected trace filter: %#v", reader.traceFilter)
	}
	if output.Trace.TraceID != traceID || output.Trace.Activities[0].Tokens.Input == nil || *output.Trace.Activities[0].Tokens.Input != 12 {
		t.Fatalf("unexpected output: %#v", output)
	}
	if output.Trace.Activities[0].Tokens.Output == nil || *output.Trace.Activities[0].Tokens.Output != 0 {
		t.Fatalf("reported zero output was lost: %#v", output.Trace.Activities[0].Tokens)
	}
	if output.Trace.Conversations == nil || output.Trace.Agents == nil {
		t.Fatalf("trace collections must be non-nil arrays: %#v", output.Trace)
	}

	reader.trace.Activities[0].Content = "captured body"
	_, output, err = service.getTrace(context.Background(), nil, TraceInput{
		TraceID: traceID, AnchorSpanID: "ABCDEFABCDEFABCD", PageSize: 3, PageToken: encodePageToken(110),
	})
	if err != nil {
		t.Fatal(err)
	}
	if reader.traceFilter.SpanID.String() != "abcdefabcdefabcd" || reader.traceFilter.Page.Size() != 3 || reader.traceFilter.Page.Offset() != 110 {
		t.Errorf("native span anchor mapping: %#v", reader.traceFilter)
	}
	if output.Trace.Activities[0].Content != "" || output.Trace.Activities[0].ContentState != "not_returned" {
		t.Errorf("anchored read changed content default: %#v", output.Trace.Activities[0])
	}
	reader.traceErr = query.ErrTraceTargetNotFound
	_, _, err = service.getTrace(context.Background(), nil, TraceInput{TraceID: traceID, AnchorSpanID: "abcdefabcdefabcd"})
	if !errors.Is(err, query.ErrTraceTargetNotFound) {
		t.Errorf("target-not-found was not preserved: %v", err)
	}
}

func TestGetSessionActivitiesIsBoundedAndUsesOpaqueContinuation(t *testing.T) {
	reader := &readerStub{activityPage: query.ActivityPage{
		Offset: 5, Total: 9, HasEarlier: true, HasMore: true,
		Activities: []query.Activity{{Name: "tool call"}},
	}}
	service := testService(reader, time.Now)

	_, output, err := service.getSessionActivities(context.Background(), nil, SessionActivitiesInput{
		Source: "codex", SessionID: "run-1", PageSize: 4, PageToken: encodePageToken(5), Direction: "older",
	})

	if err != nil {
		t.Fatal(err)
	}
	if reader.activityFilter.Identity.SourceID() != "codex" || reader.activityFilter.Identity.ConversationID() != "run-1" || reader.activityFilter.Page.Size() != 4 || reader.activityFilter.Page.Offset() != 5 {
		t.Fatalf("unexpected activity filter: %#v", reader.activityFilter)
	}
	if output.Total != 9 || len(output.Activities) != 1 || output.NextPageToken != encodePageToken(6) || !output.HasEarlier || !output.HasMore {
		t.Fatalf("unexpected activity output: %#v", output)
	}
}

func TestGetTraceRejectsInvalidOTLPIdentity(t *testing.T) {
	service := testService(&readerStub{}, time.Now)

	_, _, err := service.getTrace(context.Background(), nil, TraceInput{TraceID: "not-a-trace-id"})

	if err == nil {
		t.Fatal("expected invalid trace ID error")
	}

	for _, input := range []TraceInput{
		{TraceID: "11111111111111111111111111111111", AnchorSpanID: "not-a-span"},
		{TraceID: "11111111111111111111111111111111", AnchorSpanID: "0000000000000000"},
		{TraceID: "11111111111111111111111111111111", AnchorSpanID: "ABCDEFABCDEFABCD", PageSize: 101},
	} {
		reader := &readerStub{}
		_, _, err := testService(reader, time.Now).getTrace(context.Background(), nil, input)
		if err == nil || reader.traceFilter.TraceID.String() != "" {
			t.Errorf("invalid anchor input reached reader or missing error: %#v %v", input, err)
		}
	}
}

func TestGetAgentContextDoesNotClaimCallerIdentity(t *testing.T) {
	service := testService(&readerStub{}, time.Now)

	_, output, err := service.getAgentContext(context.Background(), nil, AgentContextInput{})

	if err != nil {
		t.Fatal(err)
	}
	if output.CallerIdentity.Available || output.RunIdentity.LatestIsImplicit {
		t.Fatalf("caller identity must be explicit: %#v", output)
	}
	if len(output.Workflow) == 0 || len(output.Tools) == 0 {
		t.Fatalf("context contract is not discoverable: %#v", output)
	}
}

func TestGetRunSummaryUsesExplicitIdentityAndReportsDerivedCompleteness(t *testing.T) {
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	reader := &readerStub{
		summary:      query.Session{ID: "run-1", SourceID: "codex", StartedAt: start, EndedAt: start.Add(10 * time.Second), ActivityCount: 1, Tokens: canonical.TokenUsage{Input: 10, Output: 5, Presence: canonical.TokenPresence{Input: true, Output: true}}},
		activityPage: query.ActivityPage{Activities: []query.Activity{{Source: "codex", RunID: "run-1", Name: "tool", StartedAt: start, EndedAt: start.Add(2 * time.Second)}}},
	}
	service := testService(reader, time.Now)

	_, output, err := service.getRunSummary(context.Background(), nil, RunContextInput{Source: "codex", RunID: "run-1"})

	if err != nil {
		t.Fatal(err)
	}
	if output.Run.SourceID != "codex" || output.Run.ID != "run-1" || output.Efficiency.WallDurationMs != 10000 || output.Efficiency.ActiveDurationMs != 2000 {
		t.Fatalf("unexpected run summary: %#v", output)
	}
	if output.Metadata.SourceCompleteness != "observed_projection_complete" || output.Metadata.SourceCoverage != "unknown" || output.Metadata.Confidence != "heuristic" {
		t.Fatalf("unexpected analysis metadata: %#v", output.Metadata)
	}
}

func TestAnalyzeReworkReturnsSessionMetricsAndUnsupportedCapabilities(t *testing.T) {
	start := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	reader := &readerStub{
		summary: query.Session{ID: "run-1", SourceID: "codex", ActivityCount: 3},
		activityPage: query.ActivityPage{Activities: []query.Activity{
			{Source: "codex", RunID: "run-1", Signal: canonical.SignalTrace, TraceID: "trace", SpanID: "fail", Name: "exec_command", ToolName: "exec_command", Kind: canonical.ActivityTool, AgentID: "main", StartedAt: start, EndedAt: start.Add(time.Second), ObservedAt: start.Add(time.Second), Attributes: map[string]any{"command": "go test ./...", "exit_code": 1}},
			{Source: "codex", RunID: "run-1", Signal: canonical.SignalTrace, TraceID: "trace", SpanID: "edit", Name: "apply_patch", ToolName: "apply_patch", Kind: canonical.ActivityTool, AgentID: "main", StartedAt: start.Add(2 * time.Second), EndedAt: start.Add(3 * time.Second), ObservedAt: start.Add(3 * time.Second), Attributes: map[string]any{"file_path": "main.go", "success": true}},
			{Source: "codex", RunID: "run-1", Signal: canonical.SignalTrace, TraceID: "trace", SpanID: "retry", Name: "exec_command", ToolName: "exec_command", Kind: canonical.ActivityTool, AgentID: "main", StartedAt: start.Add(4 * time.Second), EndedAt: start.Add(5 * time.Second), ObservedAt: start.Add(5 * time.Second), Attributes: map[string]any{"command": "go test ./...", "exit_code": 0}},
		}},
	}
	service := testService(reader, time.Now)

	_, output, err := service.analyzeRework(context.Background(), nil, RunContextInput{Source: "codex", RunID: "run-1"})

	if err != nil {
		t.Fatal(err)
	}
	if output.SourceID != "codex" || output.RunID != "run-1" || output.Metrics.ValidationFailures != 1 || output.Metrics.FailFixRetryCycles != 1 {
		t.Fatalf("unexpected rework output: %#v", output)
	}
	if output.Metrics.ReworkDurationMs != 3000 || output.Metrics.TotalAgentEffortMs != 3000 || output.Metrics.ReworkAgentEffortRate == nil || *output.Metrics.ReworkAgentEffortRate != 1 || len(output.Cycles) != 1 || len(output.Cycles[0].Evidence) != 3 {
		t.Fatalf("unexpected rework cycle output: %#v", output)
	}
	if output.Metrics.ValidationAttemptsWithOutcome != 2 || output.Metrics.FirstPassEligibleValidations != 1 || output.Metrics.FirstPassSuccessRate == nil || *output.Metrics.FirstPassSuccessRate != 0 {
		t.Fatalf("unexpected first-pass output: %#v", output.Metrics)
	}
	if output.Capabilities.ChangeRevert.State != query.CapabilityUnavailable || output.Metadata.RuleVersion != query.AnalysisRuleVersion {
		t.Fatalf("missing capability/metadata contract: %#v", output)
	}
}

func TestGetRunTimelineHidesContentByDefault(t *testing.T) {
	reader := &readerStub{activityPage: query.ActivityPage{Activities: []query.Activity{{Name: "prompt", Content: "secret"}}}}
	service := testService(reader, time.Now)

	_, hidden, err := service.getRunTimeline(context.Background(), nil, RunTimelineInput{Source: "claude", RunID: "run-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, shown, err := service.getRunTimeline(context.Background(), nil, RunTimelineInput{Source: "claude", RunID: "run-1", IncludeContent: true})
	if err != nil {
		t.Fatal(err)
	}
	if hidden.Activities[0].Content != "" || shown.Activities[0].Content != "secret" {
		t.Fatalf("content opt-in contract failed: hidden=%#v shown=%#v", hidden.Activities[0], shown.Activities[0])
	}
}

func TestGetRunTimelineBindsContinuationToRunIdentity(t *testing.T) {
	service := testService(&readerStub{activityPage: query.ActivityPage{}}, time.Now)

	_, _, err := service.getRunTimeline(context.Background(), nil, RunTimelineInput{
		Source: "codex", RunID: "run-2", PageToken: encodeScopedPageToken("codex", "run-1", "older", 100),
	})

	if err == nil {
		t.Fatal("expected scoped cursor mismatch")
	}
}

func TestGetRunTimelineRejectsUnsupportedDirection(t *testing.T) {
	service := testService(&readerStub{}, time.Now)

	_, _, err := service.getRunTimeline(context.Background(), nil, RunTimelineInput{Source: "codex", RunID: "run-1", Direction: "newer"})

	if err == nil {
		t.Fatal("expected unsupported direction error")
	}
}

func Test_mapActivityWithContent(t *testing.T) {
	type args struct {
		activity       query.Activity
		includeContent bool
	}
	tests := []struct {
		name string
		args args
		want ActivityOutput
	}{
		{
			name: "default read omits body and all attribute values",
			args: args{activity: query.Activity{ID: "a", Source: "codex", Signal: "log", Content: "Result: body sentinel", Attributes: map[string]any{"output": "body sentinel", "secret": "attribute sentinel"}}},
			want: ActivityOutput{Source: "codex", Signal: "log", ContentState: "not_returned", ContentEvidence: query.ContentEvidence{Source: "codex", ActivityID: "a", Signal: "log", Kind: "tool_output", Evidence: "read_output", Availability: "not_returned", Fields: []string{"output"}}},
		},
		{
			name: "explicit read returns body with shared evidence",
			args: args{activity: query.Activity{ID: "a", Source: "codex", Signal: "log", Content: "Result: body sentinel", Attributes: map[string]any{"output": "body sentinel", "secret": "attribute sentinel"}}, includeContent: true},
			want: ActivityOutput{Source: "codex", Signal: "log", Content: "Result: body sentinel", ContentState: "available", ContentEvidence: query.ContentEvidence{Source: "codex", ActivityID: "a", Signal: "log", Kind: "tool_output", Evidence: "read_output", Availability: "available", Fields: []string{"output"}}},
		},
		{
			name: "explicit read does not promote a redaction marker to a body",
			args: args{activity: query.Activity{ID: "a", Source: "codex", Signal: "log", Content: "[REDACTED]", Attributes: map[string]any{"prompt": "[REDACTED]"}}, includeContent: true},
			want: ActivityOutput{Source: "codex", Signal: "log", ContentState: "unavailable", ContentEvidence: query.ContentEvidence{Source: "codex", ActivityID: "a", Signal: "log", Kind: "prompt", Evidence: "unknown", Availability: "redacted", Fields: []string{"prompt"}, RedactionReason: "producer_redacted"}},
		},
		{
			name: "explicit reference text does not imply body availability",
			args: args{activity: query.Activity{ID: "a", Source: "claude", Signal: "log", Content: "file:///private/request.json", Attributes: map[string]any{"body_ref": "file:///private/request.json"}}, includeContent: true},
			want: ActivityOutput{Source: "claude", Signal: "log", Content: "file:///private/request.json", ContentState: "available", ContentEvidence: query.ContentEvidence{Source: "claude", ActivityID: "a", Signal: "log", Kind: "reference", Evidence: "reference", Availability: "not_reported", Fields: []string{"body_ref"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapActivityWithContent(tt.args.activity, tt.args.includeContent); !cmp.Equal(tt.want, got) {
				t.Errorf("mapActivityWithContent() = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got))
			}
		})
	}
}
