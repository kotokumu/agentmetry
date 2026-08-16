package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
	store "github.com/theoden9014/agentmetry/internal/storage/sqlite"
)

func TestProjectionFeedPersistsAndWindowsCommittedTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentmetry.db")
	database, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := database.CurrentProjectionPosition(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := database.CommitBatch(context.Background(), canonical.Batch{Spans: []canonical.Span{{
		Source: "codex", TraceID: "trace-1", SpanID: "span-1", Kind: canonical.ActivityResponse,
		StartedAt: now, EndedAt: now, Agent: canonical.AgentContext{RunID: "session-1"},
	}}}); err != nil {
		t.Fatal(err)
	}
	window, err := database.ReadProjectionChanges(context.Background(), initial, 256, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if window.Through.Sequence != initial.Sequence+1 {
		t.Fatalf("through sequence = %d", window.Through.Sequence)
	}
	if !containsTarget(window.Targets, query.SessionTarget("codex", "session-1")) || !containsTarget(window.Targets, query.TraceTarget("trace-1")) {
		t.Fatalf("targets = %#v", window.Targets)
	}
	generation := window.Through.Generation
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	position, err := reopened.CurrentProjectionPosition(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if position.Generation != generation || position.Sequence != window.Through.Sequence {
		t.Fatalf("position after reopen = %#v", position)
	}
}

func TestProjectionFeedWaitUsesConstantWakeupSignal(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	position, _ := database.CurrentProjectionPosition(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- database.WaitForProjectionChange(ctx, position) }()
	time.Sleep(10 * time.Millisecond)
	if err := database.CommitBatch(context.Background(), canonical.Batch{Metrics: []canonical.MetricPoint{{ObservedAt: time.Now(), Name: "x"}}}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestProjectionFeedInvalidatesOldAndNewSessionWhenSpanMoves(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC()
	span := canonical.Span{Source: "codex", TraceID: "trace-1", SpanID: "span-1", Kind: canonical.ActivityResponse, StartedAt: now, EndedAt: now, Agent: canonical.AgentContext{RunID: "old"}}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	position, _ := database.CurrentProjectionPosition(context.Background())
	span.Agent.RunID = "new"
	if err := database.CommitBatch(context.Background(), canonical.Batch{Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	window, err := database.ReadProjectionChanges(context.Background(), position, 256, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !containsTarget(window.Targets, query.SessionTarget("codex", "old")) || !containsTarget(window.Targets, query.SessionTarget("codex", "new")) {
		t.Fatalf("move targets = %#v", window.Targets)
	}
	oldIdentity, _ := query.NewConversationIdentity("codex", "old")
	if _, err := database.GetSessionSummary(context.Background(), oldIdentity); err != query.ErrConversationNotFound {
		t.Fatalf("old session summary error = %v", err)
	}
}

func TestProjectionFeedInvalidatesAggregatedRootForChildActivity(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC()
	if err := database.CommitBatch(context.Background(), canonical.Batch{
		Logs: []canonical.Log{
			{Source: "codex", ObservedAt: now, Name: "parent", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "parent"}},
			{Source: "codex", ObservedAt: now, Name: "child", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "child"}},
		},
		SessionLinks: []canonical.SessionLink{{Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now}},
	}); err != nil {
		t.Fatal(err)
	}
	position, err := database.CurrentProjectionPosition(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Logs: []canonical.Log{{
		Source: "codex", ObservedAt: now.Add(time.Second), Name: "child update", Kind: canonical.ActivityResponse,
		Agent: canonical.AgentContext{RunID: "child"},
	}}}); err != nil {
		t.Fatal(err)
	}
	window, err := database.ReadProjectionChanges(context.Background(), position, 256, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !containsTarget(window.Targets, query.SessionTarget("codex", "parent")) {
		t.Fatalf("child update targets = %#v", window.Targets)
	}
}

func TestProjectionFeedInvalidatesAllSessionsWhenMembershipChanges(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	position, err := database.CurrentProjectionPosition(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{SessionLinks: []canonical.SessionLink{{
		Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: time.Now().UTC(),
	}}}); err != nil {
		t.Fatal(err)
	}
	window, err := database.ReadProjectionChanges(context.Background(), position, 256, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !containsTarget(window.Targets, query.AllSessionsTarget()) {
		t.Fatalf("membership targets = %#v", window.Targets)
	}
}

func containsTarget(values []query.ChangeTarget, want query.ChangeTarget) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
