package ingest

import (
	"testing"

	"github.com/kotokumu/agentmetry/internal/canonical"
)

func TestDeriveJournalMetadataUsesLosslessProjectionSourceWhenNoSemanticObservationExists(t *testing.T) {
	metadata := DeriveJournalMetadata(nil, canonical.Batch{Spans: []canonical.Span{{Source: "codex"}}}, "")
	if metadata.Source != "codex" || metadata.NormalizationStatus != "projected" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestDeriveJournalMetadataMarksMixedSources(t *testing.T) {
	metadata := DeriveJournalMetadata(nil, canonical.Batch{
		Logs: []canonical.Log{{Source: "codex"}, {Source: "claude"}},
	}, "")
	if metadata.Source != "mixed" {
		t.Fatalf("source = %q", metadata.Source)
	}
}
