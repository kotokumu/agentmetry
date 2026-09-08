package billing

import (
	"fmt"
	"sort"
	"time"
)

type RateClosure struct {
	RateID      string
	EffectiveTo time.Time
}

type RateManifestPlan struct {
	Closures []RateClosure
	Inserts  []Rate
}

func (plan RateManifestPlan) Changed() bool { return len(plan.Closures) > 0 || len(plan.Inserts) > 0 }

// PlanRateManifestUpdate validates an append-only manifest and produces a
// complete temporal catalog change without touching persistence.
func PlanRateManifestUpdate(existing, manifest []Rate, evaluatedAt time.Time) (RateManifestPlan, error) {
	if evaluatedAt.IsZero() {
		return RateManifestPlan{}, fmt.Errorf("rate manifest evaluated_at is required")
	}
	evaluatedAt = evaluatedAt.UTC()
	current := append([]Rate(nil), existing...)
	byID := make(map[string]Rate, len(current))
	storedIDs := make(map[string]struct{}, len(current))
	for _, rate := range current {
		byID[rate.ID()] = rate
		storedIDs[rate.ID()] = struct{}{}
	}
	updates := append([]Rate(nil), manifest...)
	sort.Slice(updates, func(i, j int) bool {
		if keyI, keyJ := rateSeriesKey(updates[i]), rateSeriesKey(updates[j]); keyI != keyJ {
			return keyI < keyJ
		}
		return updates[i].EffectiveFrom.Before(updates[j].EffectiveFrom)
	})
	plan := RateManifestPlan{}
	for _, update := range updates {
		if err := validateManifestRate(update, evaluatedAt); err != nil {
			return RateManifestPlan{}, err
		}
		update.AppliedAt = evaluatedAt
		if stored, exists := byID[update.ID()]; exists {
			if !sameImmutableRate(stored, update) {
				return RateManifestPlan{}, fmt.Errorf("stored model rate %s differs from immutable manifest", update.ID())
			}
			continue
		}
		latestIndex := -1
		for index := range current {
			if rateSeriesKey(current[index]) != rateSeriesKey(update) {
				continue
			}
			if latestIndex < 0 || current[index].EffectiveFrom.After(current[latestIndex].EffectiveFrom) {
				latestIndex = index
			}
		}
		if latestIndex >= 0 {
			latest := current[latestIndex]
			if !update.EffectiveFrom.After(latest.EffectiveFrom) {
				return RateManifestPlan{}, fmt.Errorf("historical model rate insertion requires a storage-generation repair")
			}
			if latest.EffectiveTo != nil {
				return RateManifestPlan{}, fmt.Errorf("latest model rate interval is already closed")
			}
			closedAt := update.EffectiveFrom.UTC()
			if _, stored := storedIDs[latest.ID()]; stored {
				plan.Closures = append(plan.Closures, RateClosure{RateID: latest.ID(), EffectiveTo: closedAt})
			} else {
				for index := range plan.Inserts {
					if plan.Inserts[index].ID() == latest.ID() {
						plan.Inserts[index].EffectiveTo = &closedAt
						break
					}
				}
			}
			current[latestIndex].EffectiveTo = &closedAt
		}
		plan.Inserts = append(plan.Inserts, update)
		current = append(current, update)
		byID[update.ID()] = update
	}
	return plan, nil
}

func validateManifestRate(rate Rate, evaluatedAt time.Time) error {
	if rate.Provider == "" || rate.Model == "" || rate.Mode == "" || rate.EffectiveFrom.IsZero() || rate.SourceURL == "" || rate.EvidenceID == "" || rate.RetrievedAt.IsZero() {
		return fmt.Errorf("rate manifest is missing required evidence or identity")
	}
	if rate.EffectiveTo != nil || rate.RetrievedAt.After(evaluatedAt) {
		return fmt.Errorf("rate manifest interval or evidence time is invalid")
	}
	if rate.MaxInputTokensInclusive != nil && *rate.MaxInputTokensInclusive < 0 {
		return fmt.Errorf("rate usage condition must be non-negative")
	}
	for _, value := range []int64{rate.Pricing.InputMicroUSDPerMillion, rate.Pricing.CacheReadMicroUSDPerMillion, rate.Pricing.CacheWriteMicroUSDPerMillion, rate.Pricing.OutputMicroUSDPerMillion} {
		if value < 0 {
			return fmt.Errorf("rate values must be non-negative")
		}
	}
	return nil
}

func rateSeriesKey(rate Rate) string {
	return rate.Provider + "\x00" + rate.Model + "\x00" + rate.Mode + "\x00" + rate.condition()
}

func sameImmutableRate(left, right Rate) bool {
	return left.ID() == right.ID() && left.Provider == right.Provider && left.Model == right.Model && left.Mode == right.Mode &&
		equalOptionalLimit(left.MaxInputTokensInclusive, right.MaxInputTokensInclusive) && left.EffectiveFrom.Equal(right.EffectiveFrom) &&
		left.Pricing == right.Pricing && left.SourceURL == right.SourceURL && left.EvidenceID == right.EvidenceID && left.RetrievedAt.Equal(right.RetrievedAt)
}

func equalOptionalLimit(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
