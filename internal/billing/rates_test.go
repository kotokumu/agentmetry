package billing_test

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/billing"
)

func TestBuiltinOpenAIRatesGolden(t *testing.T) {
	rates := billing.BuiltinOpenAIRates()
	if len(rates) != 5 {
		t.Fatalf("rate count = %d, want 5", len(rates))
	}
	first := rates[0]
	wantBytes := "6167656e746d657472792d726174652d69642d763100000000066f70656e61690000000b6770742d362d6173747261000000087374616e64617264000000216d61785f696e7075745f746f6b656e735f696e636c75736976653d32373230303000000014323032362d30392d30385431353a30303a30305a"
	if got := hex.EncodeToString(first.CanonicalIDBytes()); got != wantBytes {
		t.Fatalf("canonical ID bytes = %q, want %q", got, wantBytes)
	}
	if first.ID() != "82329dc990b04cc04ecd5b02e131caffc00429dd336bda152cc575f697dc0228" {
		t.Fatalf("rate ID = %q", first.ID())
	}
	if first.Pricing != (billing.Pricing{InputMicroUSDPerMillion: 10_000_000, CacheReadMicroUSDPerMillion: 1_000_000, CacheWriteMicroUSDPerMillion: 12_500_000, OutputMicroUSDPerMillion: 50_000_000}) {
		t.Fatalf("pricing = %#v", first.Pricing)
	}
}

func TestResolveRateUsesExactModelTimeAndCondition(t *testing.T) {
	rates := billing.BuiltinOpenAIRates()
	boundary := time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		model  string
		at     time.Time
		input  int64
		status billing.RateResolutionStatus
	}{
		{name: "boundary inclusive", model: "gpt-6-astra", at: boundary, input: 272_000, status: billing.RateResolved},
		{name: "before boundary", model: "gpt-6-astra", at: boundary.Add(-time.Nanosecond), input: 100, status: billing.RateNotFound},
		{name: "exact model only", model: "gpt-6-astra-latest", at: boundary, input: 100, status: billing.RateNotFound},
		{name: "condition rejected", model: "gpt-6-astra", at: boundary, input: 272_001, status: billing.RateUnsupportedUsageCondition},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := billing.ResolveRate(rates, "openai", test.model, "standard", test.at, test.input)
			if got.Status != test.status {
				t.Fatalf("status = %v, want %v", got.Status, test.status)
			}
		})
	}
}
