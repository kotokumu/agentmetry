package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/kotokumu/agentmetry/internal/billing"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
)

func insertImmutableRate(ctx context.Context, transaction *sql.Tx, rate billing.Rate) error {
	var effectiveTo any
	if rate.EffectiveTo != nil {
		effectiveTo = formatTime(*rate.EffectiveTo)
	}
	result, err := transaction.ExecContext(ctx, `INSERT INTO model_rates (
  rate_id, provider, model, mode, max_input_tokens_inclusive, effective_from, effective_to,
  input_micro_usd_per_million, cache_read_micro_usd_per_million,
  cache_write_micro_usd_per_million, output_micro_usd_per_million,
  source_url, evidence_id, retrieved_at, applied_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(rate_id) DO NOTHING`,
		rate.ID(), rate.Provider, rate.Model, rate.Mode, rate.MaxInputTokensInclusive,
		formatTime(rate.EffectiveFrom), effectiveTo,
		rate.Pricing.InputMicroUSDPerMillion, rate.Pricing.CacheReadMicroUSDPerMillion,
		rate.Pricing.CacheWriteMicroUSDPerMillion, rate.Pricing.OutputMicroUSDPerMillion,
		rate.SourceURL, rate.EvidenceID, formatTime(rate.RetrievedAt), formatTime(rate.AppliedAt),
	)
	if err != nil {
		return fmt.Errorf("seed model rate %s: %w", rate.ID(), err)
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 1 {
		return err
	}
	var provider, model, mode, effectiveFrom, sourceURL, evidenceID, retrievedAt string
	var maxInput sql.NullInt64
	var input, cacheRead, cacheWrite, output int64
	err = transaction.QueryRowContext(ctx, `SELECT provider, model, mode, max_input_tokens_inclusive,
  effective_from, input_micro_usd_per_million, cache_read_micro_usd_per_million,
  cache_write_micro_usd_per_million, output_micro_usd_per_million, source_url, evidence_id, retrieved_at
FROM model_rates WHERE rate_id = ?`, rate.ID()).Scan(&provider, &model, &mode, &maxInput, &effectiveFrom, &input, &cacheRead, &cacheWrite, &output, &sourceURL, &evidenceID, &retrievedAt)
	if err != nil {
		return fmt.Errorf("verify model rate %s: %w", rate.ID(), err)
	}
	if !equalOptionalInt64(maxInput, rate.MaxInputTokensInclusive) || provider != rate.Provider || model != rate.Model || mode != rate.Mode || effectiveFrom != formatTime(rate.EffectiveFrom) || sourceURL != rate.SourceURL || evidenceID != rate.EvidenceID || retrievedAt != formatTime(rate.RetrievedAt) ||
		input != rate.Pricing.InputMicroUSDPerMillion || cacheRead != rate.Pricing.CacheReadMicroUSDPerMillion || cacheWrite != rate.Pricing.CacheWriteMicroUSDPerMillion || output != rate.Pricing.OutputMicroUSDPerMillion {
		return fmt.Errorf("stored model rate %s differs from immutable manifest", rate.ID())
	}
	return nil
}

func equalOptionalInt64(stored sql.NullInt64, expected *int64) bool {
	if expected == nil {
		return !stored.Valid
	}
	return stored.Valid && stored.Int64 == *expected
}

func loadRates(ctx context.Context, reader sqlReader) ([]billing.Rate, error) {
	rows, err := reader.QueryContext(ctx, `SELECT provider, model, mode, max_input_tokens_inclusive,
  effective_from, effective_to, input_micro_usd_per_million, cache_read_micro_usd_per_million,
  cache_write_micro_usd_per_million, output_micro_usd_per_million,
  source_url, evidence_id, retrieved_at, applied_at
FROM model_rates`)
	if err != nil {
		return nil, fmt.Errorf("load model rates: %w", err)
	}
	defer rows.Close()
	var rates []billing.Rate
	for rows.Next() {
		var rate billing.Rate
		var maxInput sql.NullInt64
		var effectiveFrom, retrievedAt, appliedAt string
		var effectiveTo sql.NullString
		if err := rows.Scan(&rate.Provider, &rate.Model, &rate.Mode, &maxInput, &effectiveFrom, &effectiveTo,
			&rate.Pricing.InputMicroUSDPerMillion, &rate.Pricing.CacheReadMicroUSDPerMillion,
			&rate.Pricing.CacheWriteMicroUSDPerMillion, &rate.Pricing.OutputMicroUSDPerMillion,
			&rate.SourceURL, &rate.EvidenceID, &retrievedAt, &appliedAt); err != nil {
			return nil, fmt.Errorf("scan model rate: %w", err)
		}
		if maxInput.Valid {
			value := maxInput.Int64
			rate.MaxInputTokensInclusive = &value
		}
		var parseErr error
		rate.EffectiveFrom, parseErr = time.Parse(time.RFC3339Nano, effectiveFrom)
		if parseErr != nil {
			return nil, fmt.Errorf("parse rate effective_from: %w", parseErr)
		}
		if effectiveTo.Valid {
			parsed, parseErr := time.Parse(time.RFC3339Nano, effectiveTo.String)
			if parseErr != nil {
				return nil, fmt.Errorf("parse rate effective_to: %w", parseErr)
			}
			rate.EffectiveTo = &parsed
		}
		rate.RetrievedAt, parseErr = time.Parse(time.RFC3339Nano, retrievedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("parse rate retrieved_at: %w", parseErr)
		}
		rate.AppliedAt, parseErr = time.Parse(time.RFC3339Nano, appliedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("parse rate applied_at: %w", parseErr)
		}
		if rate.EffectiveTo != nil && !rate.EffectiveTo.After(rate.EffectiveFrom) {
			return nil, fmt.Errorf("model rate %s has an empty interval", rate.ID())
		}
		for _, value := range []int64{rate.Pricing.InputMicroUSDPerMillion, rate.Pricing.CacheReadMicroUSDPerMillion, rate.Pricing.CacheWriteMicroUSDPerMillion, rate.Pricing.OutputMicroUSDPerMillion} {
			if value < 0 {
				return nil, fmt.Errorf("model rate %s has a negative price", rate.ID())
			}
		}
		rates = append(rates, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(rates, func(i, j int) bool {
		if rates[i].Provider != rates[j].Provider {
			return rates[i].Provider < rates[j].Provider
		}
		if rates[i].Model != rates[j].Model {
			return rates[i].Model < rates[j].Model
		}
		if rates[i].Mode != rates[j].Mode {
			return rates[i].Mode < rates[j].Mode
		}
		if condition := compareRateCondition(rates[i], rates[j]); condition != 0 {
			return condition < 0
		}
		return rates[i].EffectiveFrom.Before(rates[j].EffectiveFrom)
	})
	for index := 1; index < len(rates); index++ {
		if !sameRateSeries(rates[index-1], rates[index]) {
			continue
		}
		previous := rates[index-1]
		if previous.EffectiveTo == nil || previous.EffectiveTo.After(rates[index].EffectiveFrom) {
			return nil, fmt.Errorf("model rate history overlaps at %s", rates[index].ID())
		}
	}
	return rates, nil
}

// ApplyRateManifest appends trusted effective intervals and atomically
// reprices all retained Codex calls. Existing historical rows are immutable;
// corrections require a storage-generation replay.
func (store *Store) ApplyRateManifest(ctx context.Context, manifest []billing.Rate, evaluatedAt time.Time) error {
	store.writeMu.Lock()
	defer store.writeMu.Unlock()
	if evaluatedAt.IsZero() {
		return fmt.Errorf("rate manifest evaluated_at is required")
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin rate manifest: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	existing, err := loadRates(ctx, transaction)
	if err != nil {
		return err
	}
	plan, err := billing.PlanRateManifestUpdate(existing, manifest, evaluatedAt)
	if err != nil {
		return err
	}
	for _, closure := range plan.Closures {
		result, err := transaction.ExecContext(ctx, `UPDATE model_rates SET effective_to = ? WHERE rate_id = ? AND effective_to IS NULL`, formatTime(closure.EffectiveTo), closure.RateID)
		if err != nil {
			return fmt.Errorf("close prior model rate: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("count closed model rates: %w", err)
		}
		if affected != 1 {
			return fmt.Errorf("close prior model rate %s: expected one open interval, updated %d", closure.RateID, affected)
		}
	}
	for _, rate := range plan.Inserts {
		if err := insertImmutableRate(ctx, transaction, rate); err != nil {
			return err
		}
	}
	if !plan.Changed() {
		return transaction.Commit()
	}
	var codexCalls int64
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_calls WHERE source = 'codex'`).Scan(&codexCalls); err != nil {
		return fmt.Errorf("count Codex calls for repricing: %w", err)
	}
	if codexCalls == 0 {
		return transaction.Commit()
	}
	sequence, err := appendProjectionChange(ctx, transaction, []query.ChangeTarget{query.OverviewTarget(), query.AllSessionsTarget(), query.AllTracesTarget()})
	if err != nil {
		return err
	}
	if err := repriceCodexCalls(ctx, transaction, sequence); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit rate manifest: %w", err)
	}
	store.signalProjectionChange()
	return nil
}

// ReplaceRateHistoryForReplay installs the durable pricing authority before a
// journal replay. It is intended only for a newly-created compaction candidate.
func (store *Store) ReplaceRateHistoryForReplay(ctx context.Context, rates []billing.Rate) error {
	store.writeMu.Lock()
	defer store.writeMu.Unlock()
	ordered := append([]billing.Rate(nil), rates...)
	sort.Slice(ordered, func(i, j int) bool {
		left, right := ordered[i], ordered[j]
		if left.Provider != right.Provider {
			return left.Provider < right.Provider
		}
		if left.Model != right.Model {
			return left.Model < right.Model
		}
		if left.Mode != right.Mode {
			return left.Mode < right.Mode
		}
		if condition := compareRateCondition(left, right); condition != 0 {
			return condition < 0
		}
		return left.EffectiveFrom.Before(right.EffectiveFrom)
	})
	for index, rate := range ordered {
		if rate.Provider == "" || rate.Model == "" || rate.Mode == "" || rate.EffectiveFrom.IsZero() || rate.SourceURL == "" || rate.EvidenceID == "" || rate.RetrievedAt.IsZero() || rate.AppliedAt.IsZero() {
			return fmt.Errorf("replay rate history contains an incomplete row")
		}
		if rate.EffectiveTo != nil && !rate.EffectiveTo.After(rate.EffectiveFrom) {
			return fmt.Errorf("replay rate history contains an empty interval")
		}
		if index > 0 && sameRateSeries(ordered[index-1], rate) {
			previous := ordered[index-1]
			if previous.EffectiveTo == nil || previous.EffectiveTo.After(rate.EffectiveFrom) {
				return fmt.Errorf("replay rate history contains overlapping intervals")
			}
		}
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_rates`); err != nil {
		return fmt.Errorf("clear candidate rate history: %w", err)
	}
	for _, rate := range ordered {
		if err := insertImmutableRate(ctx, transaction, rate); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func sameRateSeries(left, right billing.Rate) bool {
	if left.Provider != right.Provider || left.Model != right.Model || left.Mode != right.Mode {
		return false
	}
	if left.MaxInputTokensInclusive == nil || right.MaxInputTokensInclusive == nil {
		return left.MaxInputTokensInclusive == nil && right.MaxInputTokensInclusive == nil
	}
	return *left.MaxInputTokensInclusive == *right.MaxInputTokensInclusive
}

func compareRateCondition(left, right billing.Rate) int {
	if left.MaxInputTokensInclusive == nil && right.MaxInputTokensInclusive == nil {
		return 0
	}
	if left.MaxInputTokensInclusive == nil {
		return -1
	}
	if right.MaxInputTokensInclusive == nil {
		return 1
	}
	if *left.MaxInputTokensInclusive < *right.MaxInputTokensInclusive {
		return -1
	}
	if *left.MaxInputTokensInclusive > *right.MaxInputTokensInclusive {
		return 1
	}
	return 0
}

func repriceCodexCalls(ctx context.Context, transaction *sql.Tx, sequence int64) error {
	rates, err := loadRates(ctx, transaction)
	if err != nil {
		return err
	}
	type retainedCall struct {
		id, activityID, sessionID, occurredAt, model, mode string
		usage                                              canonical.TokenUsage
		basis, reason                                      string
		rateID                                             sql.NullString
		amount                                             sql.NullInt64
	}
	const pageSize = 512
	afterCallID := ""
	for {
		rows, err := transaction.QueryContext(ctx, `SELECT c.call_id, c.representative_activity_id, c.native_session_id, c.occurred_at, c.model, c.mode,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_reported, output_reported, cache_read_reported, cache_write_reported, reasoning_reported,
  a.basis, a.amount_micro_usd, a.primary_reason, a.rate_entry_id
FROM model_calls c JOIN model_call_attributions a USING(call_id)
WHERE c.source = 'codex' AND c.call_id > ? ORDER BY c.call_id LIMIT ?`, afterCallID, pageSize)
		if err != nil {
			return fmt.Errorf("load Codex calls for repricing: %w", err)
		}
		calls := make([]retainedCall, 0, pageSize)
		for rows.Next() {
			var call retainedCall
			var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported bool
			if err := rows.Scan(&call.id, &call.activityID, &call.sessionID, &call.occurredAt, &call.model, &call.mode,
				&call.usage.Input, &call.usage.Output, &call.usage.CacheRead, &call.usage.CacheWrite, &call.usage.Reasoning,
				&inputReported, &outputReported, &cacheReadReported, &cacheWriteReported, &reasoningReported,
				&call.basis, &call.amount, &call.reason, &call.rateID); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan Codex call for repricing: %w", err)
			}
			call.usage.Presence = canonical.TokenPresence{Input: inputReported && call.usage.Input == 0, Output: outputReported && call.usage.Output == 0, CacheRead: cacheReadReported && call.usage.CacheRead == 0, CacheWrite: cacheWriteReported && call.usage.CacheWrite == 0, Reasoning: reasoningReported && call.usage.Reasoning == 0}
			calls = append(calls, call)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(calls) == 0 {
			return nil
		}
		for _, call := range calls {
			if err := repriceCodexCall(ctx, transaction, sequence, rates, call.id, call.activityID, call.sessionID, call.occurredAt, call.model, call.mode, call.usage, call.basis, call.amount, call.reason, call.rateID); err != nil {
				return err
			}
		}
		afterCallID = calls[len(calls)-1].id
		if len(calls) < pageSize {
			return nil
		}
	}
}

func repriceCodexCall(ctx context.Context, transaction *sql.Tx, sequence int64, rates []billing.Rate, callID, activityID, sessionID, occurredAtText, model, mode string, usage canonical.TokenUsage, basis string, amount sql.NullInt64, reason string, rateID sql.NullString) error {
	var occurredAt time.Time
	var err error
	if occurredAtText != "" {
		occurredAt, err = time.Parse(time.RFC3339Nano, occurredAtText)
		if err != nil {
			return fmt.Errorf("parse Codex call occurred_at: %w", err)
		}
	}
	attribution := attributeCodexCall(model, mode, occurredAt, usage, rates)
	if sameStoredAttribution(basis, amount, reason, rateID.String, attribution) {
		return nil
	}
	var nextRateID any
	if attribution.rateID != "" {
		nextRateID = attribution.rateID
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE model_call_attributions SET basis = ?, amount_micro_usd = ?, primary_reason = ?, rate_entry_id = ?,
  input_micro_usd = ?, cache_read_micro_usd = ?, cache_write_micro_usd = ?, output_micro_usd = ? WHERE call_id = ?`,
		attribution.basis, attribution.amount, attribution.reason, nextRateID, attribution.breakdown.InputMicroUSD,
		attribution.breakdown.CacheReadMicroUSD, attribution.breakdown.CacheWriteMicroUSD, attribution.breakdown.OutputMicroUSD, callID); err != nil {
		return fmt.Errorf("reprice Codex call: %w", err)
	}
	clearUnavailable := reason != "pending_replay_finalization"
	if err := publishRepresentativeCost(ctx, transaction, activityID, attribution.amount, clearUnavailable); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE model_calls SET projection_sequence = ? WHERE call_id = ?`, sequence, callID); err != nil {
		return err
	}
	if sessionID != "" {
		if err := appendActivityChange(ctx, transaction, sequence, 0, "session", "codex", sessionID, activityID, "upsert"); err != nil {
			return err
		}
	}
	traceRows, err := transaction.QueryContext(ctx, `SELECT trace_id FROM model_call_trace_memberships WHERE call_id = ?`, callID)
	if err != nil {
		return err
	}
	for traceRows.Next() {
		var traceID string
		if err := traceRows.Scan(&traceID); err != nil {
			_ = traceRows.Close()
			return err
		}
		if err := appendActivityChange(ctx, transaction, sequence, 0, "trace", "", traceID, activityID, "upsert"); err != nil {
			_ = traceRows.Close()
			return err
		}
	}
	if err := traceRows.Close(); err != nil {
		return err
	}
	return nil
}

func sameStoredAttribution(basis string, amount sql.NullInt64, reason, rateID string, next storedAttribution) bool {
	if basis != next.basis || reason != next.reason || rateID != next.rateID || amount.Valid != (next.amount != nil) {
		return false
	}
	return !amount.Valid || amount.Int64 == *next.amount
}
