package compaction

import (
	"context"
	"database/sql"
	"fmt"
	"os"

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

type Progress struct {
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
	reader, err := newLegacyReader(ctx, tx)
	if err != nil {
		_ = closeSource()
		return fail(err)
	}
	destination, err := store.Open(candidatePath, profiles)
	if err != nil {
		_ = reader.Close()
		_ = closeSource()
		return fail(fmt.Errorf("create Atlas-schema candidate: %w", err))
	}
	expected := validationExpectation{journals: make([]journalIdentity, 0, reader.Total())}
	for reader.Next() {
		record, err := reader.Export()
		if err != nil {
			return failBuild(destination, reader, closeSource, candidatePath, err)
		}
		raw, err := journal.Restore(record.Codec, record.Stored, record.Size, record.Hash)
		if err != nil {
			return failBuild(destination, reader, closeSource, candidatePath, fmt.Errorf("restore legacy export %d: %w", record.Ordinal, err))
		}
		accepted := ingest.AcceptedExport{
			Envelope:           ingest.NewEnvelope(record.Signal, record.Transport, record.ReceivedAt, raw),
			Journal:            record.Metadata,
			NormalizationError: record.NormalizationError,
		}
		if record.Metadata.NormalizationStatus != "failed" {
			accepted, err = otel.ReplayExport(record.Signal, record.Transport, record.ReceivedAt, raw, profiles)
			if err != nil {
				return failBuild(destination, reader, closeSource, candidatePath, err)
			}
			accepted.Journal = record.Metadata
		}
		if err := destination.CommitExport(ctx, accepted); err != nil {
			return failBuild(destination, reader, closeSource, candidatePath, fmt.Errorf("write compact export %d: %w", record.Ordinal, err))
		}
		expected.add(record, accepted)
		if report != nil {
			report(Progress{Completed: record.Ordinal, Total: reader.Total()})
		}
	}
	if err := reader.Close(); err != nil {
		_ = destination.Close()
		_ = closeSource()
		return fail(err)
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
	if err := finalizeCandidate(ctx, candidatePath); err != nil {
		return fail(err)
	}
	if err := validateCandidate(ctx, candidatePath, expected); err != nil {
		return fail(err)
	}
	compactInfo, err := os.Stat(candidatePath)
	if err != nil {
		return fail(err)
	}
	return Result{
		CandidatePath: candidatePath, Exports: int64(len(expected.journals)),
		SemanticSpans: int64(len(expected.spanKeys)), SourceBytes: sourceBytes,
		CompactBytes: compactInfo.Size(),
	}, nil
}

func failBuild(destination *store.Store, reader *legacyReader, closeSource func() error, candidatePath string, cause error) (Result, error) {
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
