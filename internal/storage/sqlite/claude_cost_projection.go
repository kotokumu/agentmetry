package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/modelcall"
	"github.com/kotokumu/agentmetry/internal/observation"
	claudesource "github.com/kotokumu/agentmetry/internal/source/claude"
)

func persistClaudeCallEvidence(ctx context.Context, transaction *sql.Tx, observed observation.Observation, log canonical.Log, activityID string, locator []byte, filterAt time.Time) error {
	providerCost := claudesource.ResolveProviderCost(log.Attributes)
	_, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO model_call_evidence (
  activity_id, source, native_session_id, trace_id, locator, filter_at, model, occurred_at, mode,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_reported, output_reported, cache_read_reported, cache_write_reported, reasoning_reported,
  provider_amount_state, provider_amount_micro_usd
) VALUES (?, 'claude', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		activityID, observed.SessionID, observed.TraceID, locator, formatTime(filterAt), observed.Model,
		formatOptionalTime(observed.OccurredAt), "",
		observed.Usage.Input, observed.Usage.Output, observed.Usage.CacheRead, observed.Usage.CacheWrite, observed.Usage.Reasoning,
		boolInt(observed.Usage.InputReported()), boolInt(observed.Usage.OutputReported()), boolInt(observed.Usage.CacheReadReported()),
		boolInt(observed.Usage.CacheWriteReported()), boolInt(observed.Usage.ReasoningReported()), string(providerCost.State), providerCost.AmountMicroUSD)
	if err != nil {
		return fmt.Errorf("retain Claude model call evidence: %w", err)
	}
	for _, alias := range claudeAliases(log.Attributes) {
		if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO model_call_evidence_aliases (activity_id, alias_basis, alias_value) VALUES (?, ?, ?)`, activityID, identityBasisText(alias.Basis), aliasStorageValue(alias)); err != nil {
			return fmt.Errorf("retain Claude model call alias: %w", err)
		}
	}
	return nil
}

func claudeAliases(attributes map[string]any) []modelcall.IdentityAlias {
	raw := claudesource.CallIdentityAliases(attributes)
	result := make([]modelcall.IdentityAlias, 0, len(raw))
	for _, alias := range raw {
		value := modelcall.IdentityAlias{Value: alias.Value, Sequence: alias.Sequence}
		switch alias.Kind {
		case claudesource.CallAliasClientRequestID:
			value.Basis = modelcall.IdentityClaudeClientRequestID
		case claudesource.CallAliasRequestID:
			value.Basis = modelcall.IdentityClaudeRequestID
		case claudesource.CallAliasEventSequence:
			value.Basis = modelcall.IdentityClaudeEventSequence
		default:
			continue
		}
		result = append(result, value)
	}
	return result
}

func aliasStorageValue(alias modelcall.IdentityAlias) string {
	if alias.Basis == modelcall.IdentityClaudeEventSequence {
		return strconv.FormatUint(alias.Sequence, 10)
	}
	return alias.Value
}

func rebuildClaudeCostProjection(ctx context.Context, transaction *sql.Tx, sequence int64, sessionID string) error {
	evidence, err := loadClaudeCallEvidence(ctx, transaction, sessionID)
	if err != nil {
		return err
	}
	resolved := modelcall.ResolveClaudeCalls(evidence)
	if _, err := transaction.ExecContext(ctx, `UPDATE logs SET cost_usd = NULL WHERE activity_id IN (
  SELECT activity_id FROM model_call_evidence WHERE source = 'claude' AND native_session_id = ?
)`, sessionID); err != nil {
		return fmt.Errorf("clear prior Claude activity cost: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_calls WHERE source = 'claude' AND native_session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("replace Claude model calls: %w", err)
	}
	for _, call := range resolved {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO model_calls (
  call_id, source, identity_basis, representative_activity_id, native_session_id,
  filter_at, occurred_at, model, mode, input_tokens, output_tokens,
  cache_read_tokens, cache_write_tokens, reasoning_tokens, input_reported, output_reported,
  cache_read_reported, cache_write_reported, reasoning_reported, projection_sequence
) VALUES (?, 'claude', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			call.ID, identityBasisText(call.IdentityBasis), call.RepresentativeActivityID, call.SessionID,
			formatTime(call.FilterAt), formatOptionalTime(call.OccurredAt), call.Model, call.Mode,
			call.Usage.Input, call.Usage.Output, call.Usage.CacheRead, call.Usage.CacheWrite, call.Usage.Reasoning,
			boolInt(call.Usage.InputReported()), boolInt(call.Usage.OutputReported()), boolInt(call.Usage.CacheReadReported()),
			boolInt(call.Usage.CacheWriteReported()), boolInt(call.Usage.ReasoningReported()), sequence); err != nil {
			return fmt.Errorf("project resolved Claude call: %w", err)
		}
		stored := storedAttribution{basis: costBasisText(call.Attribution.Basis), amount: call.Attribution.AmountMicroUSD, reason: reasonText(call.Attribution.Reason)}
		if err := insertAttribution(ctx, transaction, call.ID, "claude", stored); err != nil {
			return err
		}
		for _, item := range call.Evidence {
			role := "duplicate_authoritative"
			if item.ActivityID == call.RepresentativeActivityID {
				role = "representative"
				if call.Attribution.AmountMicroUSD != nil {
					if _, err := transaction.ExecContext(ctx, `UPDATE logs SET cost_usd = ? WHERE activity_id = ?`, float64(*call.Attribution.AmountMicroUSD)/1_000_000, item.ActivityID); err != nil {
						return fmt.Errorf("publish Claude representative cost: %w", err)
					}
				}
			}
			if _, err := transaction.ExecContext(ctx, `INSERT INTO model_call_activity_links (activity_id, call_id, evidence_role) VALUES (?, ?, ?)`, item.ActivityID, call.ID, role); err != nil {
				return fmt.Errorf("project Claude evidence link: %w", err)
			}
			if item.TraceID != "" {
				if _, err := transaction.ExecContext(ctx, `INSERT INTO model_call_trace_supports (call_id, trace_id, activity_id, support_kind) VALUES (?, ?, ?, 'direct')`, call.ID, item.TraceID, item.ActivityID); err != nil {
					return fmt.Errorf("project Claude direct trace support: %w", err)
				}
			}
		}
	}
	if err := rebuildClaudeCorroboratingSupports(ctx, transaction, sessionID); err != nil {
		return err
	}
	return publishClaudeCallChanges(ctx, transaction, sequence, sessionID)
}

func refreshClaudeCorroboratingProjection(ctx context.Context, transaction *sql.Tx, sequence int64, sessionID string) error {
	if err := rebuildClaudeCorroboratingSupports(ctx, transaction, sessionID); err != nil {
		return err
	}
	return publishClaudeCallChanges(ctx, transaction, sequence, sessionID)
}

func deriveClaudeTraceMemberships(ctx context.Context, transaction *sql.Tx, sessionID string) error {
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_call_trace_memberships
WHERE call_id IN (SELECT call_id FROM model_calls WHERE source = 'claude' AND native_session_id = ?)`, sessionID); err != nil {
		return fmt.Errorf("replace Claude trace memberships: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO model_call_trace_memberships (call_id, trace_id)
SELECT DISTINCT call_id, trace_id FROM model_call_trace_supports WHERE call_id IN (
  SELECT call_id FROM model_calls WHERE source = 'claude' AND native_session_id = ?
)`, sessionID); err != nil {
		return fmt.Errorf("derive Claude trace memberships: %w", err)
	}
	return nil
}

func loadClaudeCallEvidence(ctx context.Context, reader sqlReader, sessionID string) ([]modelcall.ClaudeEvidence, error) {
	rows, err := reader.QueryContext(ctx, `SELECT activity_id, native_session_id, trace_id, locator, filter_at, model, occurred_at, mode,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_reported, output_reported, cache_read_reported, cache_write_reported, reasoning_reported,
  provider_amount_state, provider_amount_micro_usd
FROM model_call_evidence WHERE source = 'claude' AND native_session_id = ? ORDER BY activity_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load Claude model call evidence: %w", err)
	}
	defer rows.Close()
	var result []modelcall.ClaudeEvidence
	for rows.Next() {
		var item modelcall.ClaudeEvidence
		var filterAt, occurredAt string
		var inputReported, outputReported, cacheReadReported, cacheWriteReported, reasoningReported bool
		var amount sql.NullInt64
		if err := rows.Scan(&item.ActivityID, &item.SessionID, &item.TraceID, &item.Locator, &filterAt, &item.Model, &occurredAt, &item.Mode,
			&item.Usage.Input, &item.Usage.Output, &item.Usage.CacheRead, &item.Usage.CacheWrite, &item.Usage.Reasoning,
			&inputReported, &outputReported, &cacheReadReported, &cacheWriteReported, &reasoningReported,
			&item.ProviderAmountState, &amount); err != nil {
			return nil, fmt.Errorf("scan Claude model call evidence: %w", err)
		}
		item.FilterAt, err = time.Parse(time.RFC3339Nano, filterAt)
		if err != nil {
			return nil, fmt.Errorf("parse Claude filter time: %w", err)
		}
		if occurredAt != "" {
			item.OccurredAt, err = time.Parse(time.RFC3339Nano, occurredAt)
			if err != nil {
				return nil, fmt.Errorf("parse Claude occurrence time: %w", err)
			}
		}
		item.Usage.Presence = canonical.TokenPresence{Input: inputReported && item.Usage.Input == 0, Output: outputReported && item.Usage.Output == 0, CacheRead: cacheReadReported && item.Usage.CacheRead == 0, CacheWrite: cacheWriteReported && item.Usage.CacheWrite == 0, Reasoning: reasoningReported && item.Usage.Reasoning == 0}
		if amount.Valid {
			value := amount.Int64
			item.ProviderAmount = &value
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Claude model call evidence: %w", err)
	}
	aliases, err := loadClaudeEvidenceAliases(ctx, reader, sessionID)
	if err != nil {
		return nil, err
	}
	for index := range result {
		result[index].Aliases = aliases[result[index].ActivityID]
	}
	return result, nil
}

func loadClaudeEvidenceAliases(ctx context.Context, reader sqlReader, sessionID string) (map[string][]modelcall.IdentityAlias, error) {
	rows, err := reader.QueryContext(ctx, `SELECT aliases.activity_id, aliases.alias_basis, aliases.alias_value
FROM model_call_evidence_aliases aliases
JOIN model_call_evidence evidence ON evidence.activity_id = aliases.activity_id
WHERE evidence.source = 'claude' AND evidence.native_session_id = ?
ORDER BY aliases.activity_id, aliases.alias_basis, aliases.alias_value`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load Claude aliases: %w", err)
	}
	defer rows.Close()
	result := make(map[string][]modelcall.IdentityAlias)
	for rows.Next() {
		var activityID, basis, value string
		if err := rows.Scan(&activityID, &basis, &value); err != nil {
			return nil, err
		}
		alias := modelcall.IdentityAlias{Basis: parseIdentityBasis(basis), Value: value}
		if alias.Basis == modelcall.IdentityClaudeEventSequence {
			alias.Sequence, err = strconv.ParseUint(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse Claude sequence alias: %w", err)
			}
		}
		result[activityID] = append(result[activityID], alias)
	}
	return result, rows.Err()
}

func rebuildClaudeCorroboratingSupports(ctx context.Context, transaction *sql.Tx, sessionID string) error {
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_call_activity_links
WHERE evidence_role = 'corroborating' AND call_id IN (
  SELECT call_id FROM model_calls WHERE source = 'claude' AND native_session_id = ?
)`, sessionID); err != nil {
		return fmt.Errorf("clear Claude corroborating activity links: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_call_trace_supports
WHERE support_kind = 'corroborating' AND call_id IN (
  SELECT call_id FROM model_calls WHERE source = 'claude' AND native_session_id = ?
)`, sessionID); err != nil {
		return fmt.Errorf("clear Claude corroborating trace supports: %w", err)
	}
	const candidates = `WITH span_aliases AS (
  SELECT activity_id, trace_id, run_id, 'claude_client_request_id' AS alias_basis,
    json_extract(attributes_json, '$."gen_ai.client.request.id"') AS alias_value
  FROM spans
  WHERE source = 'claude'
    AND run_id = ?
    AND COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), '') = 'corroborating'
    AND json_type(attributes_json, '$."gen_ai.client.request.id"') = 'text'
    AND json_extract(attributes_json, '$."gen_ai.client.request.id"') <> ''
  UNION ALL
  SELECT activity_id, trace_id, run_id, 'claude_request_id',
    json_extract(attributes_json, '$."gen_ai.request.id"')
  FROM spans
  WHERE source = 'claude'
    AND run_id = ?
    AND COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), '') = 'corroborating'
    AND json_type(attributes_json, '$."gen_ai.request.id"') = 'text'
    AND json_extract(attributes_json, '$."gen_ai.request.id"') <> ''
  UNION ALL
  SELECT activity_id, trace_id, run_id, 'claude_event_sequence',
    CAST(json_extract(attributes_json, '$."event.sequence"') AS TEXT)
  FROM spans
  WHERE source = 'claude'
    AND run_id = ?
    AND COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), '') = 'corroborating'
    AND json_type(attributes_json, '$."event.sequence"') = 'integer'
    AND json_extract(attributes_json, '$."event.sequence"') >= 0
), typed_identities AS (
  SELECT aliases.alias_basis, aliases.alias_value, MIN(links.call_id) AS call_id
  FROM model_call_evidence evidence
  JOIN model_call_evidence_aliases aliases USING (activity_id)
  JOIN model_call_activity_links links USING (activity_id)
  WHERE evidence.source = 'claude' AND evidence.native_session_id = ?
  GROUP BY aliases.alias_basis, aliases.alias_value HAVING COUNT(DISTINCT links.call_id) = 1
), typed_base AS (
  SELECT span_aliases.activity_id, span_aliases.trace_id, typed_identities.call_id
  FROM span_aliases JOIN typed_identities USING (alias_basis, alias_value)
), usage_identities AS (
  SELECT logs.usage_id,
    json_extract(logs.attributes_json, '$."gen_ai.usage.id.basis"') AS usage_basis,
    MIN(links.call_id) AS call_id
  FROM logs JOIN model_call_activity_links links USING (activity_id)
  WHERE logs.source = 'claude' AND logs.run_id = ? AND logs.usage_id <> ''
    AND COALESCE(json_extract(logs.attributes_json, '$."gen_ai.usage.id.basis"'), '') <> ''
  GROUP BY logs.usage_id, usage_basis HAVING COUNT(DISTINCT links.call_id) = 1
), usage_base AS (
  SELECT spans.activity_id, spans.trace_id, usage_identities.call_id
  FROM spans JOIN usage_identities ON usage_identities.usage_id = spans.usage_id
    AND usage_identities.usage_basis = json_extract(spans.attributes_json, '$."gen_ai.usage.id.basis"')
  WHERE spans.source = 'claude' AND spans.run_id = ?
    AND COALESCE(json_extract(spans.attributes_json, '$."gen_ai.usage.role"'), '') = 'corroborating'
), base AS (
  SELECT * FROM typed_base
  UNION
  SELECT * FROM usage_base
), unique_candidates AS (
  SELECT activity_id, MIN(trace_id) AS trace_id, MIN(call_id) AS call_id
  FROM base GROUP BY activity_id HAVING COUNT(DISTINCT call_id) = 1
)
`
	if _, err := transaction.ExecContext(ctx, candidates+`INSERT INTO model_call_activity_links (activity_id, call_id, evidence_role)
SELECT activity_id, call_id, 'corroborating' FROM unique_candidates`, sessionID, sessionID, sessionID, sessionID, sessionID, sessionID); err != nil {
		return fmt.Errorf("project Claude corroborating links: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO model_call_trace_supports (call_id, trace_id, activity_id, support_kind)
SELECT links.call_id, spans.trace_id, spans.activity_id, 'corroborating'
FROM spans JOIN model_call_activity_links links USING (activity_id)
WHERE spans.source = 'claude' AND spans.run_id = ? AND spans.trace_id <> ''
  AND links.evidence_role = 'corroborating'`, sessionID); err != nil {
		return fmt.Errorf("project Claude corroborating trace supports: %w", err)
	}
	return deriveClaudeTraceMemberships(ctx, transaction, sessionID)
}

func publishClaudeCallChanges(ctx context.Context, transaction *sql.Tx, sequence int64, sessionID string) error {
	rows, err := transaction.QueryContext(ctx, `SELECT activity_id, native_session_id, trace_id
FROM model_call_evidence WHERE source = 'claude' AND native_session_id = ?
UNION
SELECT activity_id, run_id, trace_id FROM spans
WHERE source = 'claude'
  AND run_id = ?
  AND COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), '') = 'corroborating'`, sessionID, sessionID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var activityID, sessionID, traceID string
		if err := rows.Scan(&activityID, &sessionID, &traceID); err != nil {
			return err
		}
		if sessionID != "" {
			if err := appendActivityChange(ctx, transaction, sequence, 0, "session", "claude", sessionID, activityID, "upsert"); err != nil {
				return err
			}
		}
		if traceID != "" {
			if err := appendActivityChange(ctx, transaction, sequence, 0, "trace", "", traceID, activityID, "upsert"); err != nil {
				return err
			}
		}
	}
	return rows.Err()
}

func parseIdentityBasis(value string) modelcall.IdentityBasis {
	switch value {
	case "claude_client_request_id":
		return modelcall.IdentityClaudeClientRequestID
	case "claude_request_id":
		return modelcall.IdentityClaudeRequestID
	case "claude_event_sequence":
		return modelcall.IdentityClaudeEventSequence
	default:
		return modelcall.IdentityJournalEvidenceFallback
	}
}

func costBasisText(value modelcall.CostBasis) string {
	switch value {
	case modelcall.CostProviderReported:
		return "provider_reported"
	case modelcall.CostRateCardEstimate:
		return "rate_card_estimate"
	default:
		return "unavailable"
	}
}
