package canonical

import "testing"

func TestDeriveActivityKeepsModelErrorsInTheSessionTimeline(t *testing.T) {
	kind, _, _, _ := DeriveActivity("gen_ai.model.error", nil)

	if kind != ActivityResponse {
		t.Fatalf("model error kind = %q, want %q", kind, ActivityResponse)
	}
}
