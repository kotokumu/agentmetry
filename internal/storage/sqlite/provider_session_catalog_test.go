package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	adapter "github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/source/builtin"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

// These anonymous OTLP-shaped records characterize supported adapter inputs.
// The communication fixture is not a claim that all Codex versions emit it.
func TestProviderTelemetrySessionCatalog(t *testing.T) {
	tests := []struct {
		name, kind, state string
		linked            bool
	}{
		{name: "sent spawn", kind: "spawn", state: "send", linked: true},
		{name: "received spawn", kind: "spawn", state: "receive"},
		{name: "followup", kind: "message", state: "send"},
		{name: "fork", kind: "fork", state: "send"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, err := store.Open(filepath.Join(t.TempDir(), "telemetry.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
			logs := plog.NewLogs()
			for _, attrs := range []map[string]any{
				{"service.name": "codex", "event.name": "codex.sse_event", "event.kind": "response.completed", "conversation.id": "root"},
				{"service.name": "codex", "event.name": "codex.agent_communication", "conversation.id": "root", "sender_thread_id": "root", "receiver_thread_id": "child", "kind": tt.kind, "state": tt.state},
				{"service.name": "codex", "event.name": "codex.agent_communication", "conversation.id": "root", "sender_thread_id": "root", "receiver_thread_id": "child", "kind": tt.kind, "state": tt.state},
				{"service.name": "codex", "event.name": "codex.sse_event", "event.kind": "response.completed", "conversation.id": "child", "slug": "not-a-title"},
				{"service.name": "claude-code", "event.name": "api_request", "session.id": "child", "agent_id": "main-agent"},
				{"service.name": "claude-code", "event.name": "api_request", "session.id": "child", "agent_id": "child-agent", "parent_agent_id": "main-agent", "query_source": "subagent"},
			} {
				resource := logs.ResourceLogs().AppendEmpty()
				resource.Resource().Attributes().PutStr("service.name", attrs["service.name"].(string))
				record := resource.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
				record.SetTimestamp(pcommon.NewTimestampFromTime(now))
				if err := record.Attributes().FromRaw(attrs); err != nil {
					t.Fatal(err)
				}
			}
			batch, err := adapter.NewNormalizer(builtin.Registry()).NormalizeLogs(logs)
			if err != nil {
				t.Fatal(err)
			}
			if err := database.CommitBatch(context.Background(), batch); err != nil {
				t.Fatal(err)
			}
			for _, view := range []query.SessionListView{query.SessionListRoots, query.SessionListAll} {
				page, err := database.ListSessions(context.Background(), query.SessionListFilter{View: view, Since: now.Add(-time.Hour)})
				if err != nil {
					t.Fatal(err)
				}
				type identity struct {
					Root, Parent string
					Role         query.SessionRole
				}
				got := map[string]identity{}
				for _, row := range page.Sessions {
					got[row.SourceID+"/"+row.ID] = identity{row.RootSessionID, row.ParentSessionID, row.Role()}
					if row.SourceID == "claude" && row.AgentCount != 2 {
						t.Fatalf("Claude agents = %d", row.AgentCount)
					}
				}
				want := map[string]identity{
					"claude/child": {"child", "", query.SessionRoot},
					"codex/root":   {"root", "", query.SessionRoot},
				}
				if !tt.linked {
					want["codex/child"] = identity{"child", "", query.SessionRoot}
				}
				if tt.linked && view == query.SessionListAll {
					want["codex/child"] = identity{"root", "root", query.SessionChild}
				}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Fatalf("%s (-want +got): %s", view, diff)
				}
			}
		})
	}
}
