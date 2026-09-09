package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/kotokumu/agentmetry/internal/billing"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/modelcall"
	claudesource "github.com/kotokumu/agentmetry/internal/source/claude"
	codexsource "github.com/kotokumu/agentmetry/internal/source/codex"
)

type modelCallProjectionMode uint8

const (
	// Online ingestion publishes complete cost and correlation projections in
	// the export transaction; private replay persists identity facts first.
	projectModelCallDerived modelCallProjectionMode = iota
	deferModelCallDerived
)

func (store *Store) persistModelCalls(ctx context.Context, transaction *sql.Tx, exportID int64, accepted ingest.AcceptedExport, prepared preparedExport, activityIDs []string, sequence int64, previousSpans map[storedSpanKey]storedSpanScope) error {
	return store.persistModelCallsWithMode(ctx, transaction, exportID, accepted, prepared, activityIDs, sequence, previousSpans, projectModelCallDerived)
}

func (store *Store) persistReplayModelCallFacts(ctx context.Context, transaction *sql.Tx, exportID int64, accepted ingest.AcceptedExport, prepared preparedExport, activityIDs []string, sequence int64) error {
	return store.persistModelCallsWithMode(ctx, transaction, exportID, accepted, prepared, activityIDs, sequence, nil, deferModelCallDerived)
}

func (store *Store) persistModelCallsWithMode(ctx context.Context, transaction *sql.Tx, exportID int64, accepted ingest.AcceptedExport, prepared preparedExport, activityIDs []string, sequence int64, previousSpans map[storedSpanKey]storedSpanScope, projectionMode modelCallProjectionMode) error {
	if accepted.Envelope.Signal != canonical.SignalLog {
		if projectionMode == deferModelCallDerived {
			return nil
		}
		oldClaudeSessions := make(map[string]struct{})
		newClaudeSessions := make(map[string]struct{})
		oldCodexSessions := make(map[string]struct{})
		newCodexSessions := make(map[string]struct{})
		for _, previous := range previousSpans {
			if previous.usageRole != "corroborating" {
				continue
			}
			switch previous.source {
			case "claude":
				oldClaudeSessions[previous.session] = struct{}{}
			case "codex":
				oldCodexSessions[previous.session] = struct{}{}
			}
		}
		for _, span := range accepted.Projection.Spans {
			role, _ := span.Attributes["gen_ai.usage.role"].(string)
			sourceID := normalizeSource(span.Source)
			if sourceID == "claude" && role == "corroborating" {
				newClaudeSessions[span.Agent.RunID] = struct{}{}
			}
			if sourceID == "codex" && role == "corroborating" {
				newCodexSessions[span.Agent.RunID] = struct{}{}
			}
		}
		for sessionID := range oldClaudeSessions {
			if err := refreshClaudeCorroboratingProjection(ctx, transaction, sequence, sessionID); err != nil {
				return err
			}
			delete(newClaudeSessions, sessionID)
		}
		for sessionID := range oldCodexSessions {
			if err := rebuildCodexCorroboratingSupports(ctx, transaction, sequence, sessionID); err != nil {
				return err
			}
			delete(newCodexSessions, sessionID)
		}
		for sessionID := range newClaudeSessions {
			if err := refreshClaudeCorroboratingProjection(ctx, transaction, sequence, sessionID); err != nil {
				return err
			}
		}
		for sessionID := range newCodexSessions {
			if err := rebuildCodexCorroboratingSupports(ctx, transaction, sequence, sessionID); err != nil {
				return err
			}
		}
		return nil
	}
	var occurrence int64
	if err := transaction.QueryRowContext(ctx, `SELECT payload_occurrence FROM otlp_exports WHERE id = ?`, exportID).Scan(&occurrence); err != nil {
		return fmt.Errorf("load export occurrence: %w", err)
	}
	payloadHash := prepared.payload.SHA256()
	var rates []billing.Rate
	if projectionMode == projectModelCallDerived {
		loadedRates, err := loadRates(ctx, transaction)
		if err != nil {
			return err
		}
		rates = loadedRates
	}
	claudeSessions := make(map[string]struct{})
	codexSessions := make(map[string]struct{})
	for _, observed := range accepted.Observations {
		if observed.Signal != canonical.SignalLog || observed.Ordinal < 0 || observed.Ordinal >= len(accepted.Projection.Logs) || observed.Ordinal >= len(activityIDs) {
			continue
		}
		log := accepted.Projection.Logs[observed.Ordinal]
		if !isAuthoritativeCall(observed.Source, observed.SourceEventName, log) {
			continue
		}
		locator, err := modelcall.EncodeJournalLocator(observed.Source, "log", payloadHash[:], occurrence, int64(observed.Ordinal))
		if err != nil {
			return err
		}
		mode := codexsource.BillingMode(log.Attributes)
		filterAt := observed.ObservedAt
		if filterAt.IsZero() {
			filterAt = accepted.Envelope.ReceivedAt
		}
		if observed.Source == "claude" {
			if err := persistClaudeCallEvidence(ctx, transaction, observed, log, activityIDs[observed.Ordinal], locator, filterAt); err != nil {
				return err
			}
			claudeSessions[observed.SessionID] = struct{}{}
			continue
		}
		basis := modelcall.IdentityJournalEvidenceFallback
		callID := modelcall.CallID(observed.Source, observed.SessionID, basis, modelcall.JournalAlias(locator))
		result, err := transaction.ExecContext(ctx, `INSERT INTO model_calls (
  call_id, source, identity_basis, representative_activity_id, native_session_id,
  filter_at, occurred_at, model, mode, input_tokens, output_tokens,
  cache_read_tokens, cache_write_tokens, reasoning_tokens, input_reported, output_reported,
  cache_read_reported, cache_write_reported, reasoning_reported, projection_sequence
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(call_id) DO NOTHING`, callID, observed.Source, identityBasisText(basis), activityIDs[observed.Ordinal], observed.SessionID,
			formatTime(filterAt), formatOptionalTime(observed.OccurredAt), observed.Model, mode,
			observed.Usage.Input, observed.Usage.Output, observed.Usage.CacheRead, observed.Usage.CacheWrite, observed.Usage.Reasoning,
			boolInt(observed.Usage.InputReported()), boolInt(observed.Usage.OutputReported()), boolInt(observed.Usage.CacheReadReported()), boolInt(observed.Usage.CacheWriteReported()), boolInt(observed.Usage.ReasoningReported()), sequence)
		if err != nil {
			return fmt.Errorf("insert model call: %w", err)
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		role := "representative"
		if inserted == 0 {
			role = "duplicate_authoritative"
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO model_call_activity_links (activity_id, call_id, evidence_role) VALUES (?, ?, ?)`, activityIDs[observed.Ordinal], callID, role); err != nil {
			return fmt.Errorf("link model call activity: %w", err)
		}
		if observed.TraceID != "" {
			if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO model_call_trace_memberships (call_id, trace_id) VALUES (?, ?)`, callID, observed.TraceID); err != nil {
				return fmt.Errorf("link model call trace: %w", err)
			}
		}
		if inserted == 0 {
			if _, err := transaction.ExecContext(ctx, `UPDATE logs SET cost_usd = NULL WHERE activity_id = ?`, activityIDs[observed.Ordinal]); err != nil {
				return fmt.Errorf("clear duplicate activity cost: %w", err)
			}
			continue
		}
		codexSessions[observed.SessionID] = struct{}{}
		attribution := storedAttribution{basis: "unavailable", reason: "pending_replay_finalization"}
		if projectionMode == projectModelCallDerived {
			attribution = attributeCodexCall(observed.Model, mode, observed.OccurredAt, observed.Usage, rates)
		}
		if err := insertAttribution(ctx, transaction, callID, observed.Source, attribution); err != nil {
			return err
		}
		if err := publishRepresentativeCost(ctx, transaction, activityIDs[observed.Ordinal], attribution.amount, false); err != nil {
			return err
		}
		if observed.TraceID != "" {
			if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO model_call_trace_supports (call_id, trace_id, activity_id, support_kind) VALUES (?, ?, ?, 'direct')`, callID, observed.TraceID, activityIDs[observed.Ordinal]); err != nil {
				return fmt.Errorf("link Codex direct trace support: %w", err)
			}
		}
	}
	if projectionMode == deferModelCallDerived {
		return nil
	}
	for sessionID := range claudeSessions {
		if err := rebuildClaudeCostProjection(ctx, transaction, sequence, sessionID); err != nil {
			return err
		}
	}
	for sessionID := range codexSessions {
		if err := rebuildCodexCorroboratingSupports(ctx, transaction, sequence, sessionID); err != nil {
			return err
		}
	}
	return nil
}

// publishRepresentativeCost applies the compatibility rule shared by initial
// ingestion and repricing. A priced attribution overrides the legacy log cost;
// an unavailable initial attribution preserves any producer-reported value.
// Repricing may explicitly clear a previously derived value.
func publishRepresentativeCost(ctx context.Context, transaction *sql.Tx, activityID string, amount *int64, clearUnavailable bool) error {
	if amount == nil && !clearUnavailable {
		return nil
	}
	var legacy any
	if amount != nil {
		legacy = float64(*amount) / 1_000_000
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE logs SET cost_usd = ? WHERE activity_id = ?`, legacy, activityID); err != nil {
		return fmt.Errorf("publish representative activity cost: %w", err)
	}
	return nil
}

func formatOptionalTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return formatTime(value)
}

func isAuthoritativeCall(source, sourceEvent string, log canonical.Log) bool {
	role, _ := log.Attributes["gen_ai.usage.role"].(string)
	switch source {
	case "claude":
		return claudesource.IsAuthoritativeModelCall(sourceEvent, log.Name, role)
	case "codex":
		return codexsource.IsAuthoritativeModelCall(sourceEvent, log.Name, role)
	default:
		return false
	}
}

func attributeCodexCall(model, mode string, occurredAt time.Time, usage canonical.TokenUsage, rates []billing.Rate) storedAttribution {
	if model == "" {
		return storedAttribution{basis: "unavailable", reason: "missing_model"}
	}
	if occurredAt.IsZero() {
		return storedAttribution{basis: "unavailable", reason: "missing_occurred_at"}
	}
	if !usage.InputReported() || !usage.OutputReported() || !usage.CacheReadReported() || !usage.CacheWriteReported() {
		return storedAttribution{basis: "unavailable", reason: "missing_token_usage"}
	}
	if mode != "standard" {
		return storedAttribution{basis: "unavailable", reason: "unsupported_billing_mode"}
	}
	resolved := billing.ResolveRate(rates, "openai", model, mode, occurredAt, usage.Input)
	switch resolved.Status {
	case billing.RateNotFound:
		return storedAttribution{basis: "unavailable", reason: "rate_not_found"}
	case billing.RateUnsupportedUsageCondition:
		return storedAttribution{basis: "unavailable", reason: "unsupported_usage_condition"}
	case billing.RateConflict:
		return storedAttribution{basis: "unavailable", reason: "rate_conflict"}
	}
	breakdown, err := billing.Calculate(usage, resolved.Rate.Pricing)
	if err != nil {
		return storedAttribution{basis: "unavailable", reason: "calculation_error"}
	}
	amount := breakdown.TotalMicroUSD
	return storedAttribution{basis: "rate_card_estimate", amount: &amount, rateID: resolved.Rate.ID(), breakdown: breakdown}
}

type storedAttribution struct {
	basis, reason, rateID string
	amount                *int64
	breakdown             billing.CostBreakdown
}

func insertAttribution(ctx context.Context, transaction *sql.Tx, callID, source string, value storedAttribution) error {
	var rateID any
	if value.rateID != "" {
		rateID = value.rateID
	}
	_, err := transaction.ExecContext(ctx, `INSERT INTO model_call_attributions (
  call_id, source, basis, amount_micro_usd, primary_reason, rate_entry_id,
  input_micro_usd, cache_read_micro_usd, cache_write_micro_usd, output_micro_usd
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, callID, source, value.basis, value.amount, value.reason, rateID,
		value.breakdown.InputMicroUSD, value.breakdown.CacheReadMicroUSD, value.breakdown.CacheWriteMicroUSD, value.breakdown.OutputMicroUSD)
	if err != nil {
		return fmt.Errorf("insert model call attribution: %w", err)
	}
	return nil
}

func identityBasisText(value modelcall.IdentityBasis) string {
	switch value {
	case modelcall.IdentityClaudeClientRequestID:
		return "claude_client_request_id"
	case modelcall.IdentityClaudeRequestID:
		return "claude_request_id"
	case modelcall.IdentityClaudeEventSequence:
		return "claude_event_sequence"
	default:
		return "journal_evidence_fallback"
	}
}
