package billing_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/kotokumu/agentmetry/internal/billing"
	"github.com/kotokumu/agentmetry/internal/canonical"
)

func TestCalculate(t *testing.T) {
	tests := []struct {
		name    string
		usage   canonical.TokenUsage
		pricing billing.Pricing
		want    billing.CostBreakdown
		wantErr bool
	}{
		{
			name: "cache-aware input is charged by component",
			usage: canonical.TokenUsage{
				Input:      174,
				Output:     20,
				CacheRead:  70,
				CacheWrite: 4,
			},
			pricing: billing.Pricing{
				InputMicroUSDPerMillion:      3_000_000,
				CacheReadMicroUSDPerMillion:  300_000,
				CacheWriteMicroUSDPerMillion: 3_750_000,
				OutputMicroUSDPerMillion:     15_000_000,
			},
			want: billing.CostBreakdown{
				InputTokens:         174,
				UncachedInputTokens: 100,
				CacheReadTokens:     70,
				CacheWriteTokens:    4,
				OutputTokens:        20,
				InputMicroUSD:       300,
				CacheReadMicroUSD:   21,
				CacheWriteMicroUSD:  15,
				OutputMicroUSD:      300,
				TotalMicroUSD:       636,
			},
		},
		{
			name: "reasoning is an output breakdown not an extra charge",
			usage: canonical.TokenUsage{
				Input:     100,
				Output:    20,
				CacheRead: 25,
				Reasoning: 7,
			},
			pricing: billing.Pricing{
				InputMicroUSDPerMillion:     5_000_000,
				CacheReadMicroUSDPerMillion: 400_000,
				OutputMicroUSDPerMillion:    15_000_000,
			},
			want: billing.CostBreakdown{
				InputTokens:         100,
				UncachedInputTokens: 75,
				CacheReadTokens:     25,
				OutputTokens:        20,
				InputMicroUSD:       375,
				CacheReadMicroUSD:   10,
				OutputMicroUSD:      300,
				TotalMicroUSD:       685,
			},
		},
		{
			name:  "half token rate rounds up per component",
			usage: canonical.TokenUsage{Input: 1},
			pricing: billing.Pricing{
				InputMicroUSDPerMillion: 500_000,
			},
			want: billing.CostBreakdown{
				InputTokens:         1,
				UncachedInputTokens: 1,
				InputMicroUSD:       1,
				TotalMicroUSD:       1,
			},
		},
		{
			name:    "negative token count is rejected",
			usage:   canonical.TokenUsage{Output: -1},
			pricing: billing.Pricing{},
			wantErr: true,
		},
		{
			name:  "cache breakdown cannot exceed total input",
			usage: canonical.TokenUsage{Input: 10, CacheRead: 8, CacheWrite: 3},
			pricing: billing.Pricing{
				InputMicroUSDPerMillion: 1_000_000,
			},
			wantErr: true,
		},
		{
			name:    "reasoning breakdown cannot exceed output",
			usage:   canonical.TokenUsage{Output: 5, Reasoning: 6},
			pricing: billing.Pricing{OutputMicroUSDPerMillion: 1_000_000},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := billing.Calculate(test.usage, test.pricing)
			if (err != nil) != test.wantErr {
				t.Fatalf("Calculate() error = %v, wantErr %t", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Fatalf("Calculate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
