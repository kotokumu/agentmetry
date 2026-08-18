package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
)

const claudeSource = "claude"

type claudeModelCallKey struct {
	sessionKey
	usageID string
}

type claudeRuntimeAgentEvidence struct {
	agentID       string
	parentAgentID string
}

type claudeRequestLogProjection struct {
	id, activityID, traceID, usageID   string
	agentID, parentAgentID             string
	sourceAgentID, sourceParentAgentID string
	projectionSequence                 int64
}

type claudeAttributionResult struct {
	rebuildSessions bool
	traceIDs        map[string]struct{}
}

// reconcileClaudeModelCallAgents combines the independent authorities emitted
// for one Claude model call. The request log keeps usage and descriptive
// metadata; the corroborating trace supplies runtime agent identity.
func reconcileClaudeModelCallAgents(ctx context.Context, transaction *sql.Tx, batch canonical.Batch, previous map[storedSpanKey]storedSpanScope, sequence int64) (claudeAttributionResult, error) {
	result := claudeAttributionResult{traceIDs: make(map[string]struct{})}
	for key, usageIDs := range groupClaudeModelCallKeys(claudeModelCallKeys(batch, previous)) {
		evidence, err := loadClaudeRuntimeAgentEvidence(ctx, transaction, key, usageIDs)
		if err != nil {
			return result, err
		}
		logs, err := loadClaudeRequestLogProjections(ctx, transaction, key, usageIDs)
		if err != nil {
			return result, err
		}
		historicalAgentIDs := make(map[string]struct{})
		for _, log := range logs {
			agentID, parentAgentID := log.sourceAgentID, log.sourceParentAgentID
			if runtime, exists := evidence[log.usageID]; exists {
				agentID, parentAgentID = runtime.agentID, runtime.parentAgentID
			}
			if log.agentID == agentID && log.parentAgentID == parentAgentID {
				continue
			}
			if _, err := transaction.ExecContext(ctx, `UPDATE logs
SET agent_id = ?, parent_agent_id = ? WHERE id = ?`, agentID, parentAgentID, log.id); err != nil {
				return result, fmt.Errorf("attribute Claude request log to runtime agent: %w", err)
			}
			if log.projectionSequence != sequence {
				historicalAgentIDs[normalizedAgentID(log.agentID, key.runID)] = struct{}{}
				historicalAgentIDs[normalizedAgentID(agentID, key.runID)] = struct{}{}
				if log.traceID != "" {
					result.traceIDs[log.traceID] = struct{}{}
				}
			}
			if err := appendActivityChange(ctx, transaction, sequence, 0, "session", key.source, key.runID, log.activityID, "upsert"); err != nil {
				return result, err
			}
			if log.traceID != "" {
				if err := appendActivityChange(ctx, transaction, sequence, 0, "trace", "", log.traceID, log.activityID, "upsert"); err != nil {
					return result, err
				}
			}
		}
		if err := rebuildHistoricalSessionAgents(ctx, transaction, key, historicalAgentIDs, sequence); err != nil {
			return result, err
		}
		if len(historicalAgentIDs) > 0 && len(previous) > 0 {
			result.rebuildSessions = true
		}
	}
	return result, nil
}

func rebuildClaudeAttributionTraces(ctx context.Context, transaction *sql.Tx, traceIDs map[string]struct{}) error {
	for traceID := range traceIDs {
		if err := rebuildTraceRollupTx(ctx, transaction, traceID); err != nil {
			return fmt.Errorf("rebuild Claude attribution trace %s: %w", traceID, err)
		}
	}
	return nil
}

func rebuildHistoricalSessionAgents(ctx context.Context, transaction *sql.Tx, key sessionKey, normalizedIDs map[string]struct{}, sequence int64) error {
	if len(normalizedIDs) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(normalizedIDs))
	rawSet := make(map[string]struct{})
	for agentID := range normalizedIDs {
		normalized = append(normalized, agentID)
		if agentID == "main" {
			rawSet[""] = struct{}{}
			rawSet["main"] = struct{}{}
			rawSet[key.runID] = struct{}{}
			continue
		}
		rawSet[agentID] = struct{}{}
	}
	normalizedPayload, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("encode historical Claude agent IDs: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM session_agents
WHERE source = ? AND run_id = ? AND agent_id IN (SELECT value FROM json_each(?))`,
		key.source, key.runID, string(normalizedPayload)); err != nil {
		return fmt.Errorf("clear historical Claude agent rollups: %w", err)
	}
	raw := make([]string, 0, len(rawSet))
	for agentID := range rawSet {
		raw = append(raw, agentID)
	}
	rawPayload, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("encode historical Claude raw agent IDs: %w", err)
	}
	filter := `AND source = ? AND run_id = ?
  AND agent_id IN (SELECT value FROM json_each(?)) AND projection_sequence <> ?`
	_, err = transaction.ExecContext(ctx, sessionAgentAggregateInsert(filter, filter),
		key.source, key.runID, string(rawPayload), sequence,
		key.source, key.runID, string(rawPayload), sequence,
	)
	if err != nil {
		return fmt.Errorf("rebuild historical Claude agent rollups: %w", err)
	}
	return nil
}

func claudeModelCallKeys(batch canonical.Batch, previous map[storedSpanKey]storedSpanScope) map[claudeModelCallKey]struct{} {
	keys := make(map[claudeModelCallKey]struct{})
	for _, span := range batch.Spans {
		if normalizeSource(span.Source) == claudeSource && span.Agent.RunID != "" &&
			canonicalUsageRole(span.Attributes) == "corroborating" && canonicalUsageID(span.Attributes) != "" {
			keys[claudeModelCallKey{sessionKey: sessionKey{source: claudeSource, runID: span.Agent.RunID}, usageID: canonicalUsageID(span.Attributes)}] = struct{}{}
		}
	}
	for _, log := range batch.Logs {
		if normalizeSource(log.Source) == claudeSource && log.Agent.RunID != "" &&
			canonicalUsageRole(log.Attributes) == "authoritative_call" && canonicalUsageID(log.Attributes) != "" {
			keys[claudeModelCallKey{sessionKey: sessionKey{source: claudeSource, runID: log.Agent.RunID}, usageID: canonicalUsageID(log.Attributes)}] = struct{}{}
		}
	}
	for _, scope := range previous {
		if scope.source == claudeSource && scope.session != "" && scope.usageRole == "corroborating" && scope.usageID != "" {
			keys[claudeModelCallKey{sessionKey: sessionKey{source: scope.source, runID: scope.session}, usageID: scope.usageID}] = struct{}{}
		}
	}
	return keys
}

func groupClaudeModelCallKeys(keys map[claudeModelCallKey]struct{}) map[sessionKey][]string {
	grouped := make(map[sessionKey][]string)
	for key := range keys {
		grouped[key.sessionKey] = append(grouped[key.sessionKey], key.usageID)
	}
	return grouped
}

func canonicalUsageID(attributes map[string]any) string {
	value, _ := attributes["gen_ai.usage.id"].(string)
	return value
}

func canonicalUsageRole(attributes map[string]any) string {
	value, _ := attributes["gen_ai.usage.role"].(string)
	return value
}

func addClaudeModelCallTargets(ctx context.Context, transaction *sql.Tx, targets *query.ChangeTargetSet, batch canonical.Batch, previous map[storedSpanKey]storedSpanScope) error {
	for key, usageIDs := range groupClaudeModelCallKeys(claudeModelCallKeys(batch, previous)) {
		payload, err := json.Marshal(usageIDs)
		if err != nil {
			return fmt.Errorf("encode Claude model-call target usage IDs: %w", err)
		}
		rows, err := transaction.QueryContext(ctx, `SELECT DISTINCT trace_id FROM logs
WHERE source = ? AND run_id = ? AND usage_id IN (SELECT value FROM json_each(?)) AND trace_id <> ''`,
			key.source, key.runID, string(payload))
		if err != nil {
			return fmt.Errorf("load Claude model-call trace targets: %w", err)
		}
		for rows.Next() {
			var traceID string
			if err := rows.Scan(&traceID); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan Claude model-call trace target: %w", err)
			}
			targets.Add(query.TraceTarget(traceID))
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate Claude model-call trace targets: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close Claude model-call trace targets: %w", err)
		}
	}
	return nil
}

func loadClaudeRuntimeAgentEvidence(ctx context.Context, transaction *sql.Tx, key sessionKey, usageIDs []string) (map[string]claudeRuntimeAgentEvidence, error) {
	payload, err := json.Marshal(usageIDs)
	if err != nil {
		return nil, fmt.Errorf("encode Claude runtime evidence usage IDs: %w", err)
	}
	rows, err := transaction.QueryContext(ctx, `SELECT usage_id, agent_id, parent_agent_id
FROM spans
WHERE source = ? AND run_id = ? AND usage_id IN (SELECT value FROM json_each(?))
  AND COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), '') = 'corroborating'
  AND agent_id <> ''
ORDER BY ended_at DESC, span_id DESC`, key.source, key.runID, string(payload))
	if err != nil {
		return nil, fmt.Errorf("load Claude runtime agent evidence: %w", err)
	}
	defer rows.Close()
	evidence := make(map[string]claudeRuntimeAgentEvidence)
	for rows.Next() {
		var usageID string
		var runtime claudeRuntimeAgentEvidence
		if err := rows.Scan(&usageID, &runtime.agentID, &runtime.parentAgentID); err != nil {
			return nil, fmt.Errorf("scan Claude runtime agent evidence: %w", err)
		}
		if _, exists := evidence[usageID]; !exists {
			evidence[usageID] = runtime
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Claude runtime agent evidence: %w", err)
	}
	return evidence, nil
}

func loadClaudeRequestLogProjections(ctx context.Context, transaction *sql.Tx, key sessionKey, usageIDs []string) ([]claudeRequestLogProjection, error) {
	payload, err := json.Marshal(usageIDs)
	if err != nil {
		return nil, fmt.Errorf("encode Claude request log usage IDs: %w", err)
	}
	rows, err := transaction.QueryContext(ctx, `SELECT id, activity_id, trace_id, usage_id, agent_id, parent_agent_id,
  COALESCE(NULLIF(json_extract(attributes_json, '$."gen_ai.agent.id"'), ''), NULLIF(json_extract(attributes_json, '$."agent.id"'), ''), ''),
  COALESCE(json_extract(attributes_json, '$."gen_ai.agent.parent.id"'), ''), projection_sequence
FROM logs
WHERE source = ? AND run_id = ? AND usage_id IN (SELECT value FROM json_each(?))
  AND COALESCE(json_extract(attributes_json, '$."gen_ai.usage.role"'), '') = 'authoritative_call'`,
		key.source, key.runID, string(payload))
	if err != nil {
		return nil, fmt.Errorf("load Claude request log projections: %w", err)
	}
	defer rows.Close()
	logs := make([]claudeRequestLogProjection, 0)
	for rows.Next() {
		var log claudeRequestLogProjection
		if err := rows.Scan(
			&log.id, &log.activityID, &log.traceID, &log.usageID, &log.agentID, &log.parentAgentID,
			&log.sourceAgentID, &log.sourceParentAgentID, &log.projectionSequence,
		); err != nil {
			return nil, fmt.Errorf("scan Claude request log projection: %w", err)
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Claude request log projections: %w", err)
	}
	return logs, nil
}
