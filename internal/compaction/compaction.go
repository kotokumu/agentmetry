package compaction

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/kotokumu/agentmetry/internal/billing"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/journal"
	"github.com/kotokumu/agentmetry/internal/storage/ownership"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
	storageversion "github.com/kotokumu/agentmetry/internal/storage/version"
	source "github.com/kotokumu/agentmetry/sourceplugin"
	_ "modernc.org/sqlite"
)

const CurrentStorageGeneration = storageversion.CurrentGeneration

const (
	maxReplayBatchRecords = 256
	maxReplayBatchBytes   = journal.MaxPayloadBytes
	maxReplayWorkers      = 8
)

type ProgressStage string

const (
	ProgressReplay      ProgressStage = "replay"
	ProgressProjection  ProgressStage = "projection"
	ProgressValidation  ProgressStage = "validation"
	ProgressReplacement ProgressStage = "replacement"
)

type Progress struct {
	Stage     ProgressStage
	Completed int64
	Total     int64
}

type Result struct {
	Migrated      bool
	CandidatePath string
	Exports       int64
	SemanticSpans int64
	SourceBytes   int64
	CompactBytes  int64
}

// MigrateIfNeeded is the distributed application upgrade entrypoint. It owns
// recovery, detection, candidate construction, validation, and replacement as
// one operation.
func MigrateIfNeeded(ctx context.Context, sourcePath string, profiles source.Registry, report func(Progress)) (Result, error) {
	return migrate(ctx, sourcePath, profiles, report, false)
}

// Migrate forces a rebuild using the current Atlas schema and Go data format.
func Migrate(ctx context.Context, sourcePath string, profiles source.Registry, report func(Progress)) (Result, error) {
	return migrate(ctx, sourcePath, profiles, report, true)
}

func migrate(ctx context.Context, sourcePath string, profiles source.Registry, report func(Progress), force bool) (Result, error) {
	owner, err := ownership.Acquire(ctx, sourcePath)
	if err != nil {
		return Result{}, err
	}
	defer owner.Close()
	if err := recoverOwned(sourcePath); err != nil {
		return Result{}, err
	}
	info, err := os.Stat(sourcePath)
	if os.IsNotExist(err) {
		return Result{}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("inspect telemetry database: %w", err)
	}
	if info.Size() == 0 {
		return Result{}, nil
	}
	if err := store.RecoverRetentionLifecycleOwned(ctx, sourcePath); err != nil {
		return Result{}, fmt.Errorf("recover telemetry retention lifecycle: %w", err)
	}
	needed, err := needsDataMigration(ctx, sourcePath)
	if err != nil {
		return Result{}, err
	}
	if !force && !needed {
		return Result{}, nil
	}
	result, err := buildCandidate(ctx, sourcePath, info.Size(), profiles, report)
	if err != nil {
		return Result{}, err
	}
	if report != nil {
		report(Progress{Stage: ProgressReplacement, Completed: result.Exports, Total: result.Exports})
	}
	if err := installValidated(result, sourcePath); err != nil {
		return Result{}, err
	}
	result.Migrated = true
	return result, nil
}

func needsDataMigration(ctx context.Context, sourcePath string) (bool, error) {
	database, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		return false, fmt.Errorf("open database migration metadata: %w", err)
	}
	defer database.Close()
	var version int
	if err := database.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return false, fmt.Errorf("read data format version: %w", err)
	}
	if version > CurrentStorageGeneration {
		return false, fmt.Errorf("database storage generation %d is newer than supported %d", version, CurrentStorageGeneration)
	}
	exportsExist, err := tableExists(ctx, database, "otlp_exports")
	if err != nil {
		return false, err
	}
	if !exportsExist {
		return false, fmt.Errorf("database generation %d has no lossless otlp_exports journal", version)
	}
	if version == CurrentStorageGeneration {
		rebuild, err := store.RequiresProjectionRebuild(ctx, database)
		if err != nil {
			return false, fmt.Errorf("inspect Atlas projection migration: %w", err)
		}
		return rebuild, nil
	}
	return true, nil
}

func buildCandidate(ctx context.Context, sourcePath string, sourceBytes int64, profiles source.Registry, report func(Progress)) (Result, error) {
	candidatePath := sourcePath + ".compacting"
	if err := removeTemporaryDatabaseFamily(candidatePath); err != nil {
		return Result{}, fmt.Errorf("remove stale compaction candidate: %w", err)
	}
	fail := func(err error) (Result, error) {
		_ = removeTemporaryDatabaseFamily(candidatePath)
		return Result{}, err
	}
	sourceDB, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		return fail(fmt.Errorf("open legacy database: %w", err))
	}
	if err := configureMigrationSource(ctx, sourceDB); err != nil {
		_ = sourceDB.Close()
		return fail(err)
	}
	tx, err := sourceDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		_ = sourceDB.Close()
		return fail(fmt.Errorf("begin legacy snapshot: %w", err))
	}
	closeSource := func() error {
		_ = tx.Rollback()
		return sourceDB.Close()
	}
	pipeline := newReplayPipeline(ctx)
	defer pipeline.Close()
	reader, err := newLegacyReader(pipeline.ctx, tx)
	if err != nil {
		_ = closeSource()
		return fail(err)
	}
	rateHistory, hasRateHistory, err := readRateHistory(ctx, tx)
	if err != nil {
		_ = reader.Close()
		_ = closeSource()
		return fail(err)
	}
	if !hasRateHistory {
		rateHistory = billing.BuiltinOpenAIRates()
	}
	destination, err := store.OpenReplayCandidate(ctx, candidatePath, store.ReplayCandidateConfig{
		Profiles: profiles, RateHistory: rateHistory,
	})
	if err != nil {
		_ = reader.Close()
		_ = closeSource()
		return fail(fmt.Errorf("create Atlas-schema candidate: %w", err))
	}
	expected := validationExpectation{}
	expected.rates = int64(len(rateHistory))
	if report != nil {
		report(Progress{Stage: ProgressReplay, Total: reader.Total()})
	}
	preparedSource := &preparingReplaySource{
		chunks: replayChunkSource{reader: reader}, profiles: profiles,
	}
	err = pipeline.commitPreparedBatches(preparedSource, destination, func(batch replayBatch) error {
		for _, item := range batch.items {
			if err := expected.add(item.record, item.accepted); err != nil {
				return err
			}
		}
		if report != nil {
			report(Progress{Stage: ProgressReplay, Completed: batch.lastOrdinal(), Total: reader.Total()})
		}
		return nil
	})
	if err != nil {
		return failBuild(destination, reader, closeSource, candidatePath, err)
	}
	if err := reader.Close(); err != nil {
		_ = destination.Close()
		_ = closeSource()
		return fail(err)
	}
	if report != nil {
		report(Progress{Stage: ProgressProjection, Completed: reader.Total(), Total: reader.Total()})
	}
	if err := destination.FinalizeReplay(ctx); err != nil {
		return failBuild(destination, reader, closeSource, candidatePath, fmt.Errorf("rebuild compact projections: %w", err))
	}
	if err := destination.Close(); err != nil {
		_ = closeSource()
		return fail(fmt.Errorf("close compact database: %w", err))
	}
	planCount, err := copyPlanUsageSnapshots(ctx, tx, candidatePath)
	if err != nil {
		_ = closeSource()
		return fail(err)
	}
	expected.planSnapshots = planCount
	if err := copyRetentionCatalog(ctx, tx, candidatePath); err != nil {
		_ = closeSource()
		return fail(err)
	}
	if err := closeSource(); err != nil {
		return fail(fmt.Errorf("close legacy database: %w", err))
	}
	if err := requireCleanDatabaseFamily(sourcePath); err != nil {
		return fail(err)
	}
	if report != nil {
		report(Progress{Stage: ProgressValidation, Completed: reader.Total(), Total: reader.Total()})
	}
	if err := finalizeCandidate(ctx, candidatePath); err != nil {
		return fail(err)
	}
	if err := validateCandidate(ctx, candidatePath, sourcePath+".archives", expected); err != nil {
		return fail(err)
	}
	semanticSpans, err := candidateSpanCount(ctx, candidatePath)
	if err != nil {
		return fail(err)
	}
	compactInfo, err := os.Stat(candidatePath)
	if err != nil {
		return fail(err)
	}
	return Result{
		CandidatePath: candidatePath, Exports: expected.journalCount,
		SemanticSpans: semanticSpans, SourceBytes: sourceBytes,
		CompactBytes: compactInfo.Size(),
	}, nil
}

func copyRetentionCatalog(ctx context.Context, source *sql.Tx, candidatePath string) error {
	exists, err := tableExistsTx(ctx, source, "retained_exports")
	if err != nil || !exists {
		return err
	}
	destination, err := sql.Open("sqlite", candidatePath)
	if err != nil {
		return fmt.Errorf("open candidate retention catalog: %w", err)
	}
	defer destination.Close()
	if _, err := destination.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		return err
	}
	transaction, err := destination.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `DELETE FROM retention_policy`); err != nil {
		return err
	}
	if err := copyTableRows(ctx, source, transaction, "retention_policy", "INSERT"); err != nil {
		return err
	}
	rows, err := source.QueryContext(ctx, `SELECT id, received_at, state, hold_until, prior_segment_id, deleted_at,
 deletion_policy_revision, deletion_cutoff_days, deletion_evaluated_at, deletion_operation_id FROM retained_exports ORDER BY id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		values := make([]any, 10)
		pointers := make([]any, len(values))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			_ = rows.Close()
			return err
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO retained_exports (id, received_at, state, hold_until, prior_segment_id,
 deleted_at, deletion_policy_revision, deletion_cutoff_days, deletion_evaluated_at, deletion_operation_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET received_at=excluded.received_at, state=excluded.state, hold_until=excluded.hold_until,
 prior_segment_id=excluded.prior_segment_id, deleted_at=excluded.deleted_at,
 deletion_policy_revision=excluded.deletion_policy_revision, deletion_cutoff_days=excluded.deletion_cutoff_days,
 deletion_evaluated_at=excluded.deletion_evaluated_at, deletion_operation_id=excluded.deletion_operation_id`, values...); err != nil {
			_ = rows.Close()
			return err
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, table := range []string{"archive_segments", "archive_segment_members", "current_archive_memberships",
		"archive_segment_replacements", "retention_cycles", "retention_cycle_segments", "retention_operations", "retention_operation_exports", "retention_export_authorities"} {
		exists, err := tableExistsTx(ctx, source, table)
		if err != nil {
			return err
		}
		if exists {
			if err := copyTableRows(ctx, source, transaction, table, "INSERT"); err != nil {
				return fmt.Errorf("copy %s: %w", table, err)
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit candidate retention catalog: %w", err)
	}
	return nil
}

func copyTableRows(ctx context.Context, source *sql.Tx, destination *sql.Tx, table, verb string) error {
	columns, err := source.QueryContext(ctx, `SELECT name FROM pragma_table_info(?) ORDER BY cid`, table)
	if err != nil {
		return err
	}
	var names []string
	for columns.Next() {
		var name string
		if err := columns.Scan(&name); err != nil {
			_ = columns.Close()
			return err
		}
		names = append(names, name)
	}
	if err := columns.Close(); err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	quoted := make([]string, len(names))
	placeholders := make([]string, len(names))
	for index, name := range names {
		quoted[index] = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		placeholders[index] = "?"
	}
	rows, err := source.QueryContext(ctx, `SELECT `+strings.Join(quoted, ",")+` FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`)
	if err != nil {
		return err
	}
	defer rows.Close()
	statement := verb + ` INTO "` + strings.ReplaceAll(table, `"`, `""`) + `" (` + strings.Join(quoted, ",") + `) VALUES (` + strings.Join(placeholders, ",") + `)`
	for rows.Next() {
		values := make([]any, len(names))
		pointers := make([]any, len(names))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			return err
		}
		if _, err := destination.ExecContext(ctx, statement, values...); err != nil {
			return err
		}
	}
	return rows.Err()
}

func tableExistsTx(ctx context.Context, transaction *sql.Tx, table string) (bool, error) {
	var count int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		return false, err
	}
	return count == 1, nil
}

func candidateSpanCount(ctx context.Context, path string) (int64, error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return 0, fmt.Errorf("open candidate to count spans: %w", err)
	}
	defer database.Close()
	var count int64
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM spans`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count candidate spans: %w", err)
	}
	return count, nil
}

type replayItem struct {
	record   storedExport
	accepted ingest.AcceptedExport
}

type replayChunkSource struct {
	reader  *legacyReader
	pending *storedExport
}

func (source *replayChunkSource) Next(ctx context.Context) ([]storedExport, error) {
	records := make([]storedExport, 0, maxReplayBatchRecords)
	restoredBytes := 0
	for len(records) < maxReplayBatchRecords {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var record storedExport
		if source.pending != nil {
			record = *source.pending
			source.pending = nil
		} else {
			if !source.reader.Next() {
				break
			}
			var err error
			record, err = source.reader.Export()
			if err != nil {
				return nil, err
			}
		}
		recordBytes := record.Size
		if recordBytes < len(record.Stored) {
			recordBytes = len(record.Stored)
		}
		if len(records) > 0 && recordBytes > maxReplayBatchBytes-restoredBytes {
			source.pending = &record
			break
		}
		records = append(records, record)
		restoredBytes += recordBytes
	}
	return records, nil
}

func prepareReplayChunk(ctx context.Context, records []storedExport, profiles source.Registry) ([]replayItem, error) {
	items := make([]replayItem, len(records))
	errorsByIndex := make([]error, len(records))
	prepare := func(index int) {
		items[index], errorsByIndex[index] = prepareReplayItem(ctx, records[index], profiles)
	}
	workers := min(runtime.GOMAXPROCS(0), maxReplayWorkers, len(records))
	if workers <= 1 || !profiles.SupportsParallelProfiling() {
		for index := range records {
			prepare(index)
		}
	} else {
		indexes := make(chan int)
		var group sync.WaitGroup
		group.Add(workers)
		for range workers {
			go func() {
				defer group.Done()
				for index := range indexes {
					prepare(index)
				}
			}()
		}
		for index := range records {
			indexes <- index
		}
		close(indexes)
		group.Wait()
	}
	for index, err := range errorsByIndex {
		if err != nil {
			return nil, fmt.Errorf("prepare legacy export %d: %w", records[index].Ordinal, err)
		}
	}
	return items, nil
}

func prepareReplayItem(ctx context.Context, record storedExport, profiles source.Registry) (replayItem, error) {
	if err := ctx.Err(); err != nil {
		return replayItem{}, err
	}
	raw, err := journal.Restore(record.Codec, record.Stored, record.Size, record.Hash)
	if err != nil {
		return replayItem{}, fmt.Errorf("restore legacy export: %w", err)
	}
	accepted := ingest.AcceptedExport{
		Identity:           record.Identity,
		Envelope:           ingest.NewEnvelope(record.Signal, record.Transport, record.ReceivedAt, raw),
		Journal:            record.Metadata,
		NormalizationError: record.NormalizationError,
	}
	if record.Metadata.NormalizationStatus != "failed" {
		accepted, err = otel.ReplayExport(record.Signal, record.Transport, record.ReceivedAt, raw, profiles)
		if err != nil {
			return replayItem{}, err
		}
		accepted.Journal = record.Metadata
		accepted.Identity = record.Identity
	}
	return replayItem{record: record, accepted: accepted}, nil
}

func failBuild(destination *store.ReplayCandidate, reader *legacyReader, closeSource func() error, candidatePath string, cause error) (Result, error) {
	_ = destination.Close()
	_ = reader.Close()
	_ = closeSource()
	_ = removeTemporaryDatabaseFamily(candidatePath)
	return Result{}, cause
}

func configureMigrationSource(ctx context.Context, database *sql.DB) error {
	if _, err := database.ExecContext(ctx, "PRAGMA busy_timeout=5000"); err != nil {
		return fmt.Errorf("configure migration source: %w", err)
	}
	var busy, logFrames, checkpointed int
	if err := database.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
		return fmt.Errorf("checkpoint legacy database: %w", err)
	}
	if busy != 0 || logFrames != checkpointed {
		return fmt.Errorf("checkpoint legacy database remained busy (%d/%d frames)", checkpointed, logFrames)
	}
	if _, err := database.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return fmt.Errorf("protect legacy database: %w", err)
	}
	return nil
}

func finalizeCandidate(ctx context.Context, candidatePath string) error {
	database, err := sql.Open("sqlite", candidatePath)
	if err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", CurrentStorageGeneration)); err != nil {
		_ = database.Close()
		return fmt.Errorf("record data format version: %w", err)
	}
	var busy, logFrames, checkpointed int
	if err := database.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed); err != nil {
		_ = database.Close()
		return fmt.Errorf("checkpoint compact database: %w", err)
	}
	if busy != 0 || logFrames != checkpointed {
		_ = database.Close()
		return fmt.Errorf("compact database checkpoint remained busy (%d/%d frames)", checkpointed, logFrames)
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("close checkpointed compact database: %w", err)
	}
	return requireCleanDatabaseFamily(candidatePath)
}
