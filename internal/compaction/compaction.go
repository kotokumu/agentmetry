package compaction

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"runtime"
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
	if err := validateCandidate(ctx, candidatePath, expected); err != nil {
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
