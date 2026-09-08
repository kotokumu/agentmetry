package compaction

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/kotokumu/agentmetry/internal/billing"
)

func readRateHistory(ctx context.Context, transaction *sql.Tx) ([]billing.Rate, bool, error) {
	exists, err := columnExistsTx(ctx, transaction, "model_rates", "rate_id")
	if err != nil || !exists {
		return nil, exists, err
	}
	rows, err := transaction.QueryContext(ctx, `SELECT rate_id, provider, model, mode, max_input_tokens_inclusive,
  effective_from, effective_to, input_micro_usd_per_million, cache_read_micro_usd_per_million,
  cache_write_micro_usd_per_million, output_micro_usd_per_million,
  source_url, evidence_id, retrieved_at, applied_at FROM model_rates ORDER BY provider, model, mode, effective_from`)
	if err != nil {
		return nil, true, fmt.Errorf("read source rate history: %w", err)
	}
	defer rows.Close()
	var rates []billing.Rate
	for rows.Next() {
		var storedID, effectiveFrom, retrievedAt, appliedAt string
		var effectiveTo sql.NullString
		var maxInput sql.NullInt64
		var rate billing.Rate
		if err := rows.Scan(&storedID, &rate.Provider, &rate.Model, &rate.Mode, &maxInput, &effectiveFrom, &effectiveTo,
			&rate.Pricing.InputMicroUSDPerMillion, &rate.Pricing.CacheReadMicroUSDPerMillion,
			&rate.Pricing.CacheWriteMicroUSDPerMillion, &rate.Pricing.OutputMicroUSDPerMillion,
			&rate.SourceURL, &rate.EvidenceID, &retrievedAt, &appliedAt); err != nil {
			return nil, true, fmt.Errorf("scan source rate history: %w", err)
		}
		if maxInput.Valid {
			value := maxInput.Int64
			rate.MaxInputTokensInclusive = &value
		}
		var parseErr error
		if rate.EffectiveFrom, parseErr = time.Parse(time.RFC3339Nano, effectiveFrom); parseErr != nil {
			return nil, true, parseErr
		}
		if effectiveTo.Valid {
			value, err := time.Parse(time.RFC3339Nano, effectiveTo.String)
			if err != nil {
				return nil, true, err
			}
			rate.EffectiveTo = &value
		}
		rate.RetrievedAt, parseErr = time.Parse(time.RFC3339Nano, retrievedAt)
		if parseErr != nil {
			return nil, true, parseErr
		}
		rate.AppliedAt, parseErr = time.Parse(time.RFC3339Nano, appliedAt)
		if parseErr != nil {
			return nil, true, parseErr
		}
		if rate.ID() != storedID {
			return nil, true, fmt.Errorf("source rate %s has a non-canonical identity", storedID)
		}
		rates = append(rates, rate)
	}
	return rates, true, rows.Err()
}
