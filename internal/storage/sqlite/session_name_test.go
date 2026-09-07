package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/kotokumu/agentmetry/internal/canonical"
	adapter "github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/source/builtin"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

func TestSessionNamesFromStoredTelemetry(t *testing.T) {
	for _, tt := range []struct {
		name            string
		view            query.SessionListView
		nativeEventName bool
		want            map[string]*query.SessionName
	}{
		{name: "roots never borrow child names", view: query.SessionListRoots, want: map[string]*query.SessionName{
			"claude/parent": nil, "codex/child": nil,
		}},
		{name: "all uses native conversation and historical title", view: query.SessionListAll, want: map[string]*query.SessionName{
			"claude/child": {Text: "一覧を改善する", Origin: "claude_code.generate_session_title", ObservedAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)}, "codex/child": nil,
		}},
		{name: "native OTLP event name survives normalization", view: query.SessionListAll, nativeEventName: true, want: map[string]*query.SessionName{
			"claude/child": {Text: "一覧を改善する", Origin: "claude_code.generate_session_title", ObservedAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)}, "codex/child": nil,
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "names.db")
			database, err := store.Open(path, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			initialDatabase := database
			t.Cleanup(func() { _ = initialDatabase.Close() })
			now := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
			logs := plog.NewLogs()
			// Field names and JSON title shape are from received telemetry.
			// IDs, times and text are anonymized; this is not a UI-title oracle.
			for _, attrs := range []map[string]any{
				{"service.name": "claude-code", "event.name": "assistant_response", "query_source": "generate_session_title", "session.id": "child", "event.timestamp": "2026-09-06T00:00:00Z", "request_id": "request-title", "response": `{"title":"一覧を改善する"}`},
				{"service.name": "claude-code", "event.name": "assistant_response", "query_source": "repl_main_thread", "session.id": "child", "response": `{"title":"ordinary response is not a name"}`},
				{"service.name": "codex", "event.name": "codex.sse_event", "event.kind": "response.completed", "conversation.id": "child", "query_source": "generate_session_title", "session.id": "child", "response": `{"title":"wrong provider"}`},
			} {
				record := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
				record.SetTimestamp(pcommon.NewTimestampFromTime(now))
				if tt.nativeEventName && attrs["service.name"] == "claude-code" {
					record.SetEventName("claude_code." + attrs["event.name"].(string))
					delete(attrs, "event.name")
				}
				if err := record.Attributes().FromRaw(attrs); err != nil {
					t.Fatal(err)
				}
			}
			batch, err := adapter.NewNormalizer(builtin.Registry()).NormalizeLogs(logs)
			if err != nil {
				t.Fatal(err)
			}
			batch.SessionLinks = []canonical.SessionLink{{Source: "claude", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now}}
			if err := database.CommitBatch(ctx, batch); err != nil {
				t.Fatal(err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			// Reopen already-stored telemetry: no backfill, settings or raw rewrite.
			database, err = store.Open(path, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			page, err := database.ListSessions(ctx, query.SessionListFilter{Since: now.Add(-time.Hour), View: tt.view})
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]*query.SessionName{}
			for _, entry := range page.Sessions {
				got[entry.SourceID+"/"+entry.ID] = entry.Name
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Fatalf("names (-want +got): %s", diff)
			}
		})
	}
}
