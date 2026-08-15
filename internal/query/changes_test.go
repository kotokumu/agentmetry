package query

import (
	"fmt"
	"testing"
)

func TestProjectionCursorRoundTripAndRejectsMalformedTokens(t *testing.T) {
	position := ProjectionPosition{Generation: "generation-1", Sequence: 42}
	token := EncodeProjectionCursor(position)
	decoded, err := DecodeProjectionCursor(token)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != position {
		t.Fatalf("decoded cursor = %#v, want %#v", decoded, position)
	}
	for _, malformed := range []string{"", "not-base64", EncodeProjectionCursor(ProjectionPosition{})} {
		if _, err := DecodeProjectionCursor(malformed); err == nil {
			t.Fatalf("DecodeProjectionCursor(%q) succeeded", malformed)
		}
	}
}

func TestChangeTargetSetDeduplicatesAndCoarsensPerKind(t *testing.T) {
	targets := NewChangeTargetSet(3)
	targets.Add(OverviewTarget())
	targets.Add(SessionTarget("source-a", "session-1"))
	targets.Add(SessionTarget("source-a", "session-1"))
	targets.Add(SessionTarget("source-a", "session-2"))
	targets.Add(SessionTarget("source-a", "session-3"))
	targets.Add(TraceTarget("trace-1"))

	got := targets.Values()
	want := []ChangeTarget{OverviewTarget(), AllSessionsTarget(), TraceTarget("trace-1")}
	if len(got) != len(want) {
		t.Fatalf("targets = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("targets[%d] = %#v, want %#v", index, got[index], want[index])
		}
	}
}

func TestChangeTargetMatchingIsSourceQualified(t *testing.T) {
	target := SessionTarget("source-a", "same-id")
	if !target.AffectsSession("source-a", "same-id") {
		t.Fatal("exact session target did not match")
	}
	if target.AffectsSession("source-b", "same-id") {
		t.Fatal("session target matched another source")
	}
	if !AllSessionsTarget().AffectsSession("source-b", "anything") {
		t.Fatal("coarse session target did not match")
	}
}

func TestExplicitCoarseTargetDominatesExistingExactTargets(t *testing.T) {
	targets := NewChangeTargetSet(10)
	targets.Add(SessionTarget("codex", "one"))
	targets.Add(AllSessionsTarget())
	targets.Add(SessionTarget("codex", "two"))
	got := targets.Values()
	if len(got) != 1 || got[0] != AllSessionsTarget() {
		t.Fatalf("targets = %#v", got)
	}
}

func TestChangeTargetSetEnforcesOneGlobalCapacity(t *testing.T) {
	targets := NewChangeTargetSet(1_024)
	for index := 0; index < 10_000; index++ {
		targets.Add(SourceTarget(fmt.Sprintf("source-%d", index)))
		targets.Add(SessionTarget("codex", fmt.Sprintf("session-%d", index)))
		targets.Add(TraceTarget(fmt.Sprintf("trace-%d", index)))
	}
	values := targets.Values()
	if len(values) > 1_024 {
		t.Fatalf("global target capacity = %d", len(values))
	}
	for _, coarse := range []ChangeTarget{AllSourcesTarget(), AllSessionsTarget(), AllTracesTarget()} {
		found := false
		for _, value := range values {
			found = found || value == coarse
		}
		if !found {
			t.Fatalf("missing coarse target %#v in %#v", coarse, values)
		}
	}
}
