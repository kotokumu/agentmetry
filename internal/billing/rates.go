package billing

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"time"
)

type Rate struct {
	Provider                string
	Model                   string
	Mode                    string
	MaxInputTokensInclusive *int64
	EffectiveFrom           time.Time
	EffectiveTo             *time.Time
	Pricing                 Pricing
	SourceURL               string
	EvidenceID              string
	RetrievedAt             time.Time
	AppliedAt               time.Time
}

func (rate Rate) condition() string {
	if rate.MaxInputTokensInclusive == nil {
		return "max_input_tokens_inclusive=*"
	}
	return "max_input_tokens_inclusive=" + strconv.FormatInt(*rate.MaxInputTokensInclusive, 10)
}

func (rate Rate) CanonicalIDBytes() []byte {
	var value bytes.Buffer
	value.WriteString("agentmetry-rate-id-v1")
	value.WriteByte(0)
	for _, field := range []string{rate.Provider, rate.Model, rate.Mode, rate.condition(), canonicalTime(rate.EffectiveFrom)} {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len([]byte(field))))
		value.Write(size[:])
		value.WriteString(field)
	}
	return value.Bytes()
}

func (rate Rate) ID() string {
	hash := sha256.Sum256(rate.CanonicalIDBytes())
	return hex.EncodeToString(hash[:])
}

type RateResolutionStatus int

const (
	RateResolutionUnspecified RateResolutionStatus = iota
	RateResolved
	RateNotFound
	RateUnsupportedUsageCondition
	RateConflict
)

type RateResolution struct {
	Status RateResolutionStatus
	Rate   *Rate
}

func ResolveRate(rates []Rate, provider, model, mode string, occurredAt time.Time, inputTokens int64) RateResolution {
	baseFound := false
	matches := make([]Rate, 0, 1)
	for _, candidate := range rates {
		if candidate.Provider != provider || candidate.Model != model || candidate.Mode != mode || occurredAt.Before(candidate.EffectiveFrom) || candidate.EffectiveTo != nil && !occurredAt.Before(*candidate.EffectiveTo) {
			continue
		}
		baseFound = true
		if candidate.MaxInputTokensInclusive == nil || inputTokens <= *candidate.MaxInputTokensInclusive {
			matches = append(matches, candidate)
		}
	}
	if len(matches) > 1 {
		return RateResolution{Status: RateConflict}
	}
	if len(matches) == 1 {
		return RateResolution{Status: RateResolved, Rate: &matches[0]}
	}
	if baseFound {
		return RateResolution{Status: RateUnsupportedUsageCondition}
	}
	return RateResolution{Status: RateNotFound}
}

func BuiltinOpenAIRates() []Rate {
	effective := time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC)
	retrieved := time.Date(2026, 9, 8, 18, 39, 43, 0, time.UTC)
	maxInput := int64(272_000)
	makeRate := func(model string, pricing Pricing, evidence, sourceURL string) Rate {
		limit := maxInput
		return Rate{
			Provider: "openai", Model: model, Mode: "standard", MaxInputTokensInclusive: &limit,
			EffectiveFrom: effective, Pricing: pricing, EvidenceID: evidence, SourceURL: sourceURL,
			RetrievedAt: retrieved, AppliedAt: retrieved,
		}
	}
	sol := Pricing{InputMicroUSDPerMillion: 4_000_000, CacheReadMicroUSDPerMillion: 400_000, CacheWriteMicroUSDPerMillion: 5_000_000, OutputMicroUSDPerMillion: 20_000_000}
	return []Rate{
		makeRate("gpt-6-astra", Pricing{InputMicroUSDPerMillion: 10_000_000, CacheReadMicroUSDPerMillion: 1_000_000, CacheWriteMicroUSDPerMillion: 12_500_000, OutputMicroUSDPerMillion: 50_000_000}, "OPENAI-GPT-6-ASTRA", "https://developers.openai.com/api/docs/models/gpt-6-astra"),
		makeRate("gpt-5.6-sol", sol, "OPENAI-GPT-5.6-SOL", "https://developers.openai.com/api/docs/models/gpt-5.6-sol"),
		makeRate("gpt-5.6", sol, "OPENAI-GPT-5.6-SOL", "https://developers.openai.com/api/docs/models/gpt-5.6-sol"),
		makeRate("gpt-5.6-terra", Pricing{InputMicroUSDPerMillion: 2_000_000, CacheReadMicroUSDPerMillion: 200_000, CacheWriteMicroUSDPerMillion: 2_500_000, OutputMicroUSDPerMillion: 12_000_000}, "OPENAI-GPT-5.6-TERRA", "https://developers.openai.com/api/docs/models/gpt-5.6-terra"),
		makeRate("gpt-5.6-luna", Pricing{InputMicroUSDPerMillion: 200_000, CacheReadMicroUSDPerMillion: 20_000, CacheWriteMicroUSDPerMillion: 250_000, OutputMicroUSDPerMillion: 1_200_000}, "OPENAI-GPT-5.6-LUNA", "https://developers.openai.com/api/docs/models/gpt-5.6-luna"),
	}
}

func canonicalTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
