package sqlite_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
	"github.com/theoden9014/agentmetry/internal/source/claude"
	store "github.com/theoden9014/agentmetry/internal/storage/sqlite"
	"github.com/theoden9014/agentmetry/sourceplugin"
)

func TestCommitBatchReplacesSpanRevisionAndBuildsOverview(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	now := time.Now().UTC().Truncate(time.Millisecond)
	span := canonical.Span{
		TraceID:   "trace-1",
		SpanID:    "span-1",
		Name:      "delegate to reviewer",
		Kind:      canonical.ActivityDelegation,
		StartedAt: now.Add(-time.Second),
		EndedAt:   now,
		Agent: canonical.AgentContext{
			AgentID: "reviewer",
			RunID:   "run-1",
			Model:   "example-model",
			Tokens:  canonical.TokenUsage{Input: 10, Output: 5},
		},
	}
	ctx := context.Background()
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	span.Agent.Tokens = canonical.TokenUsage{Input: 20, Output: 7}
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	logAgent := span.Agent
	logAgent.Tokens = canonical.TokenUsage{Input: 3, Output: 2}
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{{
		ObservedAt: now,
		Severity:   "INFO",
		Body:       "review started",
		Kind:       canonical.ActivityResponse,
		Agent:      logAgent,
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalMetric, Metrics: []canonical.MetricPoint{{
		ObservedAt: now,
		Name:       "agentmetry.test.metric",
		Kind:       "gauge",
		Value:      1,
		Agent:      span.Agent,
	}}}); err != nil {
		t.Fatal(err)
	}

	overview, err := database.GetOverview(ctx, query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if overview.SignalCounts != (query.SignalCounts{Traces: 1, Logs: 1, Metrics: 1}) {
		t.Fatalf("unexpected counts: %#v", overview.SignalCounts)
	}
	if overview.AgentCount != 1 || overview.RunCount != 1 {
		t.Fatalf("unexpected identity counts: %#v", overview)
	}
	if overview.Tokens.Input != 23 || overview.Tokens.Output != 9 || overview.Tokens.Total() != 32 {
		t.Fatalf("span replacement inflated tokens: %#v", overview.Tokens)
	}
	if len(overview.RecentActivity) != 2 {
		t.Fatalf("recent activity length = %d, want 2", len(overview.RecentActivity))
	}
	dashboard, err := database.GetDashboard(ctx, query.DashboardFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.RunCount != 1 || dashboard.AgentCount != 1 {
		t.Fatalf("unexpected dashboard identity counts: %#v", dashboard)
	}
	summary, err := database.GetSessionSummary(ctx, "unknown", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if summary.ActivityCount != 2 || summary.Tokens.Total() != 32 || len(summary.Agents) != 1 || summary.Agents[0].AgentID != "reviewer" {
		t.Fatalf("unexpected session summary: %#v", summary)
	}
}

func TestOverviewKeepsConversationsSeparateWhenTheyShareATrace(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{ObservedAt: now, Name: "gen_ai.session.start", TraceID: "trace-shared", Agent: canonical.AgentContext{RunID: "parent-run", Model: "gpt-parent"}},
		{ObservedAt: now.Add(time.Second), Name: "gen_ai.tool.result", TraceID: "trace-shared", Kind: canonical.ActivityDelegation, ToolName: "spawn_agent", TargetAgentType: "explorer", Agent: canonical.AgentContext{RunID: "parent-run", Model: "gpt-parent"}},
		{ObservedAt: now.Add(2 * time.Second), Name: "gen_ai.session.start", TraceID: "trace-shared", Agent: canonical.AgentContext{RunID: "child-run", Model: "gpt-child"}},
		{ObservedAt: now.Add(3 * time.Second), Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child-run", Model: "gpt-child", Tokens: canonical.TokenUsage{Input: 11, Output: 4}}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 2 {
		t.Fatalf("unexpected sessions: %#v", overview.Sessions)
	}
	byID := make(map[string]query.Session, len(overview.Sessions))
	for _, session := range overview.Sessions {
		byID[session.ID] = session
	}
	if len(byID["parent-run"].Agents) != 1 || len(byID["child-run"].Agents) != 1 {
		t.Fatalf("conversation agents were merged: %#v", overview.Sessions)
	}
	if byID["child-run"].Tokens.Total() != 15 {
		t.Fatalf("unexpected child usage: %#v", byID["child-run"].Tokens)
	}
	if len(byID["parent-run"].TraceIDs) != 1 || len(byID["child-run"].TraceIDs) != 1 {
		t.Fatalf("shared trace correlation was lost: %#v", overview.Sessions)
	}
}

func TestOverviewNamespacesConversationIdentityBySource(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "claude", ObservedAt: now, Name: "gen_ai.user.message", Kind: canonical.ActivityPrompt, Agent: canonical.AgentContext{RunID: "conversation-1"}},
		{Source: "codex", ObservedAt: now.Add(time.Second), Name: "gen_ai.user.message", Kind: canonical.ActivityPrompt, Agent: canonical.AgentContext{RunID: "conversation-1"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 2 {
		t.Fatalf("same native ID from two sources was merged: %#v", overview.Sessions)
	}
	sources := map[string]bool{}
	for _, session := range overview.Sessions {
		if session.ID != "conversation-1" {
			t.Fatalf("native conversation ID changed: %#v", session)
		}
		sources[session.SourceID] = true
	}
	if !sources["claude"] || !sources["codex"] {
		t.Fatalf("source-qualified identities missing: %#v", overview.Sessions)
	}
}

func TestGetTraceCorrelatesConversationsAndReportsIncompleteParents(t *testing.T) {
	const traceID = "11111111111111111111111111111111"
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	spans := []canonical.Span{
		{
			Source: "claude", TraceID: traceID, SpanID: "root", Name: "root operation",
			StartedAt: now, EndedAt: now.Add(4 * time.Second), Status: "Ok", Kind: canonical.ActivityReasoning,
			Agent: canonical.AgentContext{RunID: "conversation-a", AgentID: "main", Model: "model-a"},
		},
		{
			Source: "codex", TraceID: traceID, SpanID: "child", ParentSpanID: "missing", Name: "cross-source operation",
			StartedAt: now.Add(time.Second), EndedAt: now.Add(2 * time.Second), Status: "Error", Kind: canonical.ActivityTool,
			Agent: canonical.AgentContext{RunID: "conversation-b", AgentID: "reviewer", AgentDefinition: "repository-review", AgentType: "custom", Model: "model-b"},
		},
	}
	logs := []canonical.Log{{
		Source: "codex", ObservedAt: now.Add(1500 * time.Millisecond), TraceID: traceID, SpanID: "child",
		Name: "gen_ai.tool.message", Body: "correlated log", Kind: canonical.ActivityMessage,
		Agent: canonical.AgentContext{RunID: "conversation-b", AgentID: "reviewer", Tokens: canonical.TokenUsage{Input: 12}},
	}}
	ctx := context.Background()
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalTrace, Spans: spans}); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	trace, err := database.GetTrace(ctx, query.TraceFilter{TraceID: traceID})
	if err != nil {
		t.Fatal(err)
	}
	if trace.TraceID != traceID || trace.RootSpanCount != 1 || trace.MissingParentCount != 1 || trace.Status != query.TraceStatusError {
		t.Fatalf("unexpected trace summary: %#v", trace)
	}
	if trace.ActivityCount != 3 || len(trace.Conversations) != 2 || len(trace.Agents) != 2 || len(trace.Activities) != 3 {
		t.Fatalf("unexpected trace participants: %#v", trace)
	}
	if trace.Activities[0].SpanID != "root" || trace.Activities[1].Signal != canonical.SignalTrace || trace.Activities[2].Signal != canonical.SignalLog {
		t.Fatalf("trace activities not chronologically ordered: %#v", trace.Activities)
	}
	if trace.Agents[1].AgentDefinition != "repository-review" || trace.Agents[1].AgentType != "custom" || trace.Agents[1].Model != "model-b" {
		t.Fatalf("sparse event erased richer agent evidence: %#v", trace.Agents[1])
	}
	if !trace.Activities[2].ContributesToTotal {
		t.Fatalf("authoritative usage was not marked as a contribution: %#v", trace.Activities[2])
	}

	page, err := database.GetTrace(ctx, query.TraceFilter{TraceID: traceID, Offset: 1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.ActivityOffset != 1 || page.ActivityCount != 3 || len(page.Activities) != 1 || !page.HasMore || page.Activities[0].SpanID != "child" {
		t.Fatalf("unexpected trace page: %#v", page)
	}
}

func TestGetTraceReturnsNotFoundForUnknownIdentity(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.GetTrace(context.Background(), query.TraceFilter{TraceID: "22222222222222222222222222222222"})
	if !errors.Is(err, query.ErrTraceNotFound) {
		t.Fatalf("error = %v, want ErrTraceNotFound", err)
	}
}

func TestOverviewTotalsAreIndependentOfActivityDisplayLimit(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := make([]canonical.Log, 1105)
	for index := range logs {
		logs[index] = canonical.Log{
			ObservedAt: now.Add(time.Duration(index) * time.Millisecond),
			Name:       "gen_ai.response.completed",
			Body:       fmt.Sprintf("activity-%d", index),
			Kind:       canonical.ActivityResponse,
			Attributes: map[string]any{
				"gen_ai.usage.role": "authoritative_call",
				"gen_ai.usage.id":   "request-" + fmt.Sprint(index),
			},
			Agent: canonical.AgentContext{
				RunID:  "run-1",
				Tokens: canonical.TokenUsage{Input: 1, Output: 1},
			},
		}
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Tokens.Total() != 2210 || overview.Sessions[0].ActivityCount != 1105 {
		t.Fatalf("overview totals were truncated: %#v", overview)
	}
	if len(overview.Sessions[0].Activities) > 100 {
		t.Fatalf("activity display is unbounded: %d", len(overview.Sessions[0].Activities))
	}
	next, err := database.GetOverview(context.Background(), query.OverviewFilter{
		Since:          now.Add(-time.Hour),
		ActivityOffset: 100,
		ActivityLimit:  100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Sessions[0].Activities) != 100 {
		t.Fatalf("next activity page length = %d, want 100", len(next.Sessions[0].Activities))
	}
	if overview.Sessions[0].Activities[99].Content == next.Sessions[0].Activities[0].Content {
		t.Fatalf("activity pages overlap at %q", next.Sessions[0].Activities[0].Content)
	}
	if next.Tokens.Total() != overview.Tokens.Total() {
		t.Fatalf("pagination changed aggregate tokens: first=%d next=%d", overview.Tokens.Total(), next.Tokens.Total())
	}
}

func TestListSessionActivitiesFiltersByAgent(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "example", ObservedAt: now, Name: "main activity", Body: "main", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "run-1", AgentID: "main"}},
		{Source: "example", ObservedAt: now.Add(time.Second), Name: "review activity", Body: "review", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "run-1", AgentID: "reviewer"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	page, err := database.ListSessionActivities(context.Background(), query.ActivityPageFilter{SourceID: "example", ConversationID: "run-1", AgentID: "reviewer", PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Activities) != 1 || page.Activities[0].AgentID != "reviewer" {
		t.Fatalf("agent-filtered activities = %#v, want one reviewer activity", page)
	}
}

func TestOverviewCountsOneAuthoritativeUsageContribution(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	usage := canonical.TokenUsage{Input: 100, Output: 20, CacheRead: 70, Reasoning: 5}
	span := canonical.Span{
		Source: "example", TraceID: "trace-1", SpanID: "span-1", Name: "gen_ai.model.request.trace",
		StartedAt: now.Add(-time.Second), EndedAt: now, Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "corroborating", "gen_ai.usage.id": "request-1"},
		Agent:      canonical.AgentContext{RunID: "run-1", AgentID: "agent-1", Tokens: usage},
	}
	log := canonical.Log{
		Source: "example", ObservedAt: now, Name: "gen_ai.model.request", Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": "request-1"},
		Agent:      canonical.AgentContext{RunID: "run-1", AgentID: "agent-1", Tokens: usage},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{log, log}}); err != nil {
		t.Fatal(err)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Tokens != usage {
		t.Fatalf("usage was double counted: got %#v want %#v", overview.Tokens, usage)
	}
}

func TestOverviewDeduplicatesCompatibleCrossSignalUsageWithoutAStableID(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	traceUsage := canonical.TokenUsage{
		Input: 100, Output: 20, CacheRead: 70,
		Presence: canonical.TokenPresence{CacheWrite: true},
	}
	logUsage := canonical.TokenUsage{Input: 100, Output: 20, CacheRead: 70}
	span := canonical.Span{
		Source: "example", TraceID: "trace-1", SpanID: "span-1", Name: "gen_ai.model.request.trace",
		StartedAt: now.Add(-time.Second), EndedAt: now, Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "corroborating"},
		Agent:      canonical.AgentContext{RunID: "run-1", Tokens: traceUsage},
	}
	log := canonical.Log{
		Source: "example", ObservedAt: now.Add(500 * time.Millisecond), Name: "gen_ai.model.request", Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "authoritative_call"},
		Agent:      canonical.AgentContext{RunID: "run-1", Tokens: logUsage},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{log}}); err != nil {
		t.Fatal(err)
	}
	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if overview.Tokens.Input != 100 || overview.Tokens.Output != 20 {
		t.Fatalf("compatible cross-signal usage was double counted: %#v", overview.Tokens)
	}
	if len(overview.Sessions[0].Activities) != 2 || overview.Sessions[0].Activities[1].Tokens.Input != 100 {
		t.Fatalf("corroborating evidence was erased: %#v", overview.Sessions[0].Activities)
	}
}

func TestOverviewCombinesComplementaryAgentMetadataForTheSameModelCall(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"), sourceplugin.NewRegistry(claude.New()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	span := canonical.Span{
		Source: "claude", TraceID: "trace-1", SpanID: "span-1", Name: "gen_ai.model.request.trace",
		StartedAt: now.Add(-time.Second), EndedAt: now, Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "corroborating", "gen_ai.usage.id": "request-1"},
		Agent: canonical.AgentContext{
			AgentID: "repo-overview@session-1", RunID: "session-1", Model: "claude-opus-5[1m]",
		},
	}
	rootLog := canonical.Log{
		Source: "claude", ObservedAt: now.Add(-2 * time.Second), Name: "gen_ai.user.message", Kind: canonical.ActivityPrompt,
		Agent: canonical.AgentContext{RunID: "session-1", Model: "claude-opus-5"},
	}
	log := canonical.Log{
		Source: "claude", ObservedAt: now, Name: "gen_ai.model.request", Kind: canonical.ActivityResponse,
		Attributes: map[string]any{"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": "request-1"},
		Agent: canonical.AgentContext{
			AgentType: "agent:custom", RunID: "session-1", Model: "claude-opus-5",
			Tokens: canonical.TokenUsage{Input: 100, Output: 20},
		},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{rootLog, log}}); err != nil {
		t.Fatal(err)
	}

	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{Since: now.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 1 || len(overview.Sessions[0].Agents) != 2 {
		t.Fatalf("unexpected topology: %#v", overview.Sessions)
	}
	var agent, root *query.AgentSession
	for index := range overview.Sessions[0].Agents {
		switch overview.Sessions[0].Agents[index].AgentID {
		case "repo-overview@session-1":
			agent = &overview.Sessions[0].Agents[index]
		case "main":
			root = &overview.Sessions[0].Agents[index]
		}
	}
	if agent == nil || root == nil {
		t.Fatalf("root and subagent topology was not retained: %#v", overview.Sessions[0].Agents)
	}
	if agent.AgentID != "repo-overview@session-1" || agent.AgentDefinition != "repo-overview" || agent.AgentType != "custom" || agent.Model != "claude-opus-5" {
		t.Fatalf("complementary agent metadata was not combined: %#v", agent)
	}
	if agent.Tokens.Total() != 120 || root.Tokens.AnyReported() || overview.Sessions[0].Tokens.Total() != 120 {
		t.Fatalf("model-call usage was not attributed to the subagent exactly once: agent=%#v session=%#v", agent.Tokens, overview.Sessions[0].Tokens)
	}
	for _, activity := range overview.Sessions[0].Activities {
		if activity.UsageID == "request-1" && (activity.AgentID != agent.AgentID || activity.AgentDefinition != agent.AgentDefinition || activity.AgentType != agent.AgentType) {
			t.Fatalf("activity was not enriched from correlated evidence: %#v", activity)
		}
	}
}

func TestSessionSummaryInfersAgentParentFromSpanParentage(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	spans := []canonical.Span{
		{Source: "example", TraceID: "trace-topology", SpanID: "root", Name: "delegation", StartedAt: now, EndedAt: now.Add(time.Second), Kind: canonical.ActivityDelegation, Agent: canonical.AgentContext{RunID: "run-topology"}},
		{Source: "example", TraceID: "trace-topology", SpanID: "child", ParentSpanID: "root", Name: "child work", StartedAt: now.Add(time.Second), EndedAt: now.Add(2 * time.Second), Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "run-topology", AgentID: "reviewer"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: spans}); err != nil {
		t.Fatal(err)
	}
	session, err := database.GetSessionSummary(context.Background(), "example", "run-topology")
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range session.Agents {
		if agent.AgentID == "reviewer" {
			if agent.ParentAgentID != "main" {
				t.Fatalf("inferred parent = %q, want main", agent.ParentAgentID)
			}
			return
		}
	}
	t.Fatalf("reviewer agent missing from session summary: %#v", session.Agents)
}

func TestOverviewFiltersSessionsBySourceAndSearchesUnpagedActivity(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"), sourceplugin.NewRegistry(claude.New()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := make([]canonical.Log, 0, 106)
	for index := 0; index < 105; index++ {
		content := fmt.Sprintf("ordinary activity %d", index)
		if index == 0 {
			content = "Buried repository review needle"
		}
		logs = append(logs, canonical.Log{
			Source: "claude", ObservedAt: now.Add(time.Duration(index) * time.Second),
			Name: "gen_ai.agent.message", Body: content, Kind: canonical.ActivityMessage,
			Agent: canonical.AgentContext{RunID: "claude-session", Model: "claude-model"},
		})
	}
	logs = append(logs, canonical.Log{
		Source: "codex", ObservedAt: now, Name: "gen_ai.agent.message", Body: "unrelated",
		Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "codex-session", Model: "codex-model"},
	})
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}

	overview, err := database.GetOverview(context.Background(), query.OverviewFilter{
		Since: now.Add(-time.Hour), SourceID: "claude", Search: "REPOSITORY REVIEW",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Sessions) != 1 || overview.Sessions[0].ID != "claude-session" {
		t.Fatalf("unexpected filtered sessions: %#v", overview.Sessions)
	}
	if overview.Sessions[0].ActivityCount != 105 || len(overview.Sessions[0].Activities) != 100 {
		t.Fatalf("search did not inspect unpaged activity while retaining the full session: %#v", overview.Sessions[0])
	}
	if len(overview.Sessions[0].Sources) != 1 || overview.Sessions[0].Sources[0].Label != "Claude Code" {
		t.Fatalf("session source was not described by its plugin: %#v", overview.Sessions[0].Sources)
	}
	if len(overview.Sources) != 2 {
		t.Fatalf("filter options should retain all observed sources: %#v", overview.Sources)
	}
}

func TestConversationQueryIgnoresDashboardRangeAndDisplayPage(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	old := time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Millisecond)
	logs := make([]canonical.Log, 0, 205)
	for index := 0; index < 205; index++ {
		logs = append(logs, canonical.Log{
			Source: "example", ObservedAt: old.Add(time.Duration(index) * time.Second),
			Name: "gen_ai.agent.message", Body: fmt.Sprintf("activity %d", index),
			Kind:  canonical.ActivityMessage,
			Agent: canonical.AgentContext{RunID: "old-conversation", AgentID: "main"},
		})
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	const traceID = "11111111111111111111111111111111"
	const spanID = "aaaaaaaaaaaaaaaa"
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{{
		Source: "example", TraceID: traceID, SpanID: spanID, Name: "target activity",
		StartedAt: old.Add(100*time.Second + 500*time.Millisecond), EndedAt: old.Add(100*time.Second + 750*time.Millisecond),
		Kind:  canonical.ActivityUnknown,
		Agent: canonical.AgentContext{RunID: "old-conversation", AgentID: "main"},
	}}}); err != nil {
		t.Fatal(err)
	}

	conversation, err := database.GetConversation(context.Background(), query.ConversationFilter{
		SourceID: "example", ConversationID: "old-conversation", TraceID: traceID, SpanID: spanID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if conversation.ActivityCount != 206 || len(conversation.Activities) != 100 {
		t.Fatalf("exact conversation was range-limited or paged: count=%d activities=%d", conversation.ActivityCount, len(conversation.Activities))
	}
	if conversation.ActivityOffset != 79 || !conversation.HasEarlier || !conversation.HasMore {
		t.Fatalf("unexpected target window metadata: offset=%d earlier=%v more=%v", conversation.ActivityOffset, conversation.HasEarlier, conversation.HasMore)
	}
	targetFound := false
	for _, activity := range conversation.Activities {
		if activity.TraceID == traceID && activity.SpanID == spanID {
			targetFound = true
			break
		}
	}
	if !targetFound {
		t.Fatal("requested unknown-kind target was not retained in the bounded exact projection")
	}
	continuation, err := database.GetConversation(context.Background(), query.ConversationFilter{
		SourceID: "example", ConversationID: "old-conversation", TraceID: traceID, SpanID: spanID,
		ActivityOffset: conversation.ActivityOffset + len(conversation.Activities), ActivityLimit: 100,
		UseActivityOffset: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if continuation.ActivityCount != conversation.ActivityCount || continuation.ActivityOffset != 179 || len(continuation.Activities) != 27 {
		t.Fatalf("continuation changed the target projection: count=%d offset=%d activities=%d", continuation.ActivityCount, continuation.ActivityOffset, len(continuation.Activities))
	}
	if continuation.Activities[0].Content != "activity 26" || continuation.Activities[len(continuation.Activities)-1].Content != "activity 0" {
		t.Fatalf("continuation skipped or duplicated the target boundary: first=%q last=%q", continuation.Activities[0].Content, continuation.Activities[len(continuation.Activities)-1].Content)
	}
	_, err = database.GetConversation(context.Background(), query.ConversationFilter{
		SourceID: "example", ConversationID: "old-conversation", TraceID: traceID, SpanID: "bbbbbbbbbbbbbbbb",
	})
	if !errors.Is(err, query.ErrConversationTargetNotFound) {
		t.Fatalf("wrong target error = %v, want ErrConversationTargetNotFound", err)
	}
}

func TestOpenEnablesWAL(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	mode, err := database.JournalMode(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal mode = %q, want wal", mode)
	}
}
