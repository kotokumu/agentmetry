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
		codexOutput     string
		codexTime       string
		codexTruncated  bool
		lateOlder       bool
		want            map[string]*query.SessionName
	}{
		{name: "codex complete prefix survives store and truncation", view: query.SessionListAll, codexTruncated: true, codexOutput: "Wall time: 3.0673 seconds\nOutput:\n{\"schemaVersion\":4,\"pinnedThreads\":[],\"threads\":[{\"id\":\"child\",\"kind\":\"codex\",\"title\":\"Complete\"},{\"id\":\"tail\",\"title\":\"cut\n[... telemetry preview truncated ...]", want: map[string]*query.SessionName{
			"claude/child": {Text: "一覧を改善する", Origin: "claude_code.generate_session_title", ObservedAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)}, "codex/child": {Text: "Complete", Origin: "codex_app.list_threads"},
		}},
		{name: "codex later-arriving old name does not replace newer observation", view: query.SessionListAll, lateOlder: true, codexTime: "2026-09-06T00:00:00Z", codexOutput: `{"schemaVersion":4,"threads":[{"id":"child","kind":"codex","title":"Newer"}]}`, want: map[string]*query.SessionName{
			"claude/child": {Text: "一覧を改善する", Origin: "claude_code.generate_session_title", ObservedAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)}, "codex/child": {Text: "Newer", Origin: "codex_app.list_threads", ObservedAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)},
		}},
		{name: "codex targets root from historical page-excluded executor", view: query.SessionListRoots, codexTime: "2026-09-06T00:00:00Z", codexOutput: `{"schemaVersion":4,"threads":[{"id":"codex-parent","kind":"codex","title":"Root name"},{"id":"child","kind":"codex","title":"Child name"},{"id":"no-activity","kind":"codex","title":"Not a session"}]}`, want: map[string]*query.SessionName{
			"claude/parent": nil, "codex/codex-parent": {Text: "Root name", Origin: "codex_app.list_threads", ObservedAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)},
		}},
		{name: "codex all retains provider identity and native target", view: query.SessionListAll, nativeEventName: true, codexTime: "2026-09-06T00:00:00Z", codexOutput: `{"schemaVersion":4,"threads":[{"id":"child","kind":"codex","title":"Child name"},{"id":"no-activity","kind":"codex","title":"Not a session"}]}`, want: map[string]*query.SessionName{
			"claude/child": {Text: "一覧を改善する", Origin: "claude_code.generate_session_title", ObservedAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)}, "codex/child": {Text: "Child name", Origin: "codex_app.list_threads", ObservedAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)},
		}},
		{name: "codex root never borrows child name", view: query.SessionListRoots, codexOutput: `{"schemaVersion":4,"threads":[{"id":"child","kind":"codex","title":"Child name"}]}`, want: map[string]*query.SessionName{
			"claude/parent": nil, "codex/codex-parent": nil,
		}},
		{name: "codex conflicting entries fall back to id", view: query.SessionListAll, codexOutput: `{"schemaVersion":4,"threads":[{"id":"child","kind":"codex","title":"One"},{"id":"child","kind":"codex","title":"Two"}]}`, want: map[string]*query.SessionName{
			"claude/child": {Text: "一覧を改善する", Origin: "claude_code.generate_session_title", ObservedAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)}, "codex/child": nil,
		}},
		{name: "codex duplicate entries unknown time", view: query.SessionListAll, codexOutput: `{"schemaVersion":4,"threads":[{"id":"child","kind":"codex","title":"Same"},{"id":"child","kind":"codex","title":"Same"}]}`, want: map[string]*query.SessionName{
			"claude/child": {Text: "一覧を改善する", Origin: "claude_code.generate_session_title", ObservedAt: time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)}, "codex/child": {Text: "Same", Origin: "codex_app.list_threads"},
		}},
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
			if tt.codexOutput != "" {
				record := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
				record.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(-72 * time.Hour)))
				attrs := map[string]any{"service.name": "codex", "conversation.id": "executor-outside-page", "event.name": "codex.tool_result", "tool_namespace": "mcp__codex_app", "tool_name": "list_threads", "success": "true", "event.timestamp": tt.codexTime, "output": tt.codexOutput}
				attrs["output_truncated"] = tt.codexTruncated
				if tt.nativeEventName {
					record.SetEventName("codex.tool_result")
					delete(attrs, "event.name")
				}
				if err := record.Attributes().FromRaw(attrs); err != nil {
					t.Fatal(err)
				}
				if tt.lateOlder {
					older := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
					older.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(-48 * time.Hour)))
					if err := older.Attributes().FromRaw(map[string]any{"service.name": "codex", "conversation.id": "executor-outside-page", "event.name": "codex.tool_result", "tool_namespace": "mcp__codex_app", "tool_name": "list_threads", "success": "true", "event.timestamp": "2026-09-01T00:00:00Z", "output": `{"schemaVersion":4,"threads":[{"id":"child","kind":"codex","title":"Older"}]}`}); err != nil {
						t.Fatal(err)
					}
				}
			}
			batch, err := adapter.NewNormalizer(builtin.Registry()).NormalizeLogs(logs)
			if err != nil {
				t.Fatal(err)
			}
			batch.SessionLinks = []canonical.SessionLink{{Source: "claude", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now}}
			if tt.codexOutput != "" {
				batch.SessionLinks = append(batch.SessionLinks, canonical.SessionLink{Source: "codex", ParentSessionID: "codex-parent", ChildSessionID: "child", ObservedAt: now})
			}
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
			counts := map[string]int64{}
			for _, entry := range page.Sessions {
				counts[entry.SourceID] = entry.ActivityCount
			}
			if diff := cmp.Diff(map[string]int64{"claude": 2, "codex": 1}, counts); diff != "" {
				t.Fatalf("names must not change activity: %s", diff)
			}
			// A one-row page still finds targets in executors outside its page.
			paging, err := query.NewPage(1, 1)
			if err != nil {
				t.Fatal(err)
			}
			second, err := database.ListSessions(ctx, query.SessionListFilter{Since: now.Add(-time.Hour), View: tt.view, Page: paging})
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(page.Sessions[1:], second.Sessions, cmp.AllowUnexported(canonical.TokenUsage{})); diff != "" {
				t.Fatalf("page-scoped name: %s", diff)
			}
		})
	}
}
