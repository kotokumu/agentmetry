package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
)

const (
	projectionFeedRetention = 50_000
	projectionFeedByteLimit = 128 * 1024 * 1024
	projectionTargetLimit   = 1_024
	projectionPayloadLimit  = 2_048
)

func (store *Store) initializeProjectionFeed(ctx context.Context) error {
	var generation string
	err := store.db.QueryRowContext(ctx, `SELECT generation FROM projection_feed_state WHERE id = 1`).Scan(&generation)
	if err == nil {
		store.generation = generation
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read projection generation: %w", err)
	}
	random := make([]byte, 16)
	if _, err := randRead(random); err != nil {
		return fmt.Errorf("create projection generation: %w", err)
	}
	generation = hex.EncodeToString(random)
	_, err = store.db.ExecContext(ctx, `INSERT INTO projection_feed_state (id, generation) VALUES (1, ?)`, generation)
	if err != nil {
		return fmt.Errorf("store projection generation: %w", err)
	}
	store.generation = generation
	return nil
}

var randRead = func(value []byte) (int, error) { return rand.Read(value) }

func (store *Store) activityID(key string) string {
	identity := sha256.Sum256([]byte(store.generation + "\x00" + key))
	return hex.EncodeToString(identity[:16])
}

func (store *Store) newActivityID() (string, error) {
	random := make([]byte, 16)
	if _, err := randRead(random); err != nil {
		return "", fmt.Errorf("create activity identity: %w", err)
	}
	return store.activityID("log:" + hex.EncodeToString(random)), nil
}

type storedSpanScope struct {
	source, session, trace string
	agentID, activityKind  string
	parentSpanID, status   string
	startedAt, endedAt     string
	input, output          int64
	cacheRead, cacheWrite  int64
	reasoning              int64
	inputReported          bool
	outputReported         bool
	cacheReadReported      bool
	cacheWriteReported     bool
	reasoningReported      bool
	missingParent          bool
	cost                   sql.NullFloat64
	present                bool
}

type storedSpanKey struct {
	traceID string
	spanID  string
}

type projectionPlan struct {
	targets          []query.ChangeTarget
	previousSpans    map[storedSpanKey]storedSpanScope
	previousSessions map[sessionKey]struct{}
	incremental      bool
}

func buildProjectionPlan(ctx context.Context, transaction *sql.Tx, batch canonical.Batch) (projectionPlan, error) {
	keys := make([]map[string]string, 0, len(batch.Spans))
	for _, span := range batch.Spans {
		if canonical.IsSemanticSpan(span) {
			keys = append(keys, map[string]string{"trace": span.TraceID, "span": span.SpanID})
		}
	}
	previousSpans := make(map[storedSpanKey]storedSpanScope)
	if len(keys) > 0 {
		payload, err := json.Marshal(keys)
		if err != nil {
			return projectionPlan{}, fmt.Errorf("encode projected span keys: %w", err)
		}
		rows, err := transaction.QueryContext(ctx, `WITH requested AS (
  SELECT json_extract(value, '$.trace') AS trace_id, json_extract(value, '$.span') AS span_id
  FROM json_each(?)
)
SELECT spans.trace_id, spans.span_id, spans.source, spans.run_id, spans.agent_id, spans.activity_kind,
  spans.parent_span_id, spans.status, spans.started_at, spans.ended_at,
  spans.input_tokens, spans.output_tokens, spans.cache_read_tokens,
  spans.cache_write_tokens, spans.reasoning_tokens, spans.input_tokens_reported,
  spans.output_tokens_reported, spans.cache_read_tokens_reported,
  spans.cache_write_tokens_reported, spans.reasoning_tokens_reported, spans.cost_usd,
  CASE WHEN spans.parent_span_id <> '' AND NOT EXISTS (
    SELECT 1 FROM spans AS parent WHERE parent.trace_id = spans.trace_id AND parent.span_id = spans.parent_span_id
  ) THEN 1 ELSE 0 END
FROM spans JOIN requested USING (trace_id, span_id)`, string(payload))
		if err != nil {
			return projectionPlan{}, fmt.Errorf("read previous span scopes: %w", err)
		}
		for rows.Next() {
			var key storedSpanKey
			var scope storedSpanScope
			if err := rows.Scan(
				&key.traceID, &key.spanID, &scope.source, &scope.session, &scope.agentID, &scope.activityKind,
				&scope.parentSpanID, &scope.status, &scope.startedAt, &scope.endedAt,
				&scope.input, &scope.output, &scope.cacheRead, &scope.cacheWrite, &scope.reasoning,
				&scope.inputReported, &scope.outputReported, &scope.cacheReadReported,
				&scope.cacheWriteReported, &scope.reasoningReported, &scope.cost, &scope.missingParent,
			); err != nil {
				_ = rows.Close()
				return projectionPlan{}, fmt.Errorf("scan previous span scope: %w", err)
			}
			scope.trace = key.traceID
			scope.present = true
			previousSpans[key] = scope
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return projectionPlan{}, fmt.Errorf("iterate previous span scopes: %w", err)
		}
		if err := rows.Close(); err != nil {
			return projectionPlan{}, fmt.Errorf("close previous span scopes: %w", err)
		}
	}

	targetSet := query.NewChangeTargetSet(projectionTargetLimit)
	for _, target := range projectionTargets(batch) {
		targetSet.Add(target)
	}
	previousSessions := make(map[sessionKey]struct{})
	for _, scope := range previousSpans {
		addActivityTargets(targetSet, scope.source, scope.session, scope.trace)
		if scope.session != "" {
			previousSessions[sessionKey{source: scope.source, runID: scope.session}] = struct{}{}
		}
	}
	return projectionPlan{
		targets:          boundTargetPayload(targetSet.Values()),
		previousSpans:    previousSpans,
		previousSessions: previousSessions,
		incremental:      rollupCanApplyIncrementally(batch, previousSpans),
	}, nil
}

func rollupCanApplyIncrementally(batch canonical.Batch, previous map[storedSpanKey]storedSpanScope) bool {
	for _, span := range batch.Spans {
		if !canonical.IsSemanticSpan(span) {
			continue
		}
		old, exists := previous[storedSpanKey{traceID: span.TraceID, spanID: span.SpanID}]
		if !exists {
			continue
		}
		newTokens := span.Agent.Tokens
		if old.activityKind != string(span.Kind) || old.source != normalizeSource(span.Source) || old.session != span.Agent.RunID ||
			normalizedAgentID(old.agentID) != normalizedAgentID(span.Agent.AgentID) ||
			formatTime(span.EndedAt) < old.endedAt ||
			(old.inputReported && !newTokens.InputReported()) ||
			(old.outputReported && !newTokens.OutputReported()) ||
			(old.cacheReadReported && !newTokens.CacheReadReported()) ||
			(old.cacheWriteReported && !newTokens.CacheWriteReported()) ||
			(old.reasoningReported && !newTokens.ReasoningReported()) ||
			(old.cost.Valid && span.CostUSD == nil) {
			return false
		}
	}
	return true
}

func appendActivityChange(ctx context.Context, transaction *sql.Tx, sequence int64, ordinal int, scopeKind, source, scopeID, activityID, operation string) error {
	if sequence == 0 {
		return nil
	}
	_, err := transaction.ExecContext(ctx, `INSERT INTO activity_changes (sequence, scope_kind, source, scope_id, activity_id, operation) VALUES (?, ?, ?, ?, ?, ?)`, sequence, scopeKind, source, scopeID, activityID, operation)
	if err != nil {
		return fmt.Errorf("append activity change: %w", err)
	}
	mutationBytes := len(scopeKind) + len(source) + len(scopeID) + len(activityID) + len(operation) + 32
	if _, err := transaction.ExecContext(ctx, `UPDATE projection_changes SET mutation_bytes = mutation_bytes + ? WHERE sequence = ?`, mutationBytes, sequence); err != nil {
		return fmt.Errorf("account activity change bytes: %w", err)
	}
	return nil
}

func projectionTargets(batch canonical.Batch) []query.ChangeTarget {
	set := query.NewChangeTargetSet(projectionTargetLimit)
	for _, span := range batch.Spans {
		if !canonical.IsSemanticSpan(span) {
			continue
		}
		addActivityTargets(set, span.Source, span.Agent.RunID, span.TraceID)
	}
	for _, log := range batch.Logs {
		addActivityTargets(set, log.Source, log.Agent.RunID, log.TraceID)
	}
	for _, metric := range batch.Metrics {
		set.Add(query.OverviewTarget())
		set.Add(query.SourceTarget(normalizeSource(metric.Source)))
	}
	return boundTargetPayload(set.Values())
}

func addActivityTargets(set *query.ChangeTargetSet, source, session, trace string) {
	set.Add(query.OverviewTarget())
	set.Add(query.SourceTarget(normalizeSource(source)))
	if session != "" {
		set.Add(query.SessionTarget(normalizeSource(source), session))
	}
	if trace != "" {
		set.Add(query.TraceTarget(trace))
	}
}

func boundTargetPayload(targets []query.ChangeTarget) []query.ChangeTarget {
	encoded, _ := json.Marshal(targets)
	if len(encoded) <= projectionPayloadLimit {
		return targets
	}
	set := query.NewChangeTargetSet(1)
	for _, target := range targets {
		switch target.Kind {
		case query.ChangeTargetSource:
			set.Add(query.AllSourcesTarget())
		case query.ChangeTargetSession:
			set.Add(query.AllSessionsTarget())
		case query.ChangeTargetTrace:
			set.Add(query.AllTracesTarget())
		default:
			set.Add(target)
		}
	}
	return set.Values()
}

func appendProjectionChange(ctx context.Context, transaction *sql.Tx, targets []query.ChangeTarget) (int64, error) {
	if len(targets) == 0 {
		return 0, nil
	}
	payload, err := json.Marshal(targets)
	if err != nil {
		return 0, fmt.Errorf("encode projection targets: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `INSERT INTO projection_changes (committed_at, targets_json, target_bytes, mutation_bytes) VALUES (?, ?, ?, 0)`, formatTime(time.Now()), string(payload), len(payload))
	if err != nil {
		return 0, fmt.Errorf("append projection change: %w", err)
	}
	sequence, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read projection sequence: %w", err)
	}
	if sequence%512 != 0 {
		return sequence, nil
	}
	if err := retainProjectionChanges(ctx, transaction); err != nil {
		return 0, err
	}
	return sequence, nil
}

func retainProjectionChanges(ctx context.Context, transaction *sql.Tx) error {
	for {
		var count, bytes int64
		if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(target_bytes + mutation_bytes), 0) FROM projection_changes`).Scan(&count, &bytes); err != nil {
			return fmt.Errorf("inspect projection retention: %w", err)
		}
		excessRows := max(int64(0), count-projectionFeedRetention)
		excessBytes := max(int64(0), bytes-projectionFeedByteLimit)
		if excessRows == 0 && excessBytes == 0 {
			return nil
		}
		rows, err := transaction.QueryContext(ctx, `SELECT sequence, target_bytes + mutation_bytes
FROM projection_changes WHERE sequence < (SELECT MAX(sequence) FROM projection_changes)
ORDER BY sequence LIMIT 1000`)
		if err != nil {
			return fmt.Errorf("select projection retention chunk: %w", err)
		}
		var cutoff, removedRows, removedBytes int64
		for rows.Next() {
			var sequence, rowBytes int64
			if err := rows.Scan(&sequence, &rowBytes); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan projection retention chunk: %w", err)
			}
			cutoff = sequence
			removedRows++
			removedBytes += rowBytes
			if removedRows >= excessRows && removedBytes >= excessBytes {
				break
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate projection retention chunk: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close projection retention chunk: %w", err)
		}
		if cutoff == 0 {
			// One retained commit may itself exceed the byte budget. Keeping the
			// newest cursor is safer than deleting the only observable change.
			return nil
		}
		if _, err := transaction.ExecContext(ctx, `DELETE FROM projection_changes WHERE sequence <= ?`, cutoff); err != nil {
			return fmt.Errorf("retain projection changes: %w", err)
		}
	}
}

func (store *Store) CurrentProjectionPosition(ctx context.Context) (query.ProjectionPosition, error) {
	var position query.ProjectionPosition
	err := store.readDB.QueryRowContext(ctx, `SELECT generation, COALESCE((SELECT MAX(sequence) FROM projection_changes), 0) FROM projection_feed_state WHERE id = 1`).Scan(&position.Generation, &position.Sequence)
	if err != nil {
		return query.ProjectionPosition{}, fmt.Errorf("read projection position: %w", err)
	}
	return position, nil
}

func (store *Store) ReadProjectionChanges(ctx context.Context, after query.ProjectionPosition, commitLimit, targetLimit int) (query.ProjectionChangeWindow, error) {
	transaction, err := store.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return query.ProjectionChangeWindow{}, fmt.Errorf("begin projection feed snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	current, earliest, err := projectionBoundsTx(ctx, transaction)
	if err != nil {
		return query.ProjectionChangeWindow{}, err
	}
	if err := query.ValidateProjectionPosition(current, after); err != nil {
		return query.ProjectionChangeWindow{}, err
	}
	if earliest.Valid && after.Sequence < earliest.Int64-1 {
		return query.ProjectionChangeWindow{}, query.ErrProjectionCursorExpired
	}
	if commitLimit < 1 || commitLimit > 256 {
		commitLimit = 256
	}
	if targetLimit < 1 || targetLimit > 1024 {
		targetLimit = 1024
	}
	rows, err := transaction.QueryContext(ctx, `SELECT sequence, targets_json FROM projection_changes WHERE sequence > ? AND sequence <= ? ORDER BY sequence LIMIT ?`, after.Sequence, current.Sequence, commitLimit)
	if err != nil {
		return query.ProjectionChangeWindow{}, fmt.Errorf("read projection changes: %w", err)
	}
	defer rows.Close()
	set := query.NewChangeTargetSet(targetLimit)
	through := after
	for rows.Next() {
		var sequence int64
		var payload string
		if err := rows.Scan(&sequence, &payload); err != nil {
			return query.ProjectionChangeWindow{}, err
		}
		var targets []query.ChangeTarget
		if err := json.Unmarshal([]byte(payload), &targets); err != nil {
			return query.ProjectionChangeWindow{}, fmt.Errorf("decode projection targets: %w", err)
		}
		for _, target := range targets {
			set.Add(target)
		}
		through = query.ProjectionPosition{Generation: current.Generation, Sequence: sequence}
	}
	if err := rows.Err(); err != nil {
		return query.ProjectionChangeWindow{}, err
	}
	if err := rows.Close(); err != nil {
		return query.ProjectionChangeWindow{}, err
	}
	if err := transaction.Commit(); err != nil {
		return query.ProjectionChangeWindow{}, fmt.Errorf("commit projection feed snapshot: %w", err)
	}
	return query.ProjectionChangeWindow{From: after, Through: through, Targets: set.Values()}, nil
}

func (store *Store) WaitForProjectionChange(ctx context.Context, after query.ProjectionPosition) error {
	current, err := store.CurrentProjectionPosition(ctx)
	if err != nil {
		return err
	}
	if current.Generation != after.Generation || current.Sequence > after.Sequence {
		return nil
	}
	store.notifyMu.Lock()
	wake := store.notify
	store.notifyMu.Unlock()
	current, err = store.CurrentProjectionPosition(ctx)
	if err != nil || current.Generation != after.Generation || current.Sequence > after.Sequence {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wake:
		return nil
	}
}

func (store *Store) signalProjectionChange() {
	store.notifyMu.Lock()
	close(store.notify)
	store.notify = make(chan struct{})
	store.notifyMu.Unlock()
}
