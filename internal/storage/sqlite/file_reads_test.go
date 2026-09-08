package sqlite

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
)

func TestFileReadEvidencePreservesRelatedMetadata(t *testing.T) {
	activity := query.Activity{
		ID: "activity-1", Source: "claude", Signal: canonical.SignalLog,
		ContentEvidence: &query.ContentEvidence{
			Source: "claude", ActivityID: "activity-1", Signal: string(canonical.SignalLog),
			Kind: "tool_input", Evidence: "received_input", Availability: query.FileOutputNotReturned,
			Fields: []string{"tool_input"}, Truncated: true, RedactionReason: "producer_omitted_output",
		},
	}
	want := query.ContentEvidence{
		Source: "claude", ActivityID: "activity-1", Signal: string(canonical.SignalLog),
		Kind: "tool_input", Evidence: "received_input", Availability: query.FileOutputNotReturned,
		Fields: []string{"tool_input"}, Truncated: true, RedactionReason: "producer_omitted_output",
	}
	if diff := cmp.Diff(want, fileReadEvidence(activity, []string{"tool_input"})); diff != "" {
		t.Errorf("related evidence metadata mismatch (-want +got):\n%s", diff)
	}
}

func TestFileReadOutputAvailabilityPreservesActivityOutputState(t *testing.T) {
	tests := []struct {
		name         string
		availability string
		want         string
	}{
		{name: "available output", availability: query.FileOutputAvailable, want: query.FileOutputAvailable},
		{name: "redacted output", availability: query.FileOutputRedacted, want: query.FileOutputRedacted},
		{name: "not returned output", availability: query.FileOutputNotReturned, want: query.FileOutputNotReturned},
		{name: "prompt evidence is not file output", availability: query.FileOutputRedacted, want: query.FileOutputNotReported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind := "tool_output"
			if tt.name == "prompt evidence is not file output" {
				kind = "prompt"
			}
			got := fileReadOutputAvailability(query.ContentEvidence{Kind: kind, Availability: tt.availability})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("fileReadOutputAvailability() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
