package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
)

func TestActivitySyncRemovesOldSessionAndUpsertsNewSession(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC()
	span := canonical.Span{Source: "codex", TraceID: "11111111111111111111111111111111", SpanID: "1111111111111111", Kind: canonical.ActivityResponse, StartedAt: now, EndedAt: now, Agent: canonical.AgentContext{RunID: "old"}}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	after, _ := database.CurrentProjectionPosition(context.Background())
	span.Agent.RunID = "new"
	if err := database.CommitBatch(context.Background(), canonical.Batch{Spans: []canonical.Span{span}}); err != nil {
		t.Fatal(err)
	}
	through, _ := database.CurrentProjectionPosition(context.Background())
	page, _ := query.NewPage(0, 100)
	oldIdentity, _ := query.NewConversationIdentity("codex", "old")
	oldSync, err := database.SyncSessionActivities(context.Background(), query.SessionActivitySyncFilter{Identity: oldIdentity, ActivitySyncFilter: query.ActivitySyncFilter{After: after, Through: through, Page: page}})
	if err != nil {
		t.Fatal(err)
	}
	if len(oldSync.Mutations) != 1 || oldSync.Mutations[0].Operation != query.ActivityMutationRemove {
		t.Fatalf("old mutations = %#v", oldSync.Mutations)
	}
	newIdentity, _ := query.NewConversationIdentity("codex", "new")
	newSync, err := database.SyncSessionActivities(context.Background(), query.SessionActivitySyncFilter{Identity: newIdentity, ActivitySyncFilter: query.ActivitySyncFilter{After: after, Through: through, Page: page}})
	if err != nil {
		t.Fatal(err)
	}
	if len(newSync.Mutations) != 1 || newSync.Mutations[0].Operation != query.ActivityMutationUpsert || newSync.Mutations[0].Activity == nil || newSync.Mutations[0].Activity.RunID != "new" {
		t.Fatalf("new mutations = %#v", newSync.Mutations)
	}
	if oldSync.Mutations[0].ActivityID != newSync.Mutations[0].ActivityID {
		t.Fatal("moved span identity changed")
	}
}

func TestActivitySyncKeepsEqualLogsDistinct(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	after, _ := database.CurrentProjectionPosition(context.Background())
	now := time.Now().UTC()
	log := canonical.Log{Source: "codex", TraceID: "11111111111111111111111111111111", ObservedAt: now, Kind: canonical.ActivityMessage, Body: "same", Agent: canonical.AgentContext{RunID: "session"}}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Logs: []canonical.Log{log, log}}); err != nil {
		t.Fatal(err)
	}
	through, _ := database.CurrentProjectionPosition(context.Background())
	page, _ := query.NewPage(0, 100)
	identity, _ := query.NewConversationIdentity("codex", "session")
	sync, err := database.SyncSessionActivities(context.Background(), query.SessionActivitySyncFilter{Identity: identity, ActivitySyncFilter: query.ActivitySyncFilter{After: after, Through: through, Page: page}})
	if err != nil {
		t.Fatal(err)
	}
	if len(sync.Mutations) != 2 || sync.Mutations[0].ActivityID == sync.Mutations[1].ActivityID {
		t.Fatalf("mutations = %#v", sync.Mutations)
	}
	for _, mutation := range sync.Mutations {
		if mutation.Activity == nil || mutation.Activity.ID != mutation.ActivityID {
			t.Fatalf("mutation identity mismatch = %#v", mutation)
		}
	}
}

func TestActivitySyncIncludesChildUpdatesInAggregatedRootSession(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC()
	if err := database.CommitBatch(context.Background(), canonical.Batch{
		Logs: []canonical.Log{
			{Source: "codex", ObservedAt: now, Name: "parent", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "parent"}},
			{Source: "codex", ObservedAt: now.Add(time.Second), Name: "child", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "child"}},
		},
		SessionLinks: []canonical.SessionLink{{Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now}},
	}); err != nil {
		t.Fatal(err)
	}
	after, err := database.CurrentProjectionPosition(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Logs: []canonical.Log{{
		Source: "codex", ObservedAt: now.Add(2 * time.Second), Name: "child update", Kind: canonical.ActivityResponse,
		Agent: canonical.AgentContext{RunID: "child", AgentID: "main"},
	}}}); err != nil {
		t.Fatal(err)
	}
	through, err := database.CurrentProjectionPosition(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	page, _ := query.NewPage(0, 100)
	identity, _ := query.NewConversationIdentity("codex", "parent")
	sync, err := database.SyncSessionActivities(context.Background(), query.SessionActivitySyncFilter{
		Identity:           identity,
		ActivitySyncFilter: query.ActivitySyncFilter{After: after, Through: through, Page: page},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sync.Mutations) != 1 || sync.Mutations[0].Operation != query.ActivityMutationUpsert || sync.Mutations[0].Activity == nil {
		t.Fatalf("root sync mutations = %#v", sync.Mutations)
	}
	activity := sync.Mutations[0].Activity
	if activity.RunID != "child" || activity.AgentID != "child" || activity.ParentAgentID != "parent" {
		t.Fatalf("normalized child activity = %#v", activity)
	}
}

func TestActivityIdentityIsNamespacedByProjectionGeneration(t *testing.T) {
	activityID := func(path string) string {
		database, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		now := time.Now().UTC()
		span := canonical.Span{Source: "codex", TraceID: "11111111111111111111111111111111", SpanID: "1111111111111111", Kind: canonical.ActivityResponse, StartedAt: now, EndedAt: now, Agent: canonical.AgentContext{RunID: "session"}}
		if err := database.CommitBatch(context.Background(), canonical.Batch{Spans: []canonical.Span{span}}); err != nil {
			t.Fatal(err)
		}
		page, _ := query.NewPage(0, 100)
		identity, _ := query.NewConversationIdentity("codex", "session")
		activities, err := database.ListSessionActivities(context.Background(), query.ActivityPageFilter{Identity: identity, Page: page})
		if err != nil {
			t.Fatal(err)
		}
		return activities.Activities[0].ID
	}
	directory := t.TempDir()
	first := activityID(filepath.Join(directory, "one.db"))
	second := activityID(filepath.Join(directory, "two.db"))
	if first == "" || second == "" || first == second {
		t.Fatalf("generation identities = %q, %q", first, second)
	}
}
