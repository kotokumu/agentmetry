package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/ingest"
	"github.com/theoden9014/agentmetry/internal/journal"
	"github.com/theoden9014/agentmetry/internal/observation"
	"github.com/theoden9014/agentmetry/internal/storage/ownership"
	storageversion "github.com/theoden9014/agentmetry/internal/storage/version"
	"github.com/theoden9014/agentmetry/sourceplugin"
	_ "modernc.org/sqlite"
)

type Store struct {
	db       *sql.DB
	profiles sourceplugin.Registry
	owner    *ownership.Lock
}

func Open(path string, profiles ...sourceplugin.Registry) (*Store, error) {
	ownershipContext, cancelOwnership := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelOwnership()
	owner, err := ownership.Acquire(ownershipContext, path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path + ".migration.json"); err == nil {
		_ = owner.Close()
		return nil, fmt.Errorf("database has a pending data migration; recover it before opening")
	} else if !os.IsNotExist(err) {
		_ = owner.Close()
		return nil, fmt.Errorf("inspect data migration manifest: %w", err)
	}
	info, statErr := os.Stat(path)
	fresh := os.IsNotExist(statErr) || (statErr == nil && info.Size() == 0)
	if statErr != nil && !fresh {
		_ = owner.Close()
		return nil, fmt.Errorf("inspect sqlite database: %w", statErr)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		_ = owner.Close()
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	store := &Store{db: database, owner: owner}
	if len(profiles) > 0 {
		store.profiles = profiles[0]
	}
	if err := store.configure(context.Background()); err != nil {
		_ = database.Close()
		_ = owner.Close()
		return nil, err
	}
	if err := convergeSchema(context.Background(), database); err != nil {
		_ = database.Close()
		_ = owner.Close()
		return nil, err
	}
	if fresh {
		if _, err := database.Exec(fmt.Sprintf("PRAGMA user_version=%d", storageversion.CurrentGeneration)); err != nil {
			_ = database.Close()
			_ = owner.Close()
			return nil, fmt.Errorf("initialize journal format version: %w", err)
		}
	}
	if err := store.rebuildSessionRollups(context.Background()); err != nil {
		_ = database.Close()
		_ = owner.Close()
		return nil, err
	}
	return store, nil
}

func (store *Store) Close() error {
	databaseErr := store.db.Close()
	ownerErr := store.owner.Close()
	if databaseErr != nil {
		return databaseErr
	}
	return ownerErr
}

func (store *Store) configure(ctx context.Context) error {
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite with %q: %w", statement, err)
		}
	}
	return nil
}

func (store *Store) CommitBatch(ctx context.Context, batch canonical.Batch) error {
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin telemetry commit: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	if err := commitProjection(ctx, transaction, batch); err != nil {
		return err
	}
	if err := rebuildAffectedSessionRollups(ctx, transaction, batch); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit telemetry batch: %w", err)
	}
	return nil
}

func (store *Store) CommitExport(ctx context.Context, accepted ingest.AcceptedExport) error {
	prepared, err := prepareExport(accepted)
	if err != nil {
		return err
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin OTLP export commit: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	exportID, err := insertExport(ctx, transaction, accepted, prepared)
	if err != nil {
		return err
	}
	for _, item := range accepted.Observations {
		if err := insertObservation(ctx, transaction, exportID, item); err != nil {
			return err
		}
	}
	if accepted.NormalizationError == "" {
		if err := commitProjection(ctx, transaction, accepted.Projection); err != nil {
			return err
		}
		if err := rebuildAffectedSessionRollups(ctx, transaction, accepted.Projection); err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit OTLP export: %w", err)
	}
	return nil
}

func commitProjection(ctx context.Context, transaction *sql.Tx, batch canonical.Batch) error {
	for _, span := range batch.Spans {
		if !canonical.IsSemanticSpan(span) {
			continue
		}
		if err := putSpan(ctx, transaction, span); err != nil {
			return err
		}
	}
	for _, log := range batch.Logs {
		if err := appendLog(ctx, transaction, log); err != nil {
			return err
		}
	}
	for _, metric := range batch.Metrics {
		if err := appendMetric(ctx, transaction, metric); err != nil {
			return err
		}
	}
	return nil
}

type preparedExport struct {
	payload journal.Payload
	journal ingest.JournalMetadata
}

func prepareExport(accepted ingest.AcceptedExport) (preparedExport, error) {
	payload, err := journal.Encode(accepted.Envelope.Protobuf)
	if err != nil {
		return preparedExport{}, fmt.Errorf("encode OTLP journal payload: %w", err)
	}
	metadata := accepted.Journal
	if metadata.Source == "" || metadata.NormalizerVersion == 0 || metadata.NormalizationStatus == "" {
		derived := ingest.DeriveJournalMetadata(accepted.Observations, accepted.Projection, accepted.NormalizationError)
		if metadata.Source == "" {
			metadata.Source = derived.Source
		}
		if metadata.NormalizerVersion == 0 {
			metadata.NormalizerVersion = derived.NormalizerVersion
		}
		if metadata.NormalizationStatus == "" {
			metadata.NormalizationStatus = derived.NormalizationStatus
		}
	}
	return preparedExport{payload: payload, journal: metadata}, nil
}

func insertExport(ctx context.Context, transaction *sql.Tx, accepted ingest.AcceptedExport, prepared preparedExport) (int64, error) {
	payload := prepared.payload
	metadata := prepared.journal
	if metadata.NormalizationStatus == "failed" && accepted.NormalizationError == "" {
		return 0, fmt.Errorf("failed journal export has no normalization error")
	}
	hash := payload.SHA256()
	result, err := transaction.ExecContext(ctx, `INSERT INTO otlp_exports (
  received_at, signal, transport, payload_protobuf, payload_codec, payload_sha256,
  payload_size, source, normalizer_version, normalization_status, normalization_error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(accepted.Envelope.ReceivedAt), accepted.Envelope.Signal, accepted.Envelope.Transport,
		payload.Bytes(), payload.Codec(), hex.EncodeToString(hash[:]), payload.OriginalSize(),
		metadata.Source, metadata.NormalizerVersion, metadata.NormalizationStatus, accepted.NormalizationError,
	)
	if err != nil {
		return 0, fmt.Errorf("insert OTLP export: %w", err)
	}
	exportID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read OTLP export identity: %w", err)
	}
	return exportID, nil
}

func insertObservation(ctx context.Context, transaction *sql.Tx, exportID int64, item observation.Observation) error {
	_, err := transaction.ExecContext(ctx, `INSERT INTO observations (
  export_id, ordinal, signal, kind, source, source_event_name, occurred_at, observed_at,
  trace_id, span_id, parent_span_id, session_id, agent_id, agent_definition, agent_type, parent_agent_id,
  model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
  cache_write_tokens_reported, reasoning_tokens_reported,
  normalizer_version
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		exportID, item.Ordinal, item.Signal, item.Kind, item.Source, item.SourceEventName,
		formatTime(item.OccurredAt), formatTime(item.ObservedAt), item.TraceID, item.SpanID,
		item.ParentSpanID, item.SessionID, item.AgentID, item.AgentDefinition, item.AgentType, item.ParentAgentID,
		item.Model, item.Usage.Input, item.Usage.Output, item.Usage.CacheRead,
		item.Usage.CacheWrite, item.Usage.Reasoning,
		boolInt(item.Usage.InputReported()), boolInt(item.Usage.OutputReported()),
		boolInt(item.Usage.CacheReadReported()), boolInt(item.Usage.CacheWriteReported()),
		boolInt(item.Usage.ReasoningReported()), item.NormalizerVersion,
	)
	if err != nil {
		return fmt.Errorf("insert canonical observation: %w", err)
	}
	return nil
}

func putSpan(ctx context.Context, transaction *sql.Tx, span canonical.Span) error {
	attributes, err := json.Marshal(span.Attributes)
	if err != nil {
		return fmt.Errorf("encode span attributes: %w", err)
	}
	const statement = `
INSERT INTO spans (
  source, trace_id, span_id, parent_span_id, name, started_at, ended_at, status,
  activity_kind, tool_name, target_agent_id, target_agent_type, content,
  agent_id, agent_definition, agent_type, parent_agent_id, run_id, model, cost_usd,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
  reasoning_tokens, input_tokens_reported, output_tokens_reported,
  cache_read_tokens_reported, cache_write_tokens_reported, reasoning_tokens_reported,
  attributes_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(trace_id, span_id) DO UPDATE SET
  source=excluded.source,
  parent_span_id=excluded.parent_span_id,
  name=excluded.name,
  started_at=excluded.started_at,
  ended_at=excluded.ended_at,
  status=excluded.status,
  activity_kind=excluded.activity_kind,
  tool_name=excluded.tool_name,
  target_agent_id=excluded.target_agent_id,
  target_agent_type=excluded.target_agent_type,
  content=excluded.content,
  agent_id=excluded.agent_id,
  agent_definition=excluded.agent_definition,
  agent_type=excluded.agent_type,
  parent_agent_id=excluded.parent_agent_id,
  run_id=excluded.run_id,
  model=excluded.model,
  cost_usd=excluded.cost_usd,
  input_tokens=excluded.input_tokens,
  output_tokens=excluded.output_tokens,
  cache_read_tokens=excluded.cache_read_tokens,
  cache_write_tokens=excluded.cache_write_tokens,
  reasoning_tokens=excluded.reasoning_tokens,
  input_tokens_reported=excluded.input_tokens_reported,
  output_tokens_reported=excluded.output_tokens_reported,
  cache_read_tokens_reported=excluded.cache_read_tokens_reported,
  cache_write_tokens_reported=excluded.cache_write_tokens_reported,
  reasoning_tokens_reported=excluded.reasoning_tokens_reported,
  attributes_json=excluded.attributes_json`
	_, err = transaction.ExecContext(ctx, statement,
		normalizeSource(span.Source), span.TraceID, span.SpanID, span.ParentSpanID, span.Name,
		formatTime(span.StartedAt), formatTime(span.EndedAt), span.Status,
		span.Kind, span.ToolName, span.TargetAgentID, span.TargetAgentType, span.Content,
		span.Agent.AgentID, span.Agent.AgentDefinition, span.Agent.AgentType, span.Agent.ParentAgentID,
		span.Agent.RunID, span.Agent.Model, span.CostUSD,
		span.Agent.Tokens.Input, span.Agent.Tokens.Output, span.Agent.Tokens.CacheRead,
		span.Agent.Tokens.CacheWrite, span.Agent.Tokens.Reasoning,
		boolInt(span.Agent.Tokens.InputReported()), boolInt(span.Agent.Tokens.OutputReported()),
		boolInt(span.Agent.Tokens.CacheReadReported()), boolInt(span.Agent.Tokens.CacheWriteReported()),
		boolInt(span.Agent.Tokens.ReasoningReported()), string(attributes),
	)
	if err != nil {
		return fmt.Errorf("put span: %w", err)
	}
	return nil
}

func appendLog(ctx context.Context, transaction *sql.Tx, log canonical.Log) error {
	attributes, err := json.Marshal(log.Attributes)
	if err != nil {
		return fmt.Errorf("encode log attributes: %w", err)
	}
	const statement = `INSERT INTO logs (
  source, observed_at, severity, name, body, trace_id, span_id, activity_kind, tool_name,
  target_agent_id, target_agent_type, agent_id, agent_definition, agent_type, parent_agent_id, run_id, model, cost_usd,
  input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, reasoning_tokens,
  input_tokens_reported, output_tokens_reported, cache_read_tokens_reported,
  cache_write_tokens_reported, reasoning_tokens_reported, attributes_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = transaction.ExecContext(ctx, statement,
		normalizeSource(log.Source), formatTime(log.ObservedAt), log.Severity, log.Name, log.Body, log.TraceID, log.SpanID,
		log.Kind, log.ToolName, log.TargetAgentID, log.TargetAgentType, log.Agent.AgentID, log.Agent.AgentDefinition, log.Agent.AgentType,
		log.Agent.ParentAgentID, log.Agent.RunID, log.Agent.Model, log.CostUSD,
		log.Agent.Tokens.Input, log.Agent.Tokens.Output, log.Agent.Tokens.CacheRead,
		log.Agent.Tokens.CacheWrite, log.Agent.Tokens.Reasoning,
		boolInt(log.Agent.Tokens.InputReported()), boolInt(log.Agent.Tokens.OutputReported()),
		boolInt(log.Agent.Tokens.CacheReadReported()), boolInt(log.Agent.Tokens.CacheWriteReported()),
		boolInt(log.Agent.Tokens.ReasoningReported()), string(attributes),
	)
	if err != nil {
		return fmt.Errorf("append log: %w", err)
	}
	return nil
}

func appendMetric(ctx context.Context, transaction *sql.Tx, metric canonical.MetricPoint) error {
	attributes, err := json.Marshal(metric.Attributes)
	if err != nil {
		return fmt.Errorf("encode metric attributes: %w", err)
	}
	const statement = `INSERT INTO metrics (
  source, observed_at, name, kind, value, agent_id, agent_definition, agent_type, parent_agent_id, run_id,
  model, cost_usd, attributes_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = transaction.ExecContext(ctx, statement,
		normalizeSource(metric.Source), formatTime(metric.ObservedAt), metric.Name, metric.Kind, metric.Value,
		metric.Agent.AgentID, metric.Agent.AgentDefinition, metric.Agent.AgentType, metric.Agent.ParentAgentID,
		metric.Agent.RunID, metric.Agent.Model, metric.CostUSD, string(attributes),
	)
	if err != nil {
		return fmt.Errorf("append metric: %w", err)
	}
	return nil
}

func normalizeSource(source string) string {
	if source == "" {
		return "unknown"
	}
	return source
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (store *Store) JournalMode(ctx context.Context) (string, error) {
	var mode string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return "", fmt.Errorf("read journal mode: %w", err)
	}
	return mode, nil
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return time.Unix(0, 0).UTC().Format(time.RFC3339Nano)
	}
	return value.UTC().Format(time.RFC3339Nano)
}
