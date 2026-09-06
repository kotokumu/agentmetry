package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
)

func TestSessionCatalogViews(t *testing.T) {
	tests := []struct {
		name    string
		filter  query.SessionListFilter
		want    query.SessionPage
		wantErr bool
	}{
		{name: "default roots", want: query.SessionPage{AppliedView: query.SessionListRoots, Sessions: []query.SessionListEntry{
			{Session: query.Session{ID: "child", SourceID: "claude", ActivityCount: 2, AgentCount: 2}, RootSessionID: "child"},
			{Session: query.Session{ID: "root", SourceID: "codex", ActivityCount: 3, AgentCount: 3}, RootSessionID: "root"},
		}}},
		{name: "all preserves source and direct parent", filter: query.SessionListFilter{View: query.SessionListAll}, want: query.SessionPage{AppliedView: query.SessionListAll, Sessions: []query.SessionListEntry{
			{Session: query.Session{ID: "child", SourceID: "claude", ActivityCount: 2, AgentCount: 2}, RootSessionID: "child"},
			{Session: query.Session{ID: "child", SourceID: "codex", ActivityCount: 2, AgentCount: 2}, RootSessionID: "root", ParentSessionID: "root"},
			{Session: query.Session{ID: "grandchild", SourceID: "codex", ActivityCount: 1, AgentCount: 1}, RootSessionID: "root", ParentSessionID: "child"},
		}}},
		{name: "root search covers whole component", filter: query.SessionListFilter{Search: "needle"}, want: query.SessionPage{AppliedView: query.SessionListRoots, Sessions: []query.SessionListEntry{
			{Session: query.Session{ID: "root", SourceID: "codex", ActivityCount: 3, AgentCount: 3}, RootSessionID: "root"},
		}}},
		{name: "all search stays singleton", filter: query.SessionListFilter{View: query.SessionListAll, Search: "needle"}, want: query.SessionPage{AppliedView: query.SessionListAll, Sessions: []query.SessionListEntry{
			{Session: query.Session{ID: "child", SourceID: "codex", ActivityCount: 2, AgentCount: 2}, RootSessionID: "root", ParentSessionID: "root"},
		}}},
		{name: "root conditions may match different members", filter: query.SessionListFilter{Conditions: query.SessionConditions{Model: "model-a", Tool: "exec"}}, want: query.SessionPage{AppliedView: query.SessionListRoots, AppliedConditions: &query.SessionConditions{Model: "model-a", Tool: "exec"}, Sessions: []query.SessionListEntry{
			{Session: query.Session{ID: "root", SourceID: "codex", ActivityCount: 3, AgentCount: 3}, RootSessionID: "root"},
		}}},
		{name: "all conditions cannot cross members", filter: query.SessionListFilter{View: query.SessionListAll, Conditions: query.SessionConditions{Model: "model-a", Tool: "exec"}}, want: query.SessionPage{AppliedView: query.SessionListAll, AppliedConditions: &query.SessionConditions{Model: "model-a", Tool: "exec"}, Sessions: []query.SessionListEntry{}}},
		{name: "all exact child ID", filter: query.SessionListFilter{View: query.SessionListAll, SourceID: "codex", SessionID: "child"}, want: query.SessionPage{AppliedView: query.SessionListAll, Sessions: []query.SessionListEntry{
			{Session: query.Session{ID: "child", SourceID: "codex", ActivityCount: 2, AgentCount: 2}, RootSessionID: "root", ParentSessionID: "root"},
		}}},
		{name: "unobserved root is not an all row", filter: query.SessionListFilter{View: query.SessionListAll, SessionID: "root"}, want: query.SessionPage{AppliedView: query.SessionListAll, Sessions: []query.SessionListEntry{}}},
		{name: "all pagination after sorting", filter: query.SessionListFilter{View: query.SessionListAll, Page: must(query.NewPage(1, 1))}, want: query.SessionPage{AppliedView: query.SessionListAll, HasMore: true, NextOffset: 2, Sessions: []query.SessionListEntry{
			{Session: query.Session{ID: "child", SourceID: "codex", ActivityCount: 2, AgentCount: 2}, RootSessionID: "root", ParentSessionID: "root"},
		}}},
		{name: "invalid view", filter: query.SessionListFilter{View: "invalid"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, err := store.Open(filepath.Join(t.TempDir(), "catalog.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
			// Anonymous fixture: the native conversation/agent separation matches
			// telemetry observed in Agentmetry, not provider session files.
			batch := canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{
				{Source: "codex", ObservedAt: now, Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Body: "needle", Agent: canonical.AgentContext{RunID: "child", AgentID: "a", Model: "model-a"}},
				{Source: "codex", ObservedAt: now, Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child", AgentID: "b"}},
				{Source: "codex", ObservedAt: now, Name: "gen_ai.tool.result", Kind: canonical.ActivityTool, ToolName: "exec", Agent: canonical.AgentContext{RunID: "grandchild", AgentID: "g"}},
				{Source: "claude", ObservedAt: now, Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child", AgentID: "root-agent"}},
				{Source: "claude", ObservedAt: now, Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: "child", AgentID: "subagent", ParentAgentID: "root-agent"}},
			}, SessionLinks: []canonical.SessionLink{
				{Source: "codex", ParentSessionID: "root", ChildSessionID: "child", ObservedAt: now},
				{Source: "codex", ParentSessionID: "child", ChildSessionID: "grandchild", ObservedAt: now},
			}}
			if err := database.CommitBatch(context.Background(), batch); err != nil {
				t.Fatal(err)
			}
			tt.filter.Since = now.Add(-time.Hour)
			got, err := database.ListSessions(context.Background(), tt.filter)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ListSessions error=%v, wantErr=%v", err, tt.wantErr)
			}
			// Presentation-only fields have separate transport/detail tests. All
			// membership, counts, view, conditions and pagination are compared.
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(canonical.TokenUsage{}), cmpopts.IgnoreFields(query.Session{}, "Sources", "StartedAt", "EndedAt", "Agents", "Activities")); diff != "" {
				t.Fatalf("catalog (-want +got): %s", diff)
			}
		})
	}
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

func TestSessionCatalogUnsafeMembershipAndTimeScope(t *testing.T) {
	for _, view := range []query.SessionListView{query.SessionListRoots, query.SessionListAll} {
		t.Run(string(view), func(t *testing.T) {
			database, err := store.Open(filepath.Join(t.TempDir(), "edges.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
			batch := canonical.Batch{Signal: canonical.SignalLog}
			for _, id := range []string{"ambiguous", "cycle-a", "cycle-b", "active-child", "old-root"} {
				when := now
				if id == "old-root" {
					when = now.Add(-2 * time.Hour)
				}
				batch.Logs = append(batch.Logs, canonical.Log{Source: "codex", ObservedAt: when, Kind: canonical.ActivityResponse, Agent: canonical.AgentContext{RunID: id, AgentID: id}})
			}
			for _, edge := range [][2]string{{"parent-a", "ambiguous"}, {"parent-b", "ambiguous"}, {"cycle-a", "cycle-b"}, {"cycle-b", "cycle-a"}, {"old-root", "active-child"}} {
				batch.SessionLinks = append(batch.SessionLinks, canonical.SessionLink{Source: "codex", ParentSessionID: edge[0], ChildSessionID: edge[1], ObservedAt: now})
			}
			if err := database.CommitBatch(context.Background(), batch); err != nil {
				t.Fatal(err)
			}
			page, err := database.ListSessions(context.Background(), query.SessionListFilter{View: view, Since: now.Add(-time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Sessions) != 4 {
				t.Fatalf("rows = %d, want 4", len(page.Sessions))
			}
			for _, row := range page.Sessions {
				switch row.ID {
				case "ambiguous", "cycle-a", "cycle-b":
					if row.Role() != query.SessionRoot || row.RootSessionID != row.ID || row.ActivityCount != 1 {
						t.Fatalf("unsafe edge accepted: %#v", row)
					}
				case "old-root":
					if view != query.SessionListRoots || row.ActivityCount != 2 || !row.StartedAt.Equal(now.Add(-2*time.Hour)) || !row.EndedAt.Equal(now) {
						t.Fatalf("root extent = %#v", row)
					}
				case "active-child":
					if view != query.SessionListAll || row.ActivityCount != 1 || row.ParentSessionID != "old-root" || !row.StartedAt.Equal(now) {
						t.Fatalf("singleton extent = %#v", row)
					}
				default:
					t.Fatalf("unexpected row %q", row.ID)
				}
			}
		})
	}
}
