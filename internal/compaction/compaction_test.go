package compaction

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/archivefs"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/harness"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/journal"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/retention"
	"github.com/kotokumu/agentmetry/internal/source/builtin"
	claudesource "github.com/kotokumu/agentmetry/internal/source/claude"
	codexsource "github.com/kotokumu/agentmetry/internal/source/codex"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
	sourceplugin "github.com/kotokumu/agentmetry/sourceplugin"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	_ "modernc.org/sqlite"
)

type profilingProbePlugin struct {
	parallel bool
	active   atomic.Int64
	maximum  atomic.Int64
}

type sequentialPlugin struct{ sourceplugin.Plugin }

type replayOverlapProbePlugin struct {
	calls              atomic.Int64
	firstStarted       chan struct{}
	releaseFirst       chan struct{}
	secondBatchStarted chan struct{}
}

func TestForcedCompactionPreservesArchivedLifecycleCatalog(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lifecycle.db")
	database, err := store.Open(path, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-10*24*time.Hour), []byte{0x0a, 0x00}),
		Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture failure"}
	if err := database.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(database, archivefs.New(database.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := service.Segments(ctx, "", 100)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	segmentID := segments[0].ID
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, path, builtin.Registry(), nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	policy, _, err := reopened.RetentionPolicy(ctx)
	if err != nil || !policy.Enabled || policy.Revision == 0 {
		t.Fatalf("policy=%v err=%v", policy, err)
	}
	segments, _, err = reopened.ListArchiveSegments(ctx, "", 100)
	if err != nil || len(segments) != 1 || segments[0].ID != segmentID {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	if _, err := archivefs.New(reopened.ArchiveDirectory()).OpenVerified(ctx, segmentID); err != nil {
		t.Fatal(err)
	}
}

func TestCompactionRecoversStagedDeletionBeforeSnapshot(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "staged-deletion.db")
	database, err := store.Open(path, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-10*retention.Day), []byte{0x0a, 0x00}),
		Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
	if err := database.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := database.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(database, archivefs.New(database.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := service.Segments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	segmentID := segments[0].ID
	archiveDirectory := database.ArchiveDirectory()
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.Exec(`INSERT INTO retention_operations (id, kind, status, phase, requested_at, evaluated_at, staging_token, decision_policy_revision, decision_cutoff_days, affected_export_count, affected_segment_count)
VALUES ('compaction-staged-delete', 'delete', 'running', 'staged', ?, ?, ?, 1, 30, 1, 1)`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), segmentID); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.Exec(`INSERT INTO retention_operation_exports (operation_id, export_id, role) VALUES ('compaction-staged-delete', 1, 'affected')`); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.Exec(`INSERT INTO retention_export_authorities (export_id, operation_id, kind, role) VALUES (1, 'compaction-staged-delete', 'delete', 'affected')`); err != nil {
		t.Fatal(err)
	}
	if err := rawDB.Close(); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(archiveDirectory, segmentID+".tar.zst")
	if err := os.Rename(installed, installed+".deleting"); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, path, builtin.Registry(), nil); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := archivefs.New(reopened.ArchiveDirectory()).OpenVerified(ctx, segmentID); err != nil {
		t.Fatalf("staged archive was not restored before compaction: %v", err)
	}
	operations, err := reopened.RetentionOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) == 0 || operations[0].ID != "compaction-staged-delete" || operations[0].Status != retention.OperationCancelled {
		t.Fatalf("recovered operations=%v", operations)
	}
}

func (*profilingProbePlugin) ID() string { return "probe" }

func (*profilingProbePlugin) Match(sourceplugin.Event) bool { return true }

func (plugin *profilingProbePlugin) Normalize(event sourceplugin.Event) sourceplugin.Event {
	active := plugin.active.Add(1)
	for {
		maximum := plugin.maximum.Load()
		if active <= maximum || plugin.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	time.Sleep(5 * time.Millisecond)
	plugin.active.Add(-1)
	return event
}

func (plugin *profilingProbePlugin) SupportsParallelProfiling() bool { return plugin.parallel }

func (*replayOverlapProbePlugin) ID() string { return "replay-overlap-probe" }

func (*replayOverlapProbePlugin) Match(sourceplugin.Event) bool { return true }

func (plugin *replayOverlapProbePlugin) Normalize(event sourceplugin.Event) sourceplugin.Event {
	call := plugin.calls.Add(1)
	if call == 1 {
		close(plugin.firstStarted)
		<-plugin.releaseFirst
	}
	if call == maxReplayBatchRecords+1 {
		close(plugin.secondBatchStarted)
	}
	return event
}

func (*replayOverlapProbePlugin) SupportsParallelProfiling() bool { return false }

func TestMigrateRebuildsTrueLegacySchemaAndPreservesJournalMetadata(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "legacy.db")
	raw := semanticTracePayload(t, 300)
	createLegacyDatabase(t, sourcePath, []legacyFixture{
		{raw: raw, source: "codex", version: 3, status: "projected"},
		{raw: []byte{0x0a, 0x00}, source: "legacy-source", version: 7, status: "failed", normalizationError: "unsupported source revision"},
	})

	var progress []Progress
	result, err := MigrateIfNeeded(context.Background(), sourcePath, builtin.Registry(), func(update Progress) {
		progress = append(progress, update)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Migrated || result.Exports != 2 || result.SemanticSpans != 1 {
		t.Fatalf("unexpected migration result: %#v", result)
	}
	if result.CompactBytes >= result.SourceBytes*3/10 {
		t.Fatalf("compact database %d bytes is not below 30%% of legacy %d bytes", result.CompactBytes, result.SourceBytes)
	}
	wantStages := []ProgressStage{ProgressReplay, ProgressReplay, ProgressProjection, ProgressValidation, ProgressReplacement}
	if len(progress) != len(wantStages) {
		t.Fatalf("migration progress = %#v", progress)
	}
	for index, want := range wantStages {
		if progress[index].Stage != want {
			t.Fatalf("progress %d stage = %q, want %q", index, progress[index].Stage, want)
		}
	}
	if progress[1].Completed != 2 || progress[1].Total != 2 {
		t.Fatalf("committed replay progress = %#v", progress[1])
	}
	if fileExists(sourcePath+".compacting") || fileExists(manifestPath(sourcePath)) {
		t.Fatal("migration artifacts remain after install")
	}

	database, err := sql.Open("sqlite", sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rows, err := database.Query(`SELECT source, normalizer_version, normalization_status,
normalization_error, payload_codec FROM otlp_exports ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wants := []struct {
		source, status, normalizationError string
		version                            int
	}{{"codex", "projected", "", 3}, {"legacy-source", "failed", "unsupported source revision", 7}}
	for index, want := range wants {
		if !rows.Next() {
			t.Fatalf("missing journal row %d", index+1)
		}
		var sourceID, status, normalizationError, codec string
		var version int
		if err := rows.Scan(&sourceID, &version, &status, &normalizationError, &codec); err != nil {
			t.Fatal(err)
		}
		if sourceID != want.source || version != want.version || status != want.status || normalizationError != want.normalizationError {
			t.Fatalf("row %d metadata = %q/%d/%q/%q", index+1, sourceID, version, status, normalizationError)
		}
		if index == 0 && codec != "zstd" {
			t.Fatalf("compressible payload codec = %q", codec)
		}
	}
	var spans, plans, version int
	if err := database.QueryRow("SELECT COUNT(*) FROM spans").Scan(&spans); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT COUNT(*) FROM plan_usage_snapshots").Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if spans != 1 || plans != 1 || version != CurrentStorageGeneration {
		t.Fatalf("spans=%d plans=%d format=%d", spans, plans, version)
	}
}

func TestPrepareReplayChunkUsesParallelProfilingOnlyWhenRegistryAllowsIt(t *testing.T) {
	raw := semanticTracePayload(t, 0)
	hash := sha256.Sum256(raw)
	records := make([]storedExport, 8)
	for index := range records {
		records[index] = storedExport{
			Ordinal: int64(index + 1), ReceivedAt: time.Date(2026, 9, 9, 12, 0, index, 0, time.UTC),
			Signal: canonical.SignalTrace, Transport: ingest.TransportGRPC,
			Stored: raw, Codec: journal.CodecIdentity, Size: len(raw), Hash: hash,
			Metadata: ingest.JournalMetadata{Source: "probe", NormalizerVersion: 1, NormalizationStatus: "projected"},
		}
	}
	for _, test := range []struct {
		name            string
		parallel        bool
		wantConcurrency bool
	}{
		{name: "explicitly parallel-safe", parallel: true, wantConcurrency: true},
		{name: "sequential fallback", parallel: false, wantConcurrency: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			plugin := &profilingProbePlugin{parallel: test.parallel}
			items, err := prepareReplayChunk(context.Background(), records, sourceplugin.NewRegistry(plugin))
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != len(records) {
				t.Fatalf("prepared items = %d, want %d", len(items), len(records))
			}
			for index, item := range items {
				if item.record.Ordinal != int64(index+1) {
					t.Fatalf("item %d ordinal = %d", index, item.record.Ordinal)
				}
			}
			gotConcurrency := plugin.maximum.Load() > 1
			if gotConcurrency != test.wantConcurrency {
				t.Fatalf("parallel profiling = %v (maximum=%d), want %v", gotConcurrency, plugin.maximum.Load(), test.wantConcurrency)
			}
		})
	}
}

func TestPrepareReplayChunkPreservesFailedJournalWithoutProfilingIt(t *testing.T) {
	raw := []byte{0x0a, 0x00}
	plugin := &profilingProbePlugin{parallel: true}
	items, err := prepareReplayChunk(context.Background(), []storedExport{{
		Ordinal: 1, ReceivedAt: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
		Signal: canonical.SignalLog, Transport: ingest.TransportGRPC,
		Stored: raw, Codec: journal.CodecIdentity, Size: len(raw), Hash: sha256.Sum256(raw),
		Metadata:           ingest.JournalMetadata{Source: "legacy", NormalizerVersion: 1, NormalizationStatus: "failed"},
		NormalizationError: "unsupported source revision",
	}}, sourceplugin.NewRegistry(plugin))
	if err != nil {
		t.Fatal(err)
	}
	if plugin.maximum.Load() != 0 {
		t.Fatal("failed journal record was profiled")
	}
	if len(items) != 1 || items[0].accepted.NormalizationError != "unsupported source revision" || items[0].accepted.Journal.NormalizationStatus != "failed" {
		t.Fatalf("failed replay item = %#v", items)
	}
}

func TestMigratePreparesTheNextSourceBatchWhileTheCurrentBatchCommitIsBlocked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overlap.db")
	raw := semanticTracePayload(t, 0)
	fixtures := make([]legacyFixture, maxReplayBatchRecords+1)
	for index := range fixtures {
		fixtures[index] = legacyFixture{
			raw: raw, source: fmt.Sprintf("source-%03d", index+1), version: 1, status: "projected",
		}
	}
	createLegacyDatabaseWithPayloadJSON(t, path, fixtures, "")
	plugin := &replayOverlapProbePlugin{
		firstStarted:       make(chan struct{}),
		releaseFirst:       make(chan struct{}),
		secondBatchStarted: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		_, err := MigrateIfNeeded(context.Background(), path, sourceplugin.NewRegistry(plugin), nil)
		done <- err
	}()
	waitForSignal(t, plugin.firstStarted, "first source batch normalization")

	candidate, err := sql.Open("sqlite", path+".compacting")
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := candidate.Begin()
	if err != nil {
		candidate.Close()
		t.Fatal(err)
	}
	if _, err := transaction.Exec("UPDATE otlp_exports SET source=source WHERE id=-1"); err != nil {
		transaction.Rollback()
		candidate.Close()
		t.Fatal(err)
	}
	close(plugin.releaseFirst)
	waitForSignal(t, plugin.secondBatchStarted, "next source batch normalization")
	if err := transaction.Rollback(); err != nil {
		candidate.Close()
		t.Fatal(err)
	}
	if err := candidate.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("migration did not finish after releasing the candidate writer")
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	rows, err := database.Query("SELECT source FROM otlp_exports ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for index := range fixtures {
		if !rows.Next() {
			t.Fatalf("missing migrated journal row %d", index+1)
		}
		var got string
		if err := rows.Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != fixtures[index].source {
			t.Fatalf("migrated journal row %d source = %q, want %q", index+1, got, fixtures[index].source)
		}
	}
	if rows.Next() {
		t.Fatal("migration duplicated a source journal row")
	}
}

func TestMigratePreparationFailureStopsThePipelineAndRemovesCandidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	createLegacyDatabaseWithPayloadJSON(t, path, []legacyFixture{{
		raw: semanticTracePayload(t, 0), source: "codex", version: 1, status: "projected",
	}}, "")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("UPDATE otlp_exports SET payload_sha256=?", strings.Repeat("0", sha256.Size*2)); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil); err == nil || !strings.Contains(err.Error(), "restore legacy export") {
		t.Fatalf("migration error = %v, want restore failure", err)
	}
	if fileExists(path+".compacting") || fileExists(manifestPath(path)) {
		t.Fatal("failed replay pipeline left migration artifacts")
	}
	verify, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer verify.Close()
	var hash string
	if err := verify.QueryRow("SELECT payload_sha256 FROM otlp_exports").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if hash != strings.Repeat("0", sha256.Size*2) {
		t.Fatalf("authoritative source journal changed after failure: hash=%q", hash)
	}
}

func BenchmarkPrepareReplayChunk(b *testing.B) {
	raw := semanticTracePayload(b, 100)
	hash := sha256.Sum256(raw)
	records := make([]storedExport, 256)
	for index := range records {
		records[index] = storedExport{
			Ordinal: int64(index + 1), ReceivedAt: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
			Signal: canonical.SignalTrace, Transport: ingest.TransportGRPC,
			Stored: raw, Codec: journal.CodecIdentity, Size: len(raw), Hash: hash,
			Metadata: ingest.JournalMetadata{Source: "codex", NormalizerVersion: 1, NormalizationStatus: "projected"},
		}
	}
	parallel := sourceplugin.NewRegistry(codexsource.New(), claudesource.New())
	sequential := sourceplugin.NewRegistry(sequentialPlugin{codexsource.New()}, sequentialPlugin{claudesource.New()})
	for _, test := range []struct {
		name     string
		registry sourceplugin.Registry
	}{{"sequential", sequential}, {"parallel", parallel}} {
		b.Run(test.name, func(b *testing.B) {
			for range b.N {
				if _, err := prepareReplayChunk(context.Background(), records, test.registry); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestMigrateIfNeededSupportsFreshCurrentAndNewerDatabases(t *testing.T) {
	t.Run("fresh install has no historical migration", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "new.db")
		result, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil)
		if err != nil || result.Migrated || fileExists(path) {
			t.Fatalf("result=%#v exists=%v err=%v", result, fileExists(path), err)
		}
	})
	t.Run("empty placeholder is initialized as a fresh install", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.db")
		writeBytes(t, path, nil)
		result, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil)
		if err != nil || result.Migrated {
			t.Fatalf("result=%#v err=%v", result, err)
		}
		database, err := store.Open(path, builtin.Registry())
		if err != nil {
			t.Fatal(err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
		if err := validateInstalledDatabase(path, CurrentStorageGeneration); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("current database is a no-op", func(t *testing.T) {
		path := createCurrentDatabase(t)
		database, err := store.Open(path, builtin.Registry())
		if err != nil {
			t.Fatal(err)
		}
		raw := semanticTracePayload(t, 0)
		accepted, err := otel.ReplayExport(canonical.SignalTrace, ingest.TransportGRPC, time.Now(), raw, builtin.Registry())
		if err != nil {
			t.Fatal(err)
		}
		if err := database.CommitExport(context.Background(), accepted); err != nil {
			t.Fatal(err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
		result, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil)
		if err != nil || result.Migrated {
			t.Fatalf("result=%#v err=%v", result, err)
		}
		verifyDB, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer verifyDB.Close()
		var exports int
		if err := verifyDB.QueryRow("SELECT COUNT(*) FROM otlp_exports").Scan(&exports); err != nil || exports != 1 {
			t.Fatalf("current journal exports=%d err=%v", exports, err)
		}
	})
	t.Run("downgrade is rejected", func(t *testing.T) {
		path := createCurrentDatabase(t)
		database, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = database.Exec("PRAGMA user_version=99")
		_ = database.Close()
		if err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil); err == nil {
			t.Fatal("newer database was accepted by older migrator")
		}
		if _, err := Migrate(context.Background(), path, builtin.Registry(), nil); err == nil {
			t.Fatal("forced migration accepted a newer database")
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("newer authoritative database changed: err=%v", err)
		}
	})
}

func TestGenerationFourMigrationReattributesClaudeUsageFromTheJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude-generation-four.db")
	database, err := store.Open(path, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := plog.NewLogs()
	logResource := logs.ResourceLogs().AppendEmpty()
	logResource.Resource().Attributes().PutStr("service.name", "claude-code")
	logResource.Resource().Attributes().PutStr("session.id", "claude-migration")
	request := logResource.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	request.SetTimestamp(pcommon.NewTimestampFromTime(now))
	request.SetEventName("claude_code.api_request")
	request.Attributes().PutStr("client_request_id", "migration-request")
	request.Attributes().PutStr("agent.name", "Explore")
	request.Attributes().PutInt("input_tokens", 8)
	request.Attributes().PutInt("output_tokens", 2)
	request.Attributes().PutInt("cost_usd_micros", 10000)
	logPayload, err := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	acceptedLog, err := otel.ReplayExport(canonical.SignalLog, ingest.TransportGRPC, now, logPayload, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CommitExport(context.Background(), acceptedLog); err != nil {
		t.Fatal(err)
	}

	traces := ptrace.NewTraces()
	traceResource := traces.ResourceSpans().AppendEmpty()
	traceResource.Resource().Attributes().PutStr("service.name", "claude-code")
	traceResource.Resource().Attributes().PutStr("session.id", "claude-migration")
	span := traceResource.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName("claude_code.llm_request")
	span.SetTraceID(pcommon.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	span.SetSpanID(pcommon.SpanID{1, 2, 3, 4, 5, 6, 7, 8})
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(now))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(time.Second)))
	span.Attributes().PutStr("client_request_id", "migration-request")
	span.Attributes().PutStr("agent_id", "runtime-migration-agent")
	span.Attributes().PutStr("parent_agent_id", "main")
	span.Attributes().PutStr("agent.name", "Explore")
	span.Attributes().PutInt("cost_usd_micros", 10000)
	tracePayload, err := ptraceotlp.NewExportRequestFromTraces(traces).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	acceptedTrace, err := otel.ReplayExport(canonical.SignalTrace, ingest.TransportGRPC, now, tracePayload, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CommitExport(context.Background(), acceptedTrace); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`UPDATE logs SET agent_id = 'Explore'; PRAGMA user_version=4`); err != nil {
		_ = legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Migrated {
		t.Fatal("generation-four Claude journal was not replayed")
	}
	migrated, err := store.Open(path, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = migrated.Close() })
	identity, _ := query.NewConversationIdentity("claude", "claude-migration")
	summary, err := migrated.GetSessionSummary(context.Background(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Agents) != 1 || summary.Agents[0].AgentID != "runtime-migration-agent" || summary.Agents[0].Tokens.Total() != 10 {
		t.Fatalf("migrated Claude attribution = %#v", summary.Agents)
	}
	if summary.CostUSD == nil || *summary.CostUSD != 0.01 {
		t.Fatalf("migrated Claude cost = %v, want one authoritative contribution", summary.CostUSD)
	}
}

func TestReleaseAndAggregationGenerationsRebuildCodexSessionMemberships(t *testing.T) {
	for _, generation := range []int{2, 3, 4} {
		t.Run(fmt.Sprintf("generation-%d", generation), func(t *testing.T) {
			testGenerationRebuildsCodexSessionMemberships(t, generation)
		})
	}
}

func testGenerationRebuildsCodexSessionMemberships(t *testing.T, generation int) {
	path := createCurrentDatabase(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	logs := plog.NewLogs()
	records := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords()
	parent := records.AppendEmpty()
	parent.SetEventName("codex.sse_event")
	parent.SetTimestamp(pcommon.NewTimestampFromTime(now))
	parent.Attributes().PutStr("event.kind", "response.completed")
	parent.Attributes().PutStr("conversation.id", "parent")
	spawn := records.AppendEmpty()
	spawn.SetEventName("codex.agent_communication")
	spawn.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(time.Second)))
	spawn.Attributes().PutStr("kind", "spawn")
	spawn.Attributes().PutStr("state", "send")
	spawn.Attributes().PutStr("sender_thread_id", "parent")
	spawn.Attributes().PutStr("receiver_thread_id", "child")
	child := records.AppendEmpty()
	child.SetEventName("codex.sse_event")
	child.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(2 * time.Second)))
	child.Attributes().PutStr("event.kind", "response.completed")
	child.Attributes().PutStr("conversation.id", "child")
	raw, err := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := otel.ReplayExport(canonical.SignalLog, ingest.TransportGRPC, now, raw, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(path, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.CommitExport(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(fmt.Sprintf(`DELETE FROM session_memberships; DELETE FROM session_links; PRAGMA user_version=%d`, generation)); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil)
	if err != nil || !result.Migrated {
		t.Fatalf("migration result=%#v err=%v", result, err)
	}
	upgraded, err := store.Open(path, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	page, err := query.NewPage(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := upgraded.ListSessions(context.Background(), query.SessionListFilter{Since: now.Add(-time.Hour), Page: page})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions.Sessions) != 1 || sessions.Sessions[0].ID != "parent" || sessions.Sessions[0].AgentCount != 2 {
		t.Fatalf("migrated sessions = %#v", sessions.Sessions)
	}
	identity, err := query.NewConversationIdentity("codex", "child")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := upgraded.GetSessionSummary(context.Background(), identity)
	if err != nil || summary.ID != "parent" || summary.AgentCount != 2 {
		t.Fatalf("migrated summary=%#v err=%v", summary, err)
	}
}

func TestMigrateIfNeededRebuildsAnOlderStorageGenerationDirectlyIntoCurrent(t *testing.T) {
	path := createCurrentDatabase(t)
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec("PRAGMA user_version=1")
	_ = database.Close()
	if err != nil {
		t.Fatal(err)
	}
	result, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil)
	if err != nil || !result.Migrated {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if err := validateInstalledDatabase(path, CurrentStorageGeneration); err != nil {
		t.Fatal(err)
	}
}

func TestPublishedReleaseCohortsUpgradeDirectlyIntoCurrent(t *testing.T) {
	// v1.0.0 through v1.2.0 all used the pre-compression journal family. Test
	// every published release line as a supported direct-upgrade cohort.
	for _, releaseLine := range []string{"v1.0.0-v1.0.2", "v1.1.0-v1.1.2", "v1.2.0"} {
		t.Run(releaseLine, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "published.db")
			raw := semanticTracePayload(t, 2)
			wantHash := sha256.Sum256(raw)
			createLegacyDatabase(t, path, []legacyFixture{{
				raw: raw, source: "codex", version: 1, status: "projected",
			}})

			result, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil)
			if err != nil || !result.Migrated || result.Exports != 1 {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			database, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			var gotHash string
			if err := database.QueryRow("SELECT payload_sha256 FROM otlp_exports").Scan(&gotHash); err != nil {
				t.Fatal(err)
			}
			if gotHash != hex.EncodeToString(wantHash[:]) {
				t.Fatalf("journal hash=%q want=%q", gotHash, hex.EncodeToString(wantHash[:]))
			}
			if err := validateInstalledDatabase(path, CurrentStorageGeneration); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMigrateIfNeededRebuildsCurrentJournalWhenAtlasProjectionDiffIsUnsafe(t *testing.T) {
	for _, tt := range []struct{ name, ddl string }{
		{name: "extra column", ddl: "ALTER TABLE observations ADD COLUMN obsolete_projection_json TEXT NOT NULL DEFAULT ''"},
		{name: "extra index uses full startup rebuild path", ddl: "CREATE INDEX future_codex_name_idx ON logs(id) WHERE source='codex' AND tool_name='list_threads'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := createCurrentDatabase(t)
			database, err := store.Open(path, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			raw := semanticTracePayload(t, 3)
			accepted, err := otel.ReplayExport(canonical.SignalTrace, ingest.TransportGRPC, time.Now(), raw, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			accepted.Journal.Harness = harness.ReceiptEvidence{
				State: harness.ReceiptReported, Scope: "project-7f2a",
				Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", Label: "AGENTS v2",
			}
			if err := database.CommitExport(context.Background(), accepted); err != nil {
				t.Fatal(err)
			}
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
			sqlDB, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			var wantHash string
			if err := sqlDB.QueryRow("SELECT payload_sha256 FROM otlp_exports").Scan(&wantHash); err != nil {
				t.Fatal(err)
			}
			if _, err := sqlDB.Exec(tt.ddl); err != nil {
				t.Fatal(err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatal(err)
			}

			result, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil)
			if err != nil || !result.Migrated {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			verifyDB, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			defer verifyDB.Close()
			var gotHash, harnessState, harnessScope, harnessFingerprint, harnessLabel string
			if err := verifyDB.QueryRow(`SELECT payload_sha256, harness_receipt_state, harness_scope,
harness_fingerprint, harness_label FROM otlp_exports`).Scan(&gotHash, &harnessState, &harnessScope, &harnessFingerprint, &harnessLabel); err != nil || gotHash != wantHash {
				t.Fatalf("journal hash=%q want=%q err=%v", gotHash, wantHash, err)
			}
			if harnessState != "reported" || harnessScope != "project-7f2a" || harnessFingerprint != accepted.Journal.Harness.Fingerprint || harnessLabel != "AGENTS v2" {
				t.Fatalf("harness receipt was not preserved: %q/%q/%q/%q", harnessState, harnessScope, harnessFingerprint, harnessLabel)
			}
			hasObsolete, err := columnExists(context.Background(), verifyDB, "observations", "obsolete_projection_json")
			if err != nil || hasObsolete {
				t.Fatalf("obsolete projection schema remains: exists=%v err=%v", hasObsolete, err)
			}
			var extraIndexes int
			if err := verifyDB.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='future_codex_name_idx'").Scan(&extraIndexes); err != nil || extraIndexes != 0 {
				t.Fatalf("extra indexes=%d err=%v", extraIndexes, err)
			}
			if err := validateInstalledDatabase(path, CurrentStorageGeneration); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMigrateIfNeededFailsClosedWhenLegacyDatabaseHasNoLosslessJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "projection-only.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec("CREATE TABLE spans (trace_id TEXT, span_id TEXT)")
	_ = database.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil); err == nil {
		t.Fatal("projection-only database was treated as losslessly rebuildable")
	}
	verifyDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer verifyDB.Close()
	var journalExists int
	if err := verifyDB.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE name='otlp_exports')").Scan(&journalExists); err != nil || journalExists != 0 {
		t.Fatalf("projection-only database was mutated: exists=%d err=%v", journalExists, err)
	}
}

func TestMigrateIfNeededFailsClosedWhenCurrentDatabaseHasNoLosslessJournal(t *testing.T) {
	path := createCurrentDatabase(t)
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("DROP TABLE otlp_exports"); err != nil {
		database.Close()
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateIfNeeded(context.Background(), path, builtin.Registry(), nil); err == nil {
		t.Fatal("current projection-only database was treated as losslessly rebuildable")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("current projection-only database was modified")
	}
	verifyDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer verifyDB.Close()
	journalExists, err := tableExists(context.Background(), verifyDB, "otlp_exports")
	if err != nil || journalExists {
		t.Fatalf("missing journal was silently recreated: exists=%v err=%v", journalExists, err)
	}
}

func TestMigrateRefusesDatabaseOwnedByRunningStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.db")
	database, err := store.Open(path, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Migrate(ctx, path, builtin.Registry(), nil); err == nil {
		t.Fatal("migration acquired a live database")
	}
	if !fileExists(path) {
		t.Fatal("live database disappeared after refused migration")
	}
}

func TestMigrateRefusesLegacyWriterThatPredatesOwnershipLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-live.db")
	createLegacyDatabase(t, path, []legacyFixture{{raw: semanticTracePayload(t, 1), source: "codex", version: 1, status: "projected"}})
	legacyWriter, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer legacyWriter.Close()
	transaction, err := legacyWriter.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	if _, err := transaction.Exec("UPDATE otlp_exports SET source='writer-active' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(context.Background(), path, builtin.Registry(), nil); err == nil {
		t.Fatal("migration replaced a database held by a pre-lock writer")
	}
	if fileExists(path+".compacting") || fileExists(manifestPath(path)) {
		t.Fatal("failed live-writer migration left replacement artifacts")
	}
	var sourceID string
	if err := transaction.QueryRow("SELECT source FROM otlp_exports WHERE id=1").Scan(&sourceID); err != nil || sourceID != "writer-active" {
		t.Fatalf("legacy writer transaction was disturbed: source=%q err=%v", sourceID, err)
	}
}

func TestCancelledMigrationKeepsLegacyJournalAuthoritative(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancelled.db")
	fixtures := make([]legacyFixture, 5)
	for index := range fixtures {
		fixtures[index] = legacyFixture{raw: semanticTracePayload(t, index+1), source: "codex", version: 1, status: "projected"}
	}
	createLegacyDatabase(t, path, fixtures)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := MigrateIfNeeded(ctx, path, builtin.Registry(), func(progress Progress) {
		if progress.Completed > 0 {
			cancel()
		}
	})
	if err == nil {
		t.Fatal("cancelled migration succeeded")
	}
	if fileExists(path+".compacting") || fileExists(manifestPath(path)) {
		t.Fatal("cancelled migration left replacement artifacts")
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var exports int
	if err := database.QueryRow("SELECT COUNT(*) FROM otlp_exports").Scan(&exports); err != nil || exports != len(fixtures) {
		t.Fatalf("legacy exports=%d err=%v", exports, err)
	}
	hasCodec, err := columnExists(context.Background(), database, "otlp_exports", "payload_codec")
	if err != nil || hasCodec {
		t.Fatalf("legacy schema changed: codec=%v err=%v", hasCodec, err)
	}
}

func TestRecoverAlwaysLeavesOneAuthoritativeDatabase(t *testing.T) {
	tests := []struct {
		name  string
		phase migrationPhase
	}{
		{name: "crash after preserving source but before manifest advance", phase: phaseValidated},
		{name: "crash before candidate install", phase: phaseSourcePreserved},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "agentmetry.db")
			manifest := migrationManifest{FormatVersion: manifestFormatVersion, TargetGeneration: CurrentStorageGeneration, Phase: test.phase, Source: source, Candidate: source + ".compacting", Backup: source + ".pre-compaction"}
			writeBytes(t, manifest.Backup, []byte("legacy"))
			writeBytes(t, manifest.Candidate, []byte("candidate"))
			if err := writeManifest(manifest); err != nil {
				t.Fatal(err)
			}
			if err := recoverOwned(source); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, []byte("legacy")) {
				t.Fatalf("authoritative bytes = %q", got)
			}
			if fileExists(manifestPath(source)) || fileExists(manifest.Backup) {
				t.Fatal("recovery artifacts remain")
			}
		})
	}
}

func TestRecoverCompletesInstalledCandidateAndRemovesLegacyBackup(t *testing.T) {
	for _, phase := range []migrationPhase{phaseInstalled, phaseVerified} {
		t.Run(string(phase), func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "agentmetry.db")
			createCurrentDatabaseAt(t, source)
			backup := source + ".pre-compaction"
			writeBytes(t, backup, []byte("legacy"))
			manifest := migrationManifest{
				FormatVersion: manifestFormatVersion, TargetGeneration: CurrentStorageGeneration, Phase: phase,
				Source: source, Candidate: source + ".compacting", Backup: backup,
			}
			if err := writeManifest(manifest); err != nil {
				t.Fatal(err)
			}
			if err := recoverOwned(source); err != nil {
				t.Fatal(err)
			}
			if fileExists(backup) || fileExists(manifestPath(source)) {
				t.Fatal("verified installed migration was not finalized")
			}
			if err := validateInstalledDatabase(source, CurrentStorageGeneration); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNewBinaryRecoversOlderManifestThenUpgradesItsStorageGeneration(t *testing.T) {
	source := filepath.Join(t.TempDir(), "agentmetry.db")
	createCurrentDatabaseAt(t, source)
	database, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec("PRAGMA user_version=1")
	_ = database.Close()
	if err != nil {
		t.Fatal(err)
	}
	manifest := migrationManifest{
		FormatVersion: manifestFormatVersion, TargetGeneration: 1, Phase: phaseInstalled,
		Source: source, Candidate: source + ".compacting", Backup: source + ".pre-compaction",
	}
	writeBytes(t, manifest.Backup, []byte("older-authoritative-backup"))
	if err := writeManifest(manifest); err != nil {
		t.Fatal(err)
	}
	result, err := MigrateIfNeeded(context.Background(), source, builtin.Registry(), nil)
	if err != nil || !result.Migrated {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if err := validateInstalledDatabase(source, CurrentStorageGeneration); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverRestoresValidBackupWhenInstalledCandidateIsInvalid(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "agentmetry.db")
	backup := source + ".pre-compaction"
	createCurrentDatabaseAt(t, backup)
	database, err := sql.Open("sqlite", backup)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec("PRAGMA user_version=1")
	_ = database.Close()
	if err != nil {
		t.Fatal(err)
	}
	writeBytes(t, source, []byte("invalid-installed-candidate"))
	manifest := migrationManifest{
		FormatVersion: manifestFormatVersion, TargetGeneration: 1, Phase: phaseInstalled,
		Source: source, Candidate: source + ".compacting", Backup: backup,
	}
	if err := writeManifest(manifest); err != nil {
		t.Fatal(err)
	}
	if err := recoverOwned(source); err == nil {
		t.Fatal("invalid installed candidate did not report validation failure")
	}
	if err := validateInstalledDatabase(source, 1); err != nil {
		t.Fatalf("legacy backup was not restored: %v", err)
	}
	if fileExists(manifestPath(source)) || fileExists(backup) {
		t.Fatal("rollback artifacts remain")
	}
}

type legacyFixture struct {
	raw                []byte
	source             string
	version            int
	status             string
	normalizationError string
}

func createLegacyDatabase(t *testing.T, path string, fixtures []legacyFixture) {
	createLegacyDatabaseWithPayloadJSON(t, path, fixtures, strings.Repeat("legacy-json", 500000))
}

func createLegacyDatabaseWithPayloadJSON(t *testing.T, path string, fixtures []legacyFixture, payloadJSON string) {
	t.Helper()
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	_, err = database.Exec(`
PRAGMA journal_mode=WAL;
CREATE TABLE otlp_exports (
 id INTEGER PRIMARY KEY AUTOINCREMENT, received_at TEXT NOT NULL, signal TEXT NOT NULL,
 transport TEXT NOT NULL, payload_protobuf BLOB NOT NULL, payload_json TEXT NOT NULL,
 payload_sha256 TEXT NOT NULL, payload_size INTEGER NOT NULL, source TEXT NOT NULL,
 normalizer_version INTEGER NOT NULL, normalization_status TEXT NOT NULL,
 normalization_error TEXT NOT NULL
);
CREATE TABLE observations (
 id INTEGER PRIMARY KEY AUTOINCREMENT, payload_json TEXT NOT NULL, attributes_json TEXT NOT NULL
);
CREATE TABLE plan_usage_snapshots (
 id INTEGER PRIMARY KEY AUTOINCREMENT, source TEXT NOT NULL, account_id TEXT NOT NULL,
 plan TEXT NOT NULL, window_id TEXT NOT NULL, window_duration_minutes INTEGER NOT NULL,
 used_percent REAL NOT NULL, resets_at TEXT, captured_at TEXT NOT NULL,
 authority TEXT NOT NULL, raw_json TEXT NOT NULL,
 UNIQUE(source, account_id, window_id, captured_at)
);`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	for _, fixture := range fixtures {
		hash := sha256.Sum256(fixture.raw)
		_, err := database.Exec(`INSERT INTO otlp_exports (
received_at, signal, transport, payload_protobuf, payload_json, payload_sha256,
payload_size, source, normalizer_version, normalization_status, normalization_error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, now, canonical.SignalTrace, ingest.TransportGRPC,
			fixture.raw, payloadJSON, hex.EncodeToString(hash[:]),
			len(fixture.raw), fixture.source, fixture.version, fixture.status, fixture.normalizationError)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = database.Exec(`INSERT INTO plan_usage_snapshots (
source, account_id, plan, window_id, window_duration_minutes, used_percent,
resets_at, captured_at, authority, raw_json
) VALUES ('codex', 'account-1', 'plus', '5h', 300, 25, NULL, ?, 'provider', '{}')`, now)
	if err != nil {
		t.Fatal(err)
	}
}

func semanticTracePayload(t testing.TB, incidental int) []byte {
	t.Helper()
	traces := ptrace.NewTraces()
	resource := traces.ResourceSpans().AppendEmpty()
	resource.Resource().Attributes().PutStr("service.name", "codex")
	spans := resource.ScopeSpans().AppendEmpty().Spans()
	for range incidental {
		spans.AppendEmpty().SetName("handle_responses")
	}
	semantic := spans.AppendEmpty()
	semantic.SetName("codex.sse_event")
	semantic.Attributes().PutStr("event.kind", "response.completed")
	semantic.Attributes().PutStr("conversation.id", "conversation-1")
	raw, err := ptraceotlp.NewExportRequestFromTraces(traces).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func createCurrentDatabase(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "current.db")
	createCurrentDatabaseAt(t, path)
	return path
}

func createCurrentDatabaseAt(t *testing.T, path string) {
	t.Helper()
	database, err := store.Open(path, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sqlDB.Exec(fmt.Sprintf(`PRAGMA user_version=%d`, CurrentStorageGeneration))
	_ = sqlDB.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func writeBytes(t *testing.T, path string, payload []byte) {
	t.Helper()
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
}
