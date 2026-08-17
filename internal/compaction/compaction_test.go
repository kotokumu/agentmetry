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
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/source/builtin"
	store "github.com/kotokumu/agentmetry/internal/storage/sqlite"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"
	_ "modernc.org/sqlite"
)

func TestMigrateRebuildsTrueLegacySchemaAndPreservesJournalMetadata(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "legacy.db")
	raw := semanticTracePayload(t, 300)
	createLegacyDatabase(t, sourcePath, []legacyFixture{
		{raw: raw, source: "codex", version: 3, status: "projected"},
		{raw: []byte{0x0a, 0x00}, source: "legacy-source", version: 7, status: "failed", normalizationError: "unsupported source revision"},
	})

	result, err := MigrateIfNeeded(context.Background(), sourcePath, builtin.Registry(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Migrated || result.Exports != 2 || result.SemanticSpans != 1 {
		t.Fatalf("unexpected migration result: %#v", result)
	}
	if result.CompactBytes >= result.SourceBytes*3/10 {
		t.Fatalf("compact database %d bytes is not below 30%% of legacy %d bytes", result.CompactBytes, result.SourceBytes)
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

func TestReleaseAndAggregationGenerationsRebuildCodexSessionMemberships(t *testing.T) {
	for _, generation := range []int{2, 3} {
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
	if _, err := sqlDB.Exec("ALTER TABLE observations ADD COLUMN obsolete_projection_json TEXT NOT NULL DEFAULT ''"); err != nil {
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
	var gotHash string
	if err := verifyDB.QueryRow("SELECT payload_sha256 FROM otlp_exports").Scan(&gotHash); err != nil || gotHash != wantHash {
		t.Fatalf("journal hash=%q want=%q err=%v", gotHash, wantHash, err)
	}
	hasObsolete, err := columnExists(context.Background(), verifyDB, "observations", "obsolete_projection_json")
	if err != nil || hasObsolete {
		t.Fatalf("obsolete projection schema remains: exists=%v err=%v", hasObsolete, err)
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
		if progress.Completed == 1 {
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
			fixture.raw, strings.Repeat("legacy-json", 500000), hex.EncodeToString(hash[:]),
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

func semanticTracePayload(t *testing.T, incidental int) []byte {
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
