package sqlite_test

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
)

func TestListTracesPagesRollupCatalogAndRetainsParticipants(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "codex", TraceID: "trace-catalog-a", ObservedAt: now, Name: "message", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "session-a"}},
		{Source: "claude", TraceID: "trace-catalog-b", ObservedAt: now.Add(time.Second), Name: "message", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: "session-b"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	page, err := database.ListTraces(context.Background(), query.TraceListFilter{Since: now.Add(-time.Minute), Page: mustPage(t, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Traces) != 1 || !page.HasMore {
		t.Fatalf("trace page = %#v, want one row with more", page)
	}
	if len(page.Traces[0].Conversations) != 1 || page.Traces[0].Conversations[0].SourceID != "claude" {
		t.Fatalf("trace participants = %#v", page.Traces[0].Conversations)
	}
}

func TestListTracesHasMoreRequiresAnExtraCatalogRow(t *testing.T) {
	tests := []struct {
		name       string
		traceCount int
		wantIDs    []string
		wantMore   bool
	}{
		{name: "empty catalog", traceCount: 0, wantIDs: []string{}, wantMore: false},
		{name: "exact final page", traceCount: 1, wantIDs: []string{"trace-page-0"}, wantMore: false},
		{name: "extra row exists", traceCount: 2, wantIDs: []string{"trace-page-1"}, wantMore: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = database.Close() })
			now := time.Now().UTC().Truncate(time.Millisecond)
			logs := make([]canonical.Log, 0, tt.traceCount)
			for index := 0; index < tt.traceCount; index++ {
				logs = append(logs, canonical.Log{Source: "codex", TraceID: fmt.Sprintf("trace-page-%d", index), ObservedAt: now.Add(time.Duration(index) * time.Second), Name: "message", Kind: canonical.ActivityMessage, Agent: canonical.AgentContext{RunID: fmt.Sprintf("session-page-%d", index)}})
			}
			if len(logs) > 0 {
				if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
					t.Fatal(err)
				}
			}
			page, err := database.ListTraces(context.Background(), query.TraceListFilter{Page: mustPage(t, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			gotIDs := make([]string, 0, len(page.Traces))
			for _, entry := range page.Traces {
				gotIDs = append(gotIDs, entry.TraceID)
			}
			if diff := cmp.Diff(tt.wantIDs, gotIDs); diff != "" {
				t.Errorf("trace IDs mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantMore, page.HasMore); diff != "" {
				t.Errorf("HasMore mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestListTracesRejectsInvalidMinimumDuration(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	tests := []struct {
		name    string
		value   float64
		wantErr bool
	}{
		{name: "negative", value: -1, wantErr: true},
		{name: "nan", value: math.NaN(), wantErr: true},
		{name: "positive infinity", value: math.Inf(1), wantErr: true},
		{name: "negative infinity", value: math.Inf(-1), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := database.ListTraces(context.Background(), query.TraceListFilter{
				Conditions: query.TraceConditions{MinDurationMS: &tt.value}, Page: mustPage(t, 0, 1),
			})
			if diff := cmp.Diff(tt.wantErr, err != nil); diff != "" {
				t.Errorf("error presence mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestListSessionFileReadsPagesAllObservedReferencesWithoutClaimingOutput(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "claude", ObservedAt: now, Name: "gen_ai.tool", Body: `{"file_path":"src/app.go","body_ref":"request.json"}`, Kind: canonical.ActivityTool, Attributes: map[string]any{"tool_input": `{"file_path":"src/app.go","body_ref":"request.json"}`}, Agent: canonical.AgentContext{RunID: "session-files", AgentID: "agent-01", Model: "GPT-6 Astra"}},
		{Source: "claude", ObservedAt: now.Add(time.Second), Name: "gen_ai.tool", Body: `{"file_path":"src/app.go"}`, Kind: canonical.ActivityTool, Attributes: map[string]any{"tool_input": `{"file_path":"src/app.go"}`}, Agent: canonical.AgentContext{RunID: "session-files", AgentID: "agent-01", Model: "GPT-6 Astra"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	page, err := database.ListSessionFileReads(context.Background(), query.SessionFileReadFilter{Identity: mustConversationIdentity(t, "claude", "session-files"), Page: mustPage(t, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if page.Coverage != query.FileCoverageComplete || page.DistinctReferenceCount != 1 || len(page.Reads) != 1 || !page.HasMore {
		t.Fatalf("file read page = %#v, want complete one row with more", page)
	}
	if page.Reads[0].Reference != "src/app.go" || page.Reads[0].OutputMapping != query.FileMappingNotConfirmed || page.Reads[0].OutputContent != "" {
		t.Fatalf("file read output was over-claimed: %#v", page.Reads[0])
	}
	if diff := cmp.Diff([]string{"tool_input"}, page.Reads[0].ContentEvidence.Fields); diff != "" {
		t.Errorf("file reference provenance mismatch (-want +got):\n%s", diff)
	}
	second, err := database.ListSessionFileReads(context.Background(), query.SessionFileReadFilter{Identity: mustConversationIdentity(t, "claude", "session-files"), Page: mustPage(t, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Reads) != 1 || second.Reads[0].ID == page.Reads[0].ID {
		t.Fatalf("repeated path did not retain occurrence identity: %#v / %#v", page.Reads[0], second.Reads[0])
	}
}

func TestListSessionFileReadsUsesSourceQualifiedRootGroup(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "codex", ObservedAt: now, Name: "gen_ai.tool", Kind: canonical.ActivityTool, Attributes: map[string]any{"file_path": "parent.go"}, Agent: canonical.AgentContext{RunID: "parent"}},
		{Source: "codex", ObservedAt: now.Add(time.Second), Name: "gen_ai.tool", Kind: canonical.ActivityTool, Attributes: map[string]any{"file_path": "child.go"}, Agent: canonical.AgentContext{RunID: "child"}},
		{Source: "claude", ObservedAt: now.Add(2 * time.Second), Name: "gen_ai.tool", Kind: canonical.ActivityTool, Attributes: map[string]any{"file_path": "other-source.go"}, Agent: canonical.AgentContext{RunID: "child"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs, SessionLinks: []canonical.SessionLink{{Source: "codex", ParentSessionID: "parent", ChildSessionID: "child", ObservedAt: now}}}); err != nil {
		t.Fatal(err)
	}
	page, err := database.ListSessionFileReads(context.Background(), query.SessionFileReadFilter{Identity: mustConversationIdentity(t, "codex", "child"), Page: mustPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(page.Reads))
	for _, read := range page.Reads {
		got = append(got, read.Reference)
	}
	if diff := cmp.Diff([]string{"child.go", "parent.go"}, got); diff != "" {
		t.Errorf("source-qualified root group mismatch (-want +got):\n%s", diff)
	}
}

func TestListSessionFileReadsPreservesOutputEvidenceWithoutPropagatingUnrelatedRedaction(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := []canonical.Log{
		{Source: "codex", ObservedAt: now, Name: "gen_ai.tool", Body: "Result: file contents", Kind: canonical.ActivityTool, Attributes: map[string]any{"file_path": "src/app.go", "output": "file contents"}, Agent: canonical.AgentContext{RunID: "session-evidence"}},
		{Source: "codex", ObservedAt: now.Add(time.Second), Name: "gen_ai.user_prompt", Body: "[REDACTED]", Kind: canonical.ActivityPrompt, Attributes: map[string]any{"file_path": "src/private.go", "prompt": "[REDACTED]"}, Agent: canonical.AgentContext{RunID: "session-evidence"}},
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	page, err := database.ListSessionFileReads(context.Background(), query.SessionFileReadFilter{Identity: mustConversationIdentity(t, "codex", "session-evidence"), Page: mustPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]query.SessionFileRead, len(page.Reads))
	for _, read := range page.Reads {
		got[read.Reference] = read
	}
	wantAvailability := map[string]string{"src/app.go": query.FileOutputAvailable, "src/private.go": query.FileOutputNotReported}
	if diff := cmp.Diff(wantAvailability, map[string]string{"src/app.go": got["src/app.go"].OutputAvailability, "src/private.go": got["src/private.go"].OutputAvailability}); diff != "" {
		t.Errorf("output availability mismatch (-want +got):\n%s", diff)
	}
	wantFields := map[string][]string{"src/app.go": {"file_path"}, "src/private.go": {"file_path"}}
	gotFields := map[string][]string{"src/app.go": got["src/app.go"].ContentEvidence.Fields, "src/private.go": got["src/private.go"].ContentEvidence.Fields}
	if diff := cmp.Diff(wantFields, gotFields); diff != "" {
		t.Errorf("content evidence fields mismatch (-want +got):\n%s", diff)
	}
	if got["src/app.go"].OutputMapping != query.FileMappingNotConfirmed || got["src/app.go"].OutputContent != "" {
		t.Fatalf("available output was mapped to a file: %#v", got["src/app.go"])
	}
}

func TestListSessionFileReadsScansAcrossActivityBatches(t *testing.T) {
	database, err := store.Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := make([]canonical.Log, 0, 101)
	for index := 0; index < 101; index++ {
		logs = append(logs, canonical.Log{Source: "claude", ObservedAt: now.Add(time.Duration(index) * time.Second), Name: "gen_ai.tool", Body: "", Kind: canonical.ActivityTool, Attributes: map[string]any{"file_path": fmt.Sprintf("src/file-%03d.go", index)}, Agent: canonical.AgentContext{RunID: "session-batch"}})
	}
	if err := database.CommitBatch(context.Background(), canonical.Batch{Signal: canonical.SignalLog, Logs: logs}); err != nil {
		t.Fatal(err)
	}
	first, err := database.ListSessionFileReads(context.Background(), query.SessionFileReadFilter{Identity: mustConversationIdentity(t, "claude", "session-batch"), Page: mustPage(t, 0, 100)})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Reads) != 100 || first.DistinctReferenceCount != 101 || !first.HasMore || first.NextOffset != 100 {
		t.Fatalf("first batched page = %#v", first)
	}
	last, err := database.ListSessionFileReads(context.Background(), query.SessionFileReadFilter{Identity: mustConversationIdentity(t, "claude", "session-batch"), Page: mustPage(t, 100, 100)})
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Reads) != 1 || last.DistinctReferenceCount != 101 || last.HasMore || last.NextOffset != 101 {
		t.Fatalf("last batched page = %#v", last)
	}
}
