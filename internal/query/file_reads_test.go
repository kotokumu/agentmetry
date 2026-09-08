package query

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/kotokumu/agentmetry/internal/canonical"
)

func TestReportedFileReferencesKeepsOccurrencesAndExcludesRequestBodyReferences(t *testing.T) {
	activity := Activity{
		Source: "claude", Signal: canonical.SignalLog,
		Attributes: map[string]any{
			"body_ref":   "request-body.json",
			"tool_input": `{"file_paths":["src/a.go","src/b.go"],"body_ref":"ignored.json"}`,
			"file_path":  "src/a.go",
		},
	}
	got := ReportedFileReferences(activity)
	want := []string{"src/a.go", "src/b.go"}
	if len(got) != len(want) {
		t.Fatalf("ReportedFileReferences() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Errorf("ReportedFileReferences()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestSessionFileReadIDIsQualifiedByObservedActivity(t *testing.T) {
	first := SessionFileReadID("codex", "session-a", "activity-1", "README.md")
	second := SessionFileReadID("codex", "session-a", "activity-2", "README.md")
	if first == second {
		t.Fatalf("same path in different activities must retain separate identities: %q", first)
	}
	if first != SessionFileReadID("codex", "session-a", "activity-1", "README.md") {
		t.Fatal("read identity must be stable for the same qualified observation")
	}
}

func TestReportedFileReferencesWithFieldsPreservesInputProvenanceAndOmitsUnavailableMarkers(t *testing.T) {
	activity := Activity{Attributes: map[string]any{
		"body_ref":   "request-body.json",
		"file_path":  "src/a.go",
		"file_paths": []any{"src/b.go", "[REDACTED]"},
		"tool_input": `{"file_path":"src/a.go","file_paths":["src/c.go","not_returned"]}`,
	}}
	want := []ReportedFileReference{
		{Value: "src/a.go", Fields: []string{"file_path", "tool_input"}},
		{Value: "src/b.go", Fields: []string{"file_paths"}},
		{Value: "src/c.go", Fields: []string{"tool_input"}},
	}
	if diff := cmp.Diff(want, ReportedFileReferencesWithFields(activity)); diff != "" {
		t.Errorf("ReportedFileReferencesWithFields() mismatch (-want +got):\n%s", diff)
	}
}
