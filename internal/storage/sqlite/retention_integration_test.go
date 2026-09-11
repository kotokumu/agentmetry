package sqlite

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/archivefs"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/journal"
	"github.com/kotokumu/agentmetry/internal/retention"
	"github.com/kotokumu/agentmetry/internal/source/builtin"
	"github.com/kotokumu/agentmetry/sourceplugin"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
)

func TestArchiveRestoreRoundTripRemovesAndRebuildsActiveData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "retention.db")
	store, err := open(path, false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	receivedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	logs := plog.NewLogs()
	resource := logs.ResourceLogs().AppendEmpty()
	resource.Resource().Attributes().PutStr("service.name", "codex")
	record := resource.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	record.SetEventName("codex.sse_event")
	record.SetTimestamp(pcommon.NewTimestampFromTime(receivedAt))
	record.Attributes().PutStr("event.kind", "response.completed")
	record.Attributes().PutStr("conversation.id", "retention-session")
	record.Attributes().PutStr("model", "gpt-6-astra")
	raw, err := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := otel.ReplayExport(canonical.SignalLog, ingest.TransportGRPC, receivedAt, raw, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	activeCapacity, err := store.RetentionCapacity(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if !activeCapacity.ObservedAt.Equal(now) || activeCapacity.DatabaseBytes <= 0 || activeCapacity.TotalAllocatedBytes < activeCapacity.DatabaseBytes+activeCapacity.WALBytes || activeCapacity.ActiveRawBytes <= 0 || activeCapacity.ObservationBytes <= 0 || activeCapacity.QueryProjectionBytes <= 0 || activeCapacity.EstimatedArchivePeakBytes != int64(len(raw))*2 || !activeCapacity.FilesystemAvailable || activeCapacity.FilesystemFreeBytes <= 0 {
		t.Fatalf("unexpected active capacity report: %+v", activeCapacity)
	}
	clock := now
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return clock })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	assertCount := func(table string, want int) {
		t.Helper()
		var got int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s count=%d want=%d", table, got, want)
		}
	}
	assertCount("otlp_exports", 0)
	assertCount("observations", 0)
	assertCount("logs", 0)
	var state string
	if err := store.db.QueryRow(`SELECT state FROM retained_exports WHERE id = 1`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "archived" {
		t.Fatalf("state=%s", state)
	}
	archivedCapacity, err := store.RetentionCapacity(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if archivedCapacity.ArchiveBytes <= 0 || archivedCapacity.ArchiveAllocatedBytes <= 0 || archivedCapacity.EstimatedRestorePeakBytes != int64(len(raw))*2 {
		t.Fatalf("unexpected archived capacity report: %+v", archivedCapacity)
	}
	segments, _, err := service.Segments(ctx, "", 100)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	if _, err := store.UpdateRetentionPolicy(ctx, false, 0, 0, now); err != nil {
		t.Fatal(err)
	}
	hold, _ := retention.NewHold(2)
	scope, _ := retention.SegmentScope(segments[0].ID)
	if err := service.Restore(ctx, scope, hold); err != nil {
		t.Fatal(err)
	}
	assertCount("otlp_exports", 1)
	assertCount("observations", len(accepted.Observations))
	assertCount("logs", len(accepted.Projection.Logs))
	var restored []byte
	var codec, hashText string
	var size int
	if err := store.db.QueryRow(`SELECT payload_protobuf, payload_codec, payload_sha256, payload_size FROM otlp_exports WHERE id = 1`).Scan(&restored, &codec, &hashText, &size); err != nil {
		t.Fatal(err)
	}
	payload, err := restoreTestPayload(codec, restored, size, hashText)
	if err != nil || !bytes.Equal(payload, raw) {
		t.Fatalf("raw round trip failed: %v", err)
	}
	clock = now.Add(24 * time.Hour)
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	assertCount("otlp_exports", 1)
	assertForeignKeys(t, store.db)
}

func TestRetentionCapacitySeparatesReusablePagesFromAllocatedFile(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "capacity-freelist.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	payload := make([]byte, 256<<10)
	if _, err := cryptorand.Read(payload); err != nil {
		t.Fatal(err)
	}
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-10*retention.Day), payload), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
	if err := store.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	before, err := store.RetentionCapacity(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	after, err := store.RetentionCapacity(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if after.DatabaseUnusedBytes <= 0 || after.DatabaseBytes < before.DatabaseBytes || after.ArchiveAllocatedBytes <= 0 || before.EstimatedArchivePeakBytes != int64(len(payload))*2 {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
	databaseAllocated, err := allocatedFileBytes(store.path)
	if err != nil {
		t.Fatal(err)
	}
	segments, _, err := store.ListArchiveSegments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	archiveAllocated, err := allocatedFileBytes(filepath.Join(store.ArchiveDirectory(), segments[0].FileName))
	if err != nil {
		t.Fatal(err)
	}
	if after.DatabaseBytes != databaseAllocated || after.ArchiveAllocatedBytes != archiveAllocated {
		t.Fatalf("reported database/archive allocation=%d/%d, OS allocation=%d/%d", after.DatabaseBytes, after.ArchiveAllocatedBytes, databaseAllocated, archiveAllocated)
	}
	if classified := after.ActiveRawBytes + after.ObservationBytes + after.QueryProjectionBytes; classified > after.DatabaseBytes {
		t.Fatalf("classified SQLite pages overlap allocation: classes=%d database=%d", classified, after.DatabaseBytes)
	}
}

func TestRetentionCapacityWarningRequiresKnownInsufficientFilesystemSpace(t *testing.T) {
	if got := retentionCapacityWarning(101, 100, true); got == "" {
		t.Fatal("known insufficient space did not produce a warning")
	}
	if got := retentionCapacityWarning(100, 100, true); got != "" {
		t.Fatalf("equal capacity warning=%q", got)
	}
	if got := retentionCapacityWarning(101, 0, false); got != "" {
		t.Fatalf("unavailable filesystem warning=%q", got)
	}
}

func TestArchivePublicationRevalidatesCurrentPolicyAndHold(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(context.Context, *Store, retention.Policy, time.Time) error
	}{
		{name: "policy disabled", change: func(ctx context.Context, store *Store, _ retention.Policy, now time.Time) error {
			_, err := store.UpdateRetentionPolicy(ctx, false, 0, 0, now)
			return err
		}},
		{name: "hold added", change: func(ctx context.Context, store *Store, _ retention.Policy, now time.Time) error {
			_, err := store.db.ExecContext(ctx, `UPDATE retained_exports SET hold_until = ? WHERE id = 1`, formatTime(now.Add(retention.Day)))
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := open(filepath.Join(t.TempDir(), "archive-revalidation.db"), false, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
			accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-10*retention.Day), []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
			if err := store.CommitExport(ctx, accepted); err != nil {
				t.Fatal(err)
			}
			policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
			if err != nil {
				t.Fatal(err)
			}
			exports, err := store.EligibleActiveExports(ctx, policy, now)
			if err != nil || len(exports) != 1 {
				t.Fatalf("exports=%v err=%v", exports, err)
			}
			claim, err := store.ClaimArchive(ctx, exports, policy, now, "")
			if err != nil {
				t.Fatal(err)
			}
			archives := archivefs.New(store.ArchiveDirectory())
			installed, err := archives.BuildWithIncarnation(ctx, exports, claim.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.change(ctx, store, policy, now); err != nil {
				t.Fatal(err)
			}
			publication := retention.ArchivePublication{OperationID: claim.OperationID, SegmentID: installed.SegmentID, FileName: installed.Path, FileSHA256: installed.FileSHA256, MembershipSHA256: installed.MembershipSHA256, StoredBytes: installed.StoredBytes, OriginalBytes: installed.OriginalBytes, Exports: exports, EvaluatedAt: now, PolicyRevision: policy.Revision}
			publishErr := store.PublishArchive(ctx, publication)
			if publishErr == nil {
				t.Fatal("stale archive decision was published")
			}
			if err := store.FailArchive(ctx, claim.OperationID, publishErr, now); err != nil {
				t.Fatal(err)
			}
			if err := archives.Remove(installed.SegmentID); err != nil {
				t.Fatal(err)
			}
			var state, operation string
			var raw, segments, authority int
			if err := store.db.QueryRow(`SELECT state FROM retained_exports WHERE id = 1`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRow(`SELECT status FROM retention_operations WHERE id = ?`, claim.OperationID).Scan(&operation); err != nil {
				t.Fatal(err)
			}
			_ = store.db.QueryRow(`SELECT COUNT(*) FROM otlp_exports WHERE id = 1`).Scan(&raw)
			_ = store.db.QueryRow(`SELECT COUNT(*) FROM archive_segments`).Scan(&segments)
			_ = store.db.QueryRow(`SELECT COUNT(*) FROM retention_export_authorities WHERE operation_id = ?`, claim.OperationID).Scan(&authority)
			if state != "active" || operation != "failed" || raw != 1 || segments != 0 || authority != 0 {
				t.Fatalf("state=%s operation=%s raw=%d segments=%d authority=%d", state, operation, raw, segments, authority)
			}
		})
	}
}

func TestArchiveFallsBackToRemainingSpanProjectionCandidate(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "fallback.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	makeExport := func(received time.Time, name string) ingest.AcceptedExport {
		span := canonical.Span{Source: "codex", TraceID: "00000000000000000000000000000001", SpanID: "0000000000000001", Name: name,
			Kind: canonical.ActivityResponse, StartedAt: received, EndedAt: received, Agent: canonical.AgentContext{RunID: "fallback-session"}}
		return ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, received, []byte{0x0a, 0x00}),
			Journal: ingest.JournalMetadata{Source: "codex", NormalizerVersion: 1, NormalizationStatus: "projected"}, Projection: canonical.Batch{Spans: []canonical.Span{span}}}
	}
	// Projection order, not receive time, decides the currently visible retry winner.
	if err := store.CommitExport(ctx, makeExport(now.Add(-time.Hour), "remaining")); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, makeExport(now.Add(-10*24*time.Hour), "archived-winner")); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := store.db.QueryRow(`SELECT name FROM spans`).Scan(&name); err != nil || name != "archived-winner" {
		t.Fatalf("initial winner=%q err=%v", name, err)
	}
	policy, err := store.UpdateRetentionPolicy(ctx, true, 5, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	exports, err := store.EligibleActiveExports(ctx, policy, now)
	if err != nil || len(exports) != 1 || exports[0].ID != 2 {
		t.Fatalf("eligible=%v err=%v", exports, err)
	}
	archives := archivefs.New(store.ArchiveDirectory())
	installed, err := archives.Build(ctx, []archivefs.Export{archiveExportForTest(exports[0])})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimArchive(ctx, exports, policy, now, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishArchive(ctx, retention.ArchivePublication{OperationID: claim.OperationID, SegmentID: installed.SegmentID, FileName: installed.Path,
		FileSHA256: installed.FileSHA256, MembershipSHA256: installed.MembershipSHA256, StoredBytes: installed.StoredBytes,
		OriginalBytes: installed.OriginalBytes, Exports: exports, EvaluatedAt: now, PolicyRevision: policy.Revision}); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT name, export_id FROM spans`).Scan(&name, new(int64)); err != nil || name != "remaining" {
		t.Fatalf("fallback winner=%q err=%v", name, err)
	}
}

type fixedSpanRestoreReplayer struct {
	name string
}

func (replayer fixedSpanRestoreReplayer) Replay(signal canonical.Signal, transport ingest.Transport, receivedAt time.Time, protobuf []byte, _ sourceplugin.Registry) (ingest.AcceptedExport, error) {
	span := canonical.Span{Source: "codex", TraceID: "00000000000000000000000000000002", SpanID: "0000000000000002", Name: replayer.name,
		Kind: canonical.ActivityResponse, StartedAt: receivedAt, EndedAt: receivedAt, Agent: canonical.AgentContext{RunID: "restore-precedence-session"}}
	return ingest.AcceptedExport{Envelope: ingest.NewEnvelope(signal, transport, receivedAt, protobuf),
		Journal: ingest.JournalMetadata{Source: "codex", NormalizerVersion: 1, NormalizationStatus: "projected"}, Projection: canonical.Batch{Spans: []canonical.Span{span}}}, nil
}

type failedNormalizationRestoreReplayer struct{}

func (failedNormalizationRestoreReplayer) Replay(signal canonical.Signal, transport ingest.Transport, receivedAt time.Time, protobuf []byte, _ sourceplugin.Registry) (ingest.AcceptedExport, error) {
	return ingest.AcceptedExport{
		Envelope:           ingest.NewEnvelope(signal, transport, receivedAt, protobuf),
		Journal:            ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 99, NormalizationStatus: "failed"},
		NormalizationError: "injected current normalization failure",
	}, nil
}

func TestRestoreKeepsNewestStableExportAsProjectionWinner(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "restore-precedence.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	makeExport := func(received time.Time, name string) ingest.AcceptedExport {
		span := canonical.Span{Source: "codex", TraceID: "00000000000000000000000000000002", SpanID: "0000000000000002", Name: name,
			Kind: canonical.ActivityResponse, StartedAt: received, EndedAt: received, Agent: canonical.AgentContext{RunID: "restore-precedence-session"}}
		return ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, received, []byte{0x0a, 0x00}),
			Journal: ingest.JournalMetadata{Source: "codex", NormalizerVersion: 1, NormalizationStatus: "projected"}, Projection: canonical.Batch{Spans: []canonical.Span{span}}}
	}
	if err := store.CommitExport(ctx, makeExport(now.Add(-10*retention.Day), "older-archived")); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, makeExport(now.Add(-time.Hour), "newer-active")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateRetentionPolicy(ctx, true, 5, 30, now); err != nil {
		t.Fatal(err)
	}
	archiveService := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := archiveService.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := archiveService.Segments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	restoreService := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), fixedSpanRestoreReplayer{name: "older-archived"}, builtin.Registry(), func() time.Time { return now })
	if err := restoreService.Restore(ctx, mustSegmentScope(t, segments[0].ID), mustHold(t, 2)); err != nil {
		t.Fatal(err)
	}
	var name string
	var exportID int64
	if err := store.db.QueryRow(`SELECT name, export_id FROM spans WHERE trace_id = ? AND span_id = ?`, "00000000000000000000000000000002", "0000000000000002").Scan(&name, &exportID); err != nil {
		t.Fatal(err)
	}
	if name != "newer-active" || exportID != 2 {
		t.Fatalf("projection winner=%q export=%d, want newer stable export 2", name, exportID)
	}
}

func TestRestorePublishesCurrentNormalizationFailureWithoutDerivedData(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "restore-normalization-failure.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-10*retention.Day), []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "historic failure"}
	if err := store.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	archives := archivefs.New(store.ArchiveDirectory())
	archiveService := retention.NewServiceWithReplayer(store, archives, retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := archiveService.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := store.ListArchiveSegments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	restoreService := retention.NewServiceWithReplayer(store, archives, failedNormalizationRestoreReplayer{}, builtin.Registry(), func() time.Time { return now })
	if err := restoreService.Restore(ctx, mustSegmentScope(t, segments[0].ID), mustHold(t, 2)); err != nil {
		t.Fatal(err)
	}
	var state, normalizationStatus, normalizationError string
	if err := store.db.QueryRow(`SELECT r.state, e.normalization_status, e.normalization_error FROM retained_exports r JOIN otlp_exports e ON e.id = r.id WHERE r.id = 1`).Scan(&state, &normalizationStatus, &normalizationError); err != nil {
		t.Fatal(err)
	}
	var observations int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM observations WHERE export_id = 1`).Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if state != "active" || normalizationStatus != "failed" || normalizationError != "injected current normalization failure" || observations != 0 {
		t.Fatalf("state=%s normalization=%s error=%q observations=%d", state, normalizationStatus, normalizationError, observations)
	}
}

func TestRestoreConflictAndCorruptAtomicSetLeaveEveryExportArchived(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "restore-corrupt-set.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	for index := range 2 {
		accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-10*retention.Day+time.Duration(index)*time.Hour), []byte{0x0a, byte(index)}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
		if err := store.CommitExport(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	archives := archivefs.New(store.ArchiveDirectory())
	service := retention.NewServiceWithReplayer(store, archives, retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := store.ListArchiveSegments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	scope := mustSegmentScope(t, segments[0].ID)
	claim, err := store.ClaimRestore(ctx, scope, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimRestore(ctx, scope, now); !errors.Is(err, retention.ErrRestoreConflict) {
		t.Fatalf("overlapping restore error=%v", err)
	}
	if err := store.FailRestore(ctx, claim.OperationID, errors.New("release conflict fixture"), now); err != nil {
		t.Fatal(err)
	}
	var fileName string
	if err := store.db.QueryRow(`SELECT file_name FROM archive_segments WHERE id = ?`, segments[0].ID).Scan(&fileName); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(store.ArchiveDirectory(), fileName)
	content, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	content[len(content)-1] ^= 0xff
	if err := os.WriteFile(archivePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := service.Restore(ctx, scope, mustHold(t, 2)); err == nil {
		t.Fatal("corrupt atomic restore unexpectedly succeeded")
	}
	var archived, activeRaw, authorities int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM retained_exports WHERE state = 'archived'`).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM otlp_exports`).Scan(&activeRaw); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM retention_export_authorities`).Scan(&authorities); err != nil {
		t.Fatal(err)
	}
	if archived != 2 || activeRaw != 0 || authorities != 0 {
		t.Fatalf("archived=%d activeRaw=%d authorities=%d", archived, activeRaw, authorities)
	}
}

func TestRestoreAndDeletionArbitrationAtSerializedCutover(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "restore-delete-arbitration.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	for index := range 2 {
		accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-time.Duration(40+index)*retention.Day), []byte{0x0a, byte(index)}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
		if err := store.CommitExport(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	archives := archivefs.New(store.ArchiveDirectory())
	service := retention.NewServiceWithReplayer(store, archives, retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := store.ListArchiveSegments(ctx, "", 10)
	if err != nil || len(segments) != 2 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	seedDelete := func(operationID, phase, segmentID string) (int64, retention.DeletionHandle) {
		t.Helper()
		var exportID int64
		if err := store.db.QueryRow(`SELECT export_id FROM current_archive_memberships WHERE segment_id = ?`, segmentID).Scan(&exportID); err != nil {
			t.Fatal(err)
		}
		file, err := archives.DeletionFile(segmentID)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.StageAndSync(); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`INSERT INTO retention_operations (id, kind, status, phase, requested_at, evaluated_at, staging_token, decision_policy_revision, decision_cutoff_days, affected_export_count, affected_segment_count) VALUES (?, 'delete', 'running', ?, ?, ?, ?, ?, 30, 1, 1)`, operationID, phase, formatTime(now), formatTime(now), segmentID, policy.Revision); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`INSERT INTO retention_operation_exports (operation_id, export_id, role) VALUES (?, ?, 'affected')`, operationID, exportID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`INSERT INTO retention_export_authorities (export_id, operation_id, kind, role) VALUES (?, ?, 'delete', 'affected')`, exportID, operationID); err != nil {
			t.Fatal(err)
		}
		return exportID, file
	}
	_, preCutoverFile := seedDelete("pre-cutover-delete", "staged", segments[0].ID)
	claim, err := store.ClaimRestore(ctx, mustSegmentScope(t, segments[0].ID), now)
	if err != nil {
		t.Fatal(err)
	}
	var deleteStatus string
	if err := store.db.QueryRow(`SELECT status FROM retention_operations WHERE id = 'pre-cutover-delete'`).Scan(&deleteStatus); err != nil {
		t.Fatal(err)
	}
	presence, err := preCutoverFile.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if deleteStatus != "cancelled" || presence != retention.DeletionInstalled || claim.OperationID == "" {
		t.Fatalf("pre-cutover delete=%s presence=%s restore=%s", deleteStatus, presence, claim.OperationID)
	}
	if err := store.FailRestore(ctx, claim.OperationID, errors.New("finish arbitration fixture"), now); err != nil {
		t.Fatal(err)
	}
	postExportID, _ := seedDelete("post-cutover-delete", "delete_committing", segments[1].ID)
	postClaim, err := store.ClaimRestore(ctx, mustSegmentScope(t, segments[1].ID), now)
	if err != nil || postClaim.OperationID != "" {
		t.Fatalf("post-cutover restore claim=%+v error=%v", postClaim, err)
	}
	classification, err := store.ClassifyRestoreScope(ctx, mustSegmentScope(t, segments[1].ID))
	if err != nil {
		t.Fatal(err)
	}
	var postState string
	var holdUntil sql.NullString
	if err := store.db.QueryRow(`SELECT state, hold_until FROM retained_exports WHERE id = ?`, postExportID).Scan(&postState, &holdUntil); err != nil {
		t.Fatal(err)
	}
	if postState != "deleted" || holdUntil.Valid || classification.Result != "deleted" || classification.DeletedMatches != 1 {
		t.Fatalf("post-cutover state=%s hold=%v classification=%+v", postState, holdUntil, classification)
	}
}

func TestPostPONRRestoreDoesNotAggregateLiveMaintenanceParent(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "post-ponr-parent.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-40*retention.Day), []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
	if err := store.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := store.ListArchiveSegments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	cycleID, err := store.BeginRetentionCycle(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO retention_operations (id, cycle_id, kind, status, phase, requested_at, evaluated_at, staging_token, decision_policy_revision, decision_cutoff_days, affected_export_count, affected_segment_count) VALUES ('live-delete', ?, 'delete', 'running', 'delete_committing', ?, ?, ?, ?, 30, 1, 1)`, cycleID, formatTime(now), formatTime(now), segments[0].ID, policy.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO retention_operation_exports (operation_id, export_id, role) VALUES ('live-delete', 1, 'affected')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO retention_export_authorities (export_id, operation_id, kind, role) VALUES (1, 'live-delete', 'delete', 'affected')`); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRetentionCyclePlanned(ctx, cycleID, 1); err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimRestore(ctx, mustSegmentScope(t, segments[0].ID), now)
	if err != nil || claim.OperationID != "" || claim.Classification.Result != "deleted" {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	var childStatus, parentStatus string
	if err := store.db.QueryRow(`SELECT status FROM retention_operations WHERE id = 'live-delete'`).Scan(&childStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT status FROM retention_cycles WHERE id = ?`, cycleID).Scan(&parentStatus); err != nil {
		t.Fatal(err)
	}
	if childStatus != "completed" || parentStatus != "running" {
		t.Fatalf("child=%s parent=%s", childStatus, parentStatus)
	}
	cycleErr := errors.New("live maintenance failed after delete convergence")
	if err := store.CompleteRetentionCycle(ctx, cycleID, cycleErr); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT status FROM retention_cycles WHERE id = ?`, cycleID).Scan(&parentStatus); err != nil {
		t.Fatal(err)
	}
	if parentStatus != "failed" {
		t.Fatalf("parent status=%s", parentStatus)
	}
}

func TestExpiredArchiveBecomesTerminalDeletionEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "delete.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-40*24*time.Hour), []byte{0x0a, 0x00}),
		Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "projected"}}
	if err := store.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	var firstState string
	if err := store.db.QueryRow(`SELECT state FROM retained_exports WHERE id = 1`).Scan(&firstState); err != nil {
		t.Fatal(err)
	}
	if firstState != "archived" {
		t.Fatalf("first successful cycle state=%s, want archived", firstState)
	}
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	var state string
	var revision, cutoff int
	var prior string
	if err := store.db.QueryRow(`SELECT state, deletion_policy_revision, deletion_cutoff_days, prior_segment_id FROM retained_exports WHERE id = 1`).Scan(&state, &revision, &cutoff, &prior); err != nil {
		t.Fatal(err)
	}
	if state != "deleted" || revision == 0 || cutoff != 30 || prior == "" {
		t.Fatalf("terminal evidence state=%s revision=%d cutoff=%d prior=%q", state, revision, cutoff, prior)
	}
	if err := service.Restore(ctx, mustSegmentScope(t, prior), mustHold(t, 1)); err != nil {
		t.Fatal(err)
	}
	classification, err := store.ClassifyRestoreScope(ctx, mustSegmentScope(t, prior))
	if err != nil || classification.Result != "deleted" || classification.DeletedMatches != 1 {
		t.Fatalf("deleted classification=%+v err=%v", classification, err)
	}
}

func TestDeletionRequiresWholeSegmentAgeAndVerifiableMetadataButNotIntactPayload(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "deletion-eligibility.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	received := []time.Time{now.Add(-30*retention.Day - time.Hour), now.Add(-30*retention.Day + time.Hour), now.Add(-40 * retention.Day)}
	for _, receivedAt := range received {
		accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, receivedAt, []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
		if err := store.CommitExport(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	archiveAt := now.Add(40 * retention.Day)
	exports, err := store.EligibleActiveExports(ctx, policy, archiveAt)
	if err != nil || len(exports) != 3 {
		t.Fatalf("exports=%v err=%v", exports, err)
	}
	byID := make(map[int64]retention.RawExport, len(exports))
	for _, exported := range exports {
		byID[exported.ID] = exported
	}
	archives := archivefs.New(store.ArchiveDirectory())
	publish := func(group []retention.RawExport, incarnation string) retention.ArchiveInstall {
		t.Helper()
		claim, err := store.ClaimArchive(ctx, group, policy, archiveAt, "")
		if err != nil {
			t.Fatal(err)
		}
		installed, err := archives.BuildWithIncarnation(ctx, group, incarnation)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.PublishArchive(ctx, retention.ArchivePublication{OperationID: claim.OperationID, SegmentID: installed.SegmentID, FileName: installed.Path, FileSHA256: installed.FileSHA256, MembershipSHA256: installed.MembershipSHA256, StoredBytes: installed.StoredBytes, OriginalBytes: installed.OriginalBytes, Exports: group, EvaluatedAt: archiveAt, PolicyRevision: policy.Revision}); err != nil {
			t.Fatal(err)
		}
		return installed
	}
	publish([]retention.RawExport{byID[1], byID[2]}, "mixed")
	old := publish([]retention.RawExport{byID[3]}, "old")
	eligible, err := store.EligibleDeletionSegments(ctx, policy, now)
	if err != nil || len(eligible) != 1 || eligible[0].ID != old.SegmentID {
		t.Fatalf("eligible segments=%v err=%v", eligible, err)
	}
	if _, err := store.db.Exec(`UPDATE archive_segments SET payload_integrity = 'corrupt' WHERE id = ?`, old.SegmentID); err != nil {
		t.Fatal(err)
	}
	eligible, err = store.EligibleDeletionSegments(ctx, policy, now)
	if err != nil || len(eligible) != 1 || eligible[0].ID != old.SegmentID {
		t.Fatalf("corrupt payload eligibility=%v err=%v", eligible, err)
	}
	if _, err := store.db.Exec(`UPDATE archive_segments SET metadata_integrity = 'unverifiable' WHERE id = ?`, old.SegmentID); err != nil {
		t.Fatal(err)
	}
	eligible, err = store.EligibleDeletionSegments(ctx, policy, now)
	if err != nil || len(eligible) != 0 {
		t.Fatalf("unverifiable metadata eligibility=%v err=%v", eligible, err)
	}
	if _, err := store.db.Exec(`UPDATE archive_segments SET metadata_integrity = 'verifiable' WHERE id = ?`, old.SegmentID); err != nil {
		t.Fatal(err)
	}
	file, err := archives.DeletionFile(old.SegmentID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteArchiveSegment(ctx, old.SegmentID, policy, now, "", file); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := store.db.QueryRow(`SELECT state FROM retained_exports WHERE id = 3`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "deleted" {
		t.Fatalf("corrupt payload export state=%s", state)
	}
}

type syncFailureDeletionFile struct{ archivefs.DeletionHandle }

func (file syncFailureDeletionFile) RemoveAndSync() error {
	err := file.DeletionHandle.RemoveAndSync()
	return errors.Join(err, errors.New("injected directory sync failure after unlink"))
}

type inspectFailureDeletionFile struct {
	archivefs.DeletionHandle
	failAt int
	calls  int
}

type policyChangingDeletionFile struct {
	archivefs.DeletionHandle
	change func() error
}

func (file policyChangingDeletionFile) StageAndSync() error {
	if err := file.DeletionHandle.StageAndSync(); err != nil {
		return err
	}
	return file.change()
}

func TestDeletionRevalidatesCurrentPolicyBeforePointOfNoReturn(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "delete-policy-revalidation.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-40*retention.Day), []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
	if err := store.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := store.ListArchiveSegments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	handle, err := archivefs.New(store.ArchiveDirectory()).DeletionFile(segments[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	file := policyChangingDeletionFile{DeletionHandle: handle, change: func() error {
		_, err := store.db.ExecContext(ctx, `UPDATE retention_policy SET enabled = 0, archive_days = NULL, delete_days = NULL, revision = revision + 1, updated_at = ? WHERE id = 1`, formatTime(now))
		return err
	}}
	if err := store.DeleteArchiveSegment(ctx, segments[0].ID, policy, now, "", file); err != nil {
		t.Fatal(err)
	}
	var state, segmentState, operation string
	var authority, memberships int
	if err := store.db.QueryRow(`SELECT state FROM retained_exports WHERE id = 1`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT reference_state FROM archive_segments WHERE id = ?`, segments[0].ID).Scan(&segmentState); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT status FROM retention_operations WHERE kind = 'delete' ORDER BY requested_at DESC LIMIT 1`).Scan(&operation); err != nil {
		t.Fatal(err)
	}
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM retention_export_authorities`).Scan(&authority)
	_ = store.db.QueryRow(`SELECT COUNT(*) FROM current_archive_memberships WHERE segment_id = ?`, segments[0].ID).Scan(&memberships)
	presence, err := handle.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if state != "archived" || segmentState != "current" || operation != "cancelled" || authority != 0 || memberships != 1 || presence != retention.DeletionInstalled {
		t.Fatalf("state=%s segment=%s operation=%s authority=%d memberships=%d presence=%s", state, segmentState, operation, authority, memberships, presence)
	}
}

func (file *inspectFailureDeletionFile) Inspect() (retention.DeletionPresence, error) {
	file.calls++
	if file.calls == file.failAt {
		return "", errors.New("injected deletion inspect failure")
	}
	return file.DeletionHandle.Inspect()
}

func TestDeletionPublishesTruthWhenDirectorySyncFailsAfterUnlink(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "delete-sync.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-40*retention.Day), []byte{0x0a, 0x00}),
		Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "projected"}}
	if err := store.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := store.ListArchiveSegments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	file, err := archivefs.New(store.ArchiveDirectory()).DeletionFile(segments[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteArchiveSegment(ctx, segments[0].ID, policy, now, "", syncFailureDeletionFile{file}); err != nil {
		t.Fatalf("missing content after unlink must converge to Deleted evidence: %v", err)
	}
	var state, operationStatus string
	if err := store.db.QueryRow(`SELECT state FROM retained_exports WHERE id = 1`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT status FROM retention_operations WHERE kind = 'delete' ORDER BY requested_at DESC LIMIT 1`).Scan(&operationStatus); err != nil {
		t.Fatal(err)
	}
	if state != "deleted" || operationStatus != "completed" {
		t.Fatalf("state=%s operation=%s", state, operationStatus)
	}
}

func TestDeletionErrorConvergesInProcessAtEitherSideOfCutover(t *testing.T) {
	for _, test := range []struct {
		name                     string
		failAt                   int
		wantState, wantOperation string
	}{
		{name: "before cutover", failAt: 1, wantState: "archived", wantOperation: "cancelled"},
		{name: "after cutover", failAt: 2, wantState: "deleted", wantOperation: "completed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := open(filepath.Join(t.TempDir(), "delete-convergence.db"), false, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
			accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-40*retention.Day), []byte{0x0a, 0x00}),
				Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "projected"}}
			if err := store.CommitExport(ctx, accepted); err != nil {
				t.Fatal(err)
			}
			policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
			if err != nil {
				t.Fatal(err)
			}
			service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
			if err := service.RunMaintenance(ctx); err != nil {
				t.Fatal(err)
			}
			segments, _, err := store.ListArchiveSegments(ctx, "", 10)
			if err != nil || len(segments) != 1 {
				t.Fatalf("segments=%v err=%v", segments, err)
			}
			file, err := archivefs.New(store.ArchiveDirectory()).DeletionFile(segments[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			injected := &inspectFailureDeletionFile{DeletionHandle: file, failAt: test.failAt}
			if err := store.DeleteArchiveSegment(ctx, segments[0].ID, policy, now, "", injected); err == nil {
				t.Fatal("injected failure unexpectedly returned nil")
			}
			var state, operationStatus string
			if err := store.db.QueryRow(`SELECT state FROM retained_exports WHERE id = 1`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRow(`SELECT status FROM retention_operations WHERE kind = 'delete' ORDER BY requested_at DESC LIMIT 1`).Scan(&operationStatus); err != nil {
				t.Fatal(err)
			}
			if state != test.wantState || operationStatus != test.wantOperation {
				t.Fatalf("state=%s operation=%s, want state=%s operation=%s", state, operationStatus, test.wantState, test.wantOperation)
			}
		})
	}
}

func TestStartupRecoversSerializedDeletionFromFilePresence(t *testing.T) {
	for _, test := range []struct {
		name                     string
		phase, presence          string
		wantState, wantOperation string
		wantPresence             retention.DeletionPresence
	}{
		{name: "staging with installed content", phase: "staging", presence: "installed", wantState: "archived", wantOperation: "cancelled", wantPresence: retention.DeletionInstalled},
		{name: "staged with staged content", phase: "staged", presence: "staged", wantState: "archived", wantOperation: "cancelled", wantPresence: retention.DeletionInstalled},
		{name: "committing with installed content", phase: "delete_committing", presence: "installed", wantState: "deleted", wantOperation: "completed", wantPresence: retention.DeletionMissing},
		{name: "committing with staged content", phase: "delete_committing", presence: "staged", wantState: "deleted", wantOperation: "completed", wantPresence: retention.DeletionMissing},
		{name: "committing with missing content", phase: "delete_committing", presence: "missing", wantState: "deleted", wantOperation: "completed", wantPresence: retention.DeletionMissing},
		{name: "content removed", phase: "content_removed", presence: "missing", wantState: "deleted", wantOperation: "completed", wantPresence: retention.DeletionMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "recovery.db")
			store, err := open(path, false, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
			accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-40*retention.Day), []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
			if err := store.CommitExport(ctx, accepted); err != nil {
				t.Fatal(err)
			}
			policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
			if err != nil {
				t.Fatal(err)
			}
			exports, err := store.EligibleActiveExports(ctx, policy, now)
			if err != nil {
				t.Fatal(err)
			}
			archives := archivefs.New(store.ArchiveDirectory())
			installed, err := archives.Build(ctx, []archivefs.Export{archiveExportForTest(exports[0])})
			if err != nil {
				t.Fatal(err)
			}
			claim, err := store.ClaimArchive(ctx, exports, policy, now, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := store.PublishArchive(ctx, retention.ArchivePublication{OperationID: claim.OperationID, SegmentID: installed.SegmentID, FileName: installed.Path, FileSHA256: installed.FileSHA256, MembershipSHA256: installed.MembershipSHA256, StoredBytes: installed.StoredBytes, OriginalBytes: installed.OriginalBytes, Exports: exports, EvaluatedAt: now, PolicyRevision: policy.Revision}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(`INSERT INTO retention_operations (id, kind, status, phase, requested_at, evaluated_at, staging_token, decision_policy_revision, decision_cutoff_days) VALUES ('recovery-op', 'delete', 'running', ?, ?, ?, ?, ?, 30)`, test.phase, formatTime(now), formatTime(now), installed.SegmentID, policy.Revision); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(`INSERT INTO retention_operation_exports (operation_id, export_id, role) VALUES ('recovery-op', 1, 'affected')`); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.Exec(`INSERT INTO retention_export_authorities (export_id, operation_id, kind, role) VALUES (1, 'recovery-op', 'delete', 'affected')`); err != nil {
				t.Fatal(err)
			}
			handle, err := archives.DeletionFile(installed.SegmentID)
			if err != nil {
				t.Fatal(err)
			}
			if test.presence == "staged" {
				if err := handle.StageAndSync(); err != nil {
					t.Fatal(err)
				}
			}
			if test.presence == "missing" {
				if err := archives.Remove(installed.SegmentID); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := open(path, false, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			var state, operation string
			if err := reopened.db.QueryRow(`SELECT state FROM retained_exports WHERE id = 1`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if err := reopened.db.QueryRow(`SELECT status FROM retention_operations WHERE id = 'recovery-op'`).Scan(&operation); err != nil {
				t.Fatal(err)
			}
			if state != test.wantState || operation != test.wantOperation {
				t.Fatalf("state=%s operation=%s", state, operation)
			}
			recoveredHandle, err := archivefs.New(reopened.ArchiveDirectory()).DeletionFile(installed.SegmentID)
			if err != nil {
				t.Fatal(err)
			}
			presence, err := recoveredHandle.Inspect()
			if err != nil || presence != test.wantPresence {
				t.Fatalf("presence=%s err=%v want=%s", presence, err, test.wantPresence)
			}
			var authority, membership, raw int
			_ = reopened.db.QueryRow(`SELECT COUNT(*) FROM retention_export_authorities WHERE operation_id = 'recovery-op'`).Scan(&authority)
			_ = reopened.db.QueryRow(`SELECT COUNT(*) FROM current_archive_memberships WHERE segment_id = ?`, installed.SegmentID).Scan(&membership)
			_ = reopened.db.QueryRow(`SELECT COUNT(*) FROM otlp_exports WHERE id = 1`).Scan(&raw)
			if authority != 0 || (test.wantState == "archived" && (membership != 1 || raw != 0)) || (test.wantState == "deleted" && (membership != 0 || raw != 0)) {
				t.Fatalf("authority=%d membership=%d raw=%d", authority, membership, raw)
			}
		})
	}
}

func TestLaterMaintenanceResumesDurableDeletionWithoutRestart(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "maintenance-recovery.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-40*retention.Day), []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
	if err := store.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	archives := archivefs.New(store.ArchiveDirectory())
	service := retention.NewServiceWithReplayer(store, archives, retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := store.ListArchiveSegments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	cycleID, err := store.BeginRetentionCycle(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRetentionCyclePlanned(ctx, cycleID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO retention_operations (id, cycle_id, kind, status, phase, requested_at, evaluated_at, staging_token, decision_policy_revision, decision_cutoff_days, affected_export_count, affected_segment_count) VALUES ('retry-op', ?, 'delete', 'running', 'delete_committing', ?, ?, ?, ?, 30, 1, 1)`, cycleID, formatTime(now), formatTime(now), segments[0].ID, policy.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO retention_operation_exports (operation_id, export_id, role) VALUES ('retry-op', 1, 'affected')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO retention_export_authorities (export_id, operation_id, kind, role) VALUES (1, 'retry-op', 'delete', 'affected')`); err != nil {
		t.Fatal(err)
	}
	waitingCycles := make([]string, 2)
	for index := range waitingCycles {
		waitingCycles[index], err = store.BeginRetentionCycle(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	var state, operationStatus, cycleStatus string
	if err := store.db.QueryRow(`SELECT state FROM retained_exports WHERE id = 1`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT status FROM retention_operations WHERE id = 'retry-op'`).Scan(&operationStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT status FROM retention_cycles WHERE id = ?`, cycleID).Scan(&cycleStatus); err != nil {
		t.Fatal(err)
	}
	if state != "deleted" || operationStatus != "completed" || cycleStatus != "completed" {
		t.Fatalf("state=%s operation=%s cycle=%s", state, operationStatus, cycleStatus)
	}
	for _, waitingCycle := range waitingCycles {
		if err := store.db.QueryRow(`SELECT status FROM retention_cycles WHERE id = ?`, waitingCycle).Scan(&cycleStatus); err != nil {
			t.Fatal(err)
		}
		if cycleStatus != "running" {
			t.Fatalf("unrelated live cycle %s status=%s, want running", waitingCycle, cycleStatus)
		}
	}
}

func TestDeletionResumeAggregatesEachParentBeforeALaterFailure(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "batch-resume.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	for index := range 2 {
		accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-time.Duration(40+index)*retention.Day), []byte{0x0a, byte(index)}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
		if err := store.CommitExport(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}
	policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	archives := archivefs.New(store.ArchiveDirectory())
	service := retention.NewServiceWithReplayer(store, archives, retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := store.ListArchiveSegments(ctx, "", 10)
	if err != nil || len(segments) != 2 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	cycleIDs := make([]string, 2)
	for index, segment := range segments {
		cycleIDs[index], err = store.BeginRetentionCycle(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.MarkRetentionCyclePlanned(ctx, cycleIDs[index], 1); err != nil {
			t.Fatal(err)
		}
		var exportID int64
		if err := store.db.QueryRow(`SELECT export_id FROM current_archive_memberships WHERE segment_id = ?`, segment.ID).Scan(&exportID); err != nil {
			t.Fatal(err)
		}
		operationID := fmt.Sprintf("batch-retry-%d", index)
		requestedAt := formatTime(now.Add(time.Duration(index) * time.Second))
		if _, err := store.db.Exec(`INSERT INTO retention_operations (id, cycle_id, kind, status, phase, requested_at, evaluated_at, staging_token, decision_policy_revision, decision_cutoff_days, affected_export_count, affected_segment_count) VALUES (?, ?, 'delete', 'running', 'delete_committing', ?, ?, ?, ?, 30, 1, 1)`, operationID, cycleIDs[index], requestedAt, formatTime(now), segment.ID, policy.Revision); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`INSERT INTO retention_operation_exports (operation_id, export_id, role) VALUES (?, ?, 'affected')`, operationID, exportID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`INSERT INTO retention_export_authorities (export_id, operation_id, kind, role) VALUES (?, ?, 'delete', 'affected')`, exportID, operationID); err != nil {
			t.Fatal(err)
		}
	}
	orphanedParentID, err := store.BeginRetentionCycle(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRetentionCyclePlanned(ctx, orphanedParentID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO retention_operations (id, cycle_id, kind, status, phase, requested_at, evaluated_at, completed_at, affected_segment_count) VALUES ('orphaned-terminal', ?, 'delete', 'completed', 'terminal', ?, ?, ?, 1)`, orphanedParentID, formatTime(now.Add(-time.Hour)), formatTime(now), formatTime(now)); err != nil {
		t.Fatal(err)
	}
	realDeletionFile := store.deletionFile
	store.deletionFile = func(segmentID string) (retention.DeletionHandle, error) {
		if segmentID == segments[1].ID {
			return nil, errors.New("injected second deletion resolver failure")
		}
		return realDeletionFile(segmentID)
	}
	if err := store.ResumeRetentionDeletions(ctx); err == nil {
		t.Fatal("second deletion resolver unexpectedly succeeded")
	}
	var firstStatus, secondStatus, orphanedStatus string
	if err := store.db.QueryRow(`SELECT status FROM retention_cycles WHERE id = ?`, cycleIDs[0]).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT status FROM retention_cycles WHERE id = ?`, cycleIDs[1]).Scan(&secondStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT status FROM retention_cycles WHERE id = ?`, orphanedParentID).Scan(&orphanedStatus); err != nil {
		t.Fatal(err)
	}
	if firstStatus != "completed" || secondStatus != "running" || orphanedStatus != "completed" {
		t.Fatalf("first cycle=%s second cycle=%s rediscovered cycle=%s", firstStatus, secondStatus, orphanedStatus)
	}
}

func TestDeletionResumeRediscoversTerminalChildWithUnfinishedParent(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "parent-rediscovery.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	parentID, err := store.BeginRetentionCycle(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkRetentionCyclePlanned(ctx, parentID, 1); err != nil {
		t.Fatal(err)
	}
	waitingID, err := store.BeginRetentionCycle(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO retention_operations (id, cycle_id, kind, status, phase, requested_at, evaluated_at, completed_at, affected_segment_count) VALUES ('already-terminal', ?, 'delete', 'completed', 'terminal', ?, ?, ?, 1)`, parentID, formatTime(now), formatTime(now), formatTime(now)); err != nil {
		t.Fatal(err)
	}
	var recoverable int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM retention_cycles c WHERE c.id = ? AND c.status = 'running' AND c.planning_completed = 1 AND c.planned_children = (SELECT COUNT(*) FROM retention_operations o WHERE o.cycle_id = c.id) AND EXISTS (SELECT 1 FROM retention_operations o WHERE o.cycle_id = c.id AND o.kind = 'delete') AND NOT EXISTS (SELECT 1 FROM retention_operations o WHERE o.cycle_id = c.id AND o.status IN ('pending', 'running'))`, parentID).Scan(&recoverable); err != nil {
		t.Fatal(err)
	}
	if recoverable != 1 {
		t.Fatal("terminal deletion parent is not discoverable")
	}
	if err := store.ResumeRetentionDeletions(ctx); err != nil {
		t.Fatal(err)
	}
	var parentStatus, waitingStatus string
	if err := store.db.QueryRow(`SELECT status FROM retention_cycles WHERE id = ?`, parentID).Scan(&parentStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT status FROM retention_cycles WHERE id = ?`, waitingID).Scan(&waitingStatus); err != nil {
		t.Fatal(err)
	}
	if parentStatus != "completed" || waitingStatus != "running" {
		t.Fatalf("rediscovered parent=%s unrelated waiting=%s", parentStatus, waitingStatus)
	}
}

func TestStartupRecoversInterruptedArchiveClaimAndInstalledFile(t *testing.T) {
	for _, test := range []struct {
		phase       string
		installFile bool
	}{
		{phase: "planned"},
		{phase: "planned", installFile: true},
		{phase: "file_ready"},
		{phase: "file_ready", installFile: true},
	} {
		t.Run(fmt.Sprintf("phase=%s/installed=%v", test.phase, test.installFile), func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "archive-claim-recovery.db")
			store, err := open(path, false, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
			accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-10*retention.Day), []byte{0x0a, 0x00}),
				Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
			if err := store.CommitExport(ctx, accepted); err != nil {
				t.Fatal(err)
			}
			policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
			if err != nil {
				t.Fatal(err)
			}
			cycleID, err := store.BeginRetentionCycle(ctx, now)
			if err != nil {
				t.Fatal(err)
			}
			exports, err := store.EligibleActiveExports(ctx, policy, now)
			if err != nil {
				t.Fatal(err)
			}
			claim, err := store.ClaimArchive(ctx, exports, policy, now, cycleID)
			if err != nil {
				t.Fatal(err)
			}
			var installedPath string
			if test.installFile {
				installed, err := archivefs.New(store.ArchiveDirectory()).BuildWithIncarnation(ctx, exports, cycleID)
				if err != nil {
					t.Fatal(err)
				}
				installedPath = installed.Path
			}
			if test.phase != "planned" {
				if _, err := store.db.Exec(`UPDATE retention_operations SET phase = ? WHERE id = ?`, test.phase, claim.OperationID); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := open(path, false, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			var operationStatus, cycleStatus string
			if err := reopened.db.QueryRow(`SELECT status FROM retention_operations WHERE id = ?`, claim.OperationID).Scan(&operationStatus); err != nil {
				t.Fatal(err)
			}
			if err := reopened.db.QueryRow(`SELECT status FROM retention_cycles WHERE id = ?`, cycleID).Scan(&cycleStatus); err != nil {
				t.Fatal(err)
			}
			var authority int
			if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM retention_export_authorities WHERE operation_id = ?`, claim.OperationID).Scan(&authority); err != nil {
				t.Fatal(err)
			}
			eligible, err := reopened.EligibleActiveExports(ctx, policy, now)
			if err != nil {
				t.Fatal(err)
			}
			if operationStatus != "failed" || cycleStatus != "failed" || authority != 0 || len(eligible) != 1 {
				t.Fatalf("operation=%s cycle=%s authority=%d eligible=%d", operationStatus, cycleStatus, authority, len(eligible))
			}
			if installedPath != "" {
				if _, err := os.Stat(installedPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("unreferenced installed archive remains: %v", err)
				}
			}
		})
	}
}

func TestStartupRecoversRestoreInterruptedBeforePublication(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restore-claim-recovery.db")
	store, err := open(path, false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-10*retention.Day), []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
	if err := store.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := store.ListArchiveSegments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	claim, err := store.ClaimRestore(ctx, mustSegmentScope(t, segments[0].ID), now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := open(path, false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var state, operationStatus string
	var authority int
	if err := reopened.db.QueryRow(`SELECT state FROM retained_exports WHERE id = 1`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := reopened.db.QueryRow(`SELECT status FROM retention_operations WHERE id = ?`, claim.OperationID).Scan(&operationStatus); err != nil {
		t.Fatal(err)
	}
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM retention_export_authorities WHERE operation_id = ?`, claim.OperationID).Scan(&authority); err != nil {
		t.Fatal(err)
	}
	if state != "archived" || operationStatus != "failed" || authority != 0 {
		t.Fatalf("state=%s operation=%s authority=%d", state, operationStatus, authority)
	}
}

func TestStartupRecoversRestoreFileReadyAndContentRemovedPhases(t *testing.T) {
	for _, phase := range []string{"file_ready", "content_removed"} {
		t.Run(phase, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "restore-phase-recovery.db")
			store, err := open(path, false, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
			store.retentionNow = func() time.Time { return now }
			accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-10*retention.Day), []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
			if err := store.CommitExport(ctx, accepted); err != nil {
				t.Fatal(err)
			}
			if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
				t.Fatal(err)
			}
			archives := archivefs.New(store.ArchiveDirectory())
			service := retention.NewServiceWithReplayer(store, archives, retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
			if err := service.RunMaintenance(ctx); err != nil {
				t.Fatal(err)
			}
			segments, _, err := store.ListArchiveSegments(ctx, "", 10)
			if err != nil || len(segments) != 1 {
				t.Fatalf("segments=%v err=%v", segments, err)
			}
			segment := segments[0]
			claim, err := store.ClaimRestore(ctx, mustSegmentScope(t, segment.ID), now)
			if err != nil {
				t.Fatal(err)
			}
			wantState, wantOperation, wantSegment, wantPresence := "archived", "failed", "current", retention.DeletionInstalled
			if phase == "file_ready" {
				if _, err := store.db.Exec(`UPDATE retention_operations SET phase = 'file_ready' WHERE id = ?`, claim.OperationID); err != nil {
					t.Fatal(err)
				}
			} else {
				verified, err := archives.OpenVerified(ctx, segment.ID)
				if err != nil || len(verified.Exports) != 1 {
					t.Fatalf("verified=%v err=%v", verified.Exports, err)
				}
				replayed, err := otel.ReplayExport(canonical.Signal(verified.Exports[0].Signal), ingest.Transport(verified.Exports[0].Transport), verified.Exports[0].ReceivedAt, verified.Exports[0].Protobuf, builtin.Registry())
				if err != nil {
					t.Fatal(err)
				}
				replayed.Identity = ingest.RetainedIdentity{ID: verified.Exports[0].ID, PayloadOccurrence: verified.Exports[0].PayloadOccurrence}
				if err := store.PublishRestore(ctx, retention.RestorePublication{OperationID: claim.OperationID, Selected: []ingest.AcceptedExport{replayed}, OriginalSegments: []string{segment.ID}, Hold: mustHold(t, 2)}); err != nil {
					t.Fatal(err)
				}
				wantState, wantOperation, wantSegment, wantPresence = "active", "completed", "restored", retention.DeletionMissing
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := open(path, false, builtin.Registry())
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			var state, operation, segmentState string
			var authority, raw, observations int
			if err := reopened.db.QueryRow(`SELECT state FROM retained_exports WHERE id = 1`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if err := reopened.db.QueryRow(`SELECT status FROM retention_operations WHERE id = ?`, claim.OperationID).Scan(&operation); err != nil {
				t.Fatal(err)
			}
			if err := reopened.db.QueryRow(`SELECT reference_state FROM archive_segments WHERE id = ?`, segment.ID).Scan(&segmentState); err != nil {
				t.Fatal(err)
			}
			_ = reopened.db.QueryRow(`SELECT COUNT(*) FROM retention_export_authorities WHERE operation_id = ?`, claim.OperationID).Scan(&authority)
			_ = reopened.db.QueryRow(`SELECT COUNT(*) FROM otlp_exports WHERE id = 1`).Scan(&raw)
			_ = reopened.db.QueryRow(`SELECT COUNT(*) FROM observations WHERE export_id = 1`).Scan(&observations)
			handle, err := archivefs.New(reopened.ArchiveDirectory()).DeletionFile(segment.ID)
			if err != nil {
				t.Fatal(err)
			}
			presence, err := handle.Inspect()
			if err != nil {
				t.Fatal(err)
			}
			wantRaw := 0
			if phase == "content_removed" {
				wantRaw = 1
			}
			if state != wantState || operation != wantOperation || segmentState != wantSegment || authority != 0 || presence != wantPresence || raw != wantRaw || observations != 0 {
				t.Fatalf("state=%s operation=%s segment=%s authority=%d presence=%s raw=%d observations=%d", state, operation, segmentState, authority, presence, raw, observations)
			}
		})
	}
}

func TestRetentionCycleHighWaterExcludesExportsCommittedAfterStart(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "fixed-cohort.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	commitOld := func() {
		t.Helper()
		accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-10*retention.Day), []byte{0x0a, 0x00}),
			Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
		if err := store.CommitExport(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}
	commitOld()
	policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	cycleID, err := store.BeginRetentionCycle(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	commitOld()
	exports, err := store.EligibleActiveExportsForCycle(ctx, policy, now, cycleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(exports) != 1 || exports[0].ID != 1 {
		t.Fatalf("fixed cohort exports=%v, want only pre-start export 1", exports)
	}
}

func TestDeletionCohortExcludesSegmentsPublishedAfterCycleStart(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "fixed-segment-cohort.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	commitExpired := func() {
		accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-40*retention.Day), []byte{0x0a, 0x00}),
			Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
		if err := store.CommitExport(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}
	commitExpired()
	policy, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now)
	if err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	cycleID, err := store.BeginRetentionCycle(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	commitExpired()
	exports, err := store.EligibleActiveExports(ctx, policy, now)
	if err != nil || len(exports) != 1 {
		t.Fatalf("new archive exports=%v err=%v", exports, err)
	}
	claim, err := store.ClaimArchive(ctx, exports, policy, now, "")
	if err != nil {
		t.Fatal(err)
	}
	installed, err := archivefs.New(store.ArchiveDirectory()).BuildWithIncarnation(ctx, exports, "after-cycle-start")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishArchive(ctx, retention.ArchivePublication{OperationID: claim.OperationID, SegmentID: installed.SegmentID, FileName: installed.Path,
		FileSHA256: installed.FileSHA256, MembershipSHA256: installed.MembershipSHA256, StoredBytes: installed.StoredBytes,
		OriginalBytes: installed.OriginalBytes, Exports: exports, EvaluatedAt: now, PolicyRevision: policy.Revision}); err != nil {
		t.Fatal(err)
	}
	eligible, err := store.EligibleDeletionSegmentsForCycle(ctx, policy, now, cycleID)
	if err != nil {
		t.Fatal(err)
	}
	if len(eligible) != 1 || eligible[0].ID == installed.SegmentID {
		t.Fatalf("deletion cohort=%v includes segment published after cycle start", eligible)
	}
}

func TestPeriodRestorePublishesReplacementForUnselectedMembers(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "partial.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	store.retentionNow = func() time.Time { return now }
	firstDay := now.Add(-10 * retention.Day).Truncate(retention.Day)
	received := []time.Time{firstDay, firstDay.Add(time.Hour), firstDay.Add(retention.Day), firstDay.Add(retention.Day + time.Hour)}
	for _, receivedAt := range received {
		accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, receivedAt, []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
		if err := store.CommitExport(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := service.Segments(ctx, "", 100)
	if err != nil || len(segments) != 2 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	scope, err := retention.PeriodScope(firstDay.Add(30*time.Minute), firstDay.Add(retention.Day+30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Restore(ctx, scope, mustHold(t, 5)); err != nil {
		t.Fatal(err)
	}
	rows, err := store.db.Query(`SELECT id, state, hold_until FROM retained_exports ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	states := map[int64]string{}
	holds := map[int64]sql.NullString{}
	for rows.Next() {
		var id int64
		var state string
		var hold sql.NullString
		if err := rows.Scan(&id, &state, &hold); err != nil {
			t.Fatal(err)
		}
		states[id] = state
		holds[id] = hold
	}
	wantHold := formatTime(now.Add(5 * retention.Day))
	if states[1] != "archived" || states[2] != "active" || states[3] != "active" || states[4] != "archived" || holds[2].String != wantHold || holds[3].String != wantHold || holds[1].Valid || holds[4].Valid {
		t.Fatalf("states=%v holds=%v want selected hold=%s", states, holds, wantHold)
	}
	var superseded int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM archive_segments WHERE reference_state = 'superseded'`).Scan(&superseded); err != nil {
		t.Fatal(err)
	}
	var currentSegments, memberships int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM archive_segments WHERE reference_state = 'current'`).Scan(&currentSegments); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM current_archive_memberships WHERE export_id IN (1, 4)`).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if superseded != 2 || currentSegments != 2 || memberships != 2 {
		t.Fatalf("superseded=%d current segments=%d memberships=%d", superseded, currentSegments, memberships)
	}
	if err := service.Restore(ctx, scope, mustHold(t, 10)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{2, 3} {
		var hold string
		if err := store.db.QueryRow(`SELECT hold_until FROM retained_exports WHERE id = ?`, id).Scan(&hold); err != nil {
			t.Fatal(err)
		}
		if hold != wantHold {
			t.Fatalf("repeat restore changed export %d hold to %s, want %s", id, hold, wantHold)
		}
	}
	assertForeignKeys(t, store.db)
}

func TestRestoreHoldStartsAtPublisherCommitBoundaryAfterGateWait(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "restore-publication-clock.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-10*retention.Day), []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
	if err := store.CommitExport(ctx, accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	archives := archivefs.New(store.ArchiveDirectory())
	service := retention.NewServiceWithReplayer(store, archives, retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := store.ListArchiveSegments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	claim, err := store.ClaimRestore(ctx, mustSegmentScope(t, segments[0].ID), now)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := archives.OpenVerified(ctx, segments[0].ID)
	if err != nil || len(verified.Exports) != 1 {
		t.Fatalf("verified=%v err=%v", verified.Exports, err)
	}
	exported := verified.Exports[0]
	replayed, err := otel.ReplayExport(canonical.Signal(exported.Signal), ingest.Transport(exported.Transport), exported.ReceivedAt, exported.Protobuf, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	replayed.Identity = ingest.RetainedIdentity{ID: exported.ID, PayloadOccurrence: exported.PayloadOccurrence}
	publicationAt := now
	store.retentionNow = func() time.Time { return publicationAt }
	if err := store.lockWrite(ctx); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		done <- store.PublishRestore(ctx, retention.RestorePublication{OperationID: claim.OperationID, Selected: []ingest.AcceptedExport{replayed}, OriginalSegments: []string{segments[0].ID}, Hold: mustHold(t, 7)})
	}()
	<-started
	publicationAt = now.Add(6 * time.Hour)
	store.unlockWrite()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var holdUntil string
	if err := store.db.QueryRow(`SELECT hold_until FROM retained_exports WHERE id = 1`).Scan(&holdUntil); err != nil {
		t.Fatal(err)
	}
	if want := formatTime(publicationAt.Add(7 * retention.Day)); holdUntil != want {
		t.Fatalf("hold_until=%s want publication-boundary hold=%s", holdUntil, want)
	}
}

func TestRestoreScopeClassifiesMixedStatesAndRestoringConflict(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "restore-classification.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	received := []time.Time{now, now.Add(-10 * retention.Day), now.Add(-11 * retention.Day), now.Add(-40 * retention.Day)}
	for _, receivedAt := range received {
		accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, receivedAt, []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
		if err := store.CommitExport(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	restoringScope, err := retention.PeriodScope(received[2].Add(-time.Minute), received[2].Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	claim, err := store.ClaimRestore(ctx, restoringScope, now)
	if err != nil || claim.OperationID == "" {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	broadScope, err := retention.PeriodScope(now.Add(-50*retention.Day), now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClassifyRestoreScope(ctx, broadScope); !errors.Is(err, retention.ErrRestoreConflict) {
		t.Fatalf("restoring period classification error=%v", err)
	}
	if err := store.FailRestore(ctx, claim.OperationID, errors.New("release classification fixture"), now); err != nil {
		t.Fatal(err)
	}
	classification, err := store.ClassifyRestoreScope(ctx, broadScope)
	if err != nil || classification.Result != "current" || classification.ActiveMatches != 1 || classification.ArchivedMatches != 2 || classification.DeletedMatches != 1 {
		t.Fatalf("mixed classification=%+v err=%v", classification, err)
	}
}

func TestSegmentReferenceLookupResolvesTransitiveReplacementsAndRestoredTerminal(t *testing.T) {
	ctx := context.Background()
	store, err := open(filepath.Join(t.TempDir(), "replacement-chain.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	store.retentionNow = func() time.Time { return now }
	first := now.Add(-10 * retention.Day).Truncate(retention.Day)
	for index := range 4 {
		accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, first.Add(time.Duration(index)*time.Hour), []byte{0x0a, 0x00}), Journal: ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"}, NormalizationError: "fixture"}
		if err := store.CommitExport(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.UpdateRetentionPolicy(ctx, true, 1, 30, now); err != nil {
		t.Fatal(err)
	}
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	if err := service.RunMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	segments, _, err := service.Segments(ctx, "", 10)
	if err != nil || len(segments) != 1 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	originalID := segments[0].ID
	firstScope, err := retention.PeriodScope(first, first.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Restore(ctx, firstScope, mustHold(t, 1)); err != nil {
		t.Fatal(err)
	}
	firstLookup, err := store.ClassifyRestoreScope(ctx, mustSegmentScope(t, originalID))
	if err != nil || firstLookup.Result != "superseded" || len(firstLookup.CurrentSegmentIDs) != 1 {
		t.Fatalf("first lookup=%+v err=%v", firstLookup, err)
	}
	firstReplacementID := firstLookup.CurrentSegmentIDs[0]
	secondScope, err := retention.PeriodScope(first.Add(time.Hour), first.Add(time.Hour+30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Restore(ctx, secondScope, mustHold(t, 1)); err != nil {
		t.Fatal(err)
	}
	transitive, err := store.ClassifyRestoreScope(ctx, mustSegmentScope(t, originalID))
	if err != nil || transitive.Result != "superseded" || len(transitive.CurrentSegmentIDs) != 1 || transitive.CurrentSegmentIDs[0] == firstReplacementID {
		t.Fatalf("transitive lookup=%+v err=%v", transitive, err)
	}
	currentID := transitive.CurrentSegmentIDs[0]
	middle, err := store.ClassifyRestoreScope(ctx, mustSegmentScope(t, firstReplacementID))
	if err != nil || len(middle.CurrentSegmentIDs) != 1 || middle.CurrentSegmentIDs[0] != currentID {
		t.Fatalf("middle lookup=%+v err=%v", middle, err)
	}
	if err := service.Restore(ctx, mustSegmentScope(t, currentID), mustHold(t, 1)); err != nil {
		t.Fatal(err)
	}
	restored, err := store.ClassifyRestoreScope(ctx, mustSegmentScope(t, currentID))
	if err != nil || restored.Result != "restored" || len(restored.CurrentSegmentIDs) != 0 {
		t.Fatalf("restored lookup=%+v err=%v", restored, err)
	}
	assertForeignKeys(t, store.db)
}

func TestTerminalOperationHistoryIsPrunedToNewestHundred(t *testing.T) {
	store, err := open(filepath.Join(t.TempDir(), "prune.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := range 101 {
		if _, err := transaction.Exec(`INSERT INTO retention_operations (id, kind, status, phase, requested_at, completed_at) VALUES (?, 'archive', 'completed', 'terminal', ?, ?)`, fmt.Sprintf("op-%03d", index), formatTime(time.Unix(int64(index), 0)), formatTime(time.Unix(int64(index), 0))); err != nil {
			t.Fatal(err)
		}
	}
	if err := pruneTerminalRetentionOperations(ctx, transaction); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM retention_operations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 100 {
		t.Fatalf("terminal operation count=%d", count)
	}
	var oldest string
	if err := store.db.QueryRow(`SELECT id FROM retention_operations ORDER BY completed_at LIMIT 1`).Scan(&oldest); err != nil {
		t.Fatal(err)
	}
	if oldest != "op-001" {
		t.Fatalf("oldest retained=%s", oldest)
	}
}

func TestRetentionStatusReturnsEveryRunningAndNewestHundredTerminalCycles(t *testing.T) {
	store, err := open(filepath.Join(t.TempDir(), "cycle-history.db"), false, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for index := range 101 {
		instant := formatTime(time.Unix(int64(index), 0))
		if _, err := store.db.Exec(`INSERT INTO retention_cycles (
 id, status, started_at, evaluated_at, cohort_size, cohort_max_export_id, planning_completed, planned_children, completed_at
) VALUES (?, 'completed', ?, ?, 0, 0, 1, 0, ?)`, fmt.Sprintf("terminal-%03d", index), instant, instant, instant); err != nil {
			t.Fatal(err)
		}
	}
	instant := formatTime(time.Unix(200, 0))
	if _, err := store.db.Exec(`INSERT INTO retention_cycles (id, status, started_at, evaluated_at, cohort_size, cohort_max_export_id) VALUES ('running', 'running', ?, ?, 0, 0)`, instant, instant); err != nil {
		t.Fatal(err)
	}
	cycles, err := store.RetentionCycles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cycles) != 101 || cycles[0].ID != "running" || cycles[1].ID != "terminal-100" || cycles[len(cycles)-1].ID != "terminal-001" {
		t.Fatalf("unexpected cycle history bounds: count=%d first=%q newestTerminal=%q oldestTerminal=%q", len(cycles), cycles[0].ID, cycles[1].ID, cycles[len(cycles)-1].ID)
	}
}

func archiveExportForTest(value retention.RawExport) archivefs.Export {
	return archivefs.Export{ID: value.ID, PayloadOccurrence: value.PayloadOccurrence, ReceivedAt: value.ReceivedAt,
		Signal: value.Signal, Transport: value.Transport, Source: value.Source, NormalizerVersion: value.NormalizerVersion,
		NormalizationStatus: value.NormalizationStatus, NormalizationError: value.NormalizationError,
		HarnessState: value.HarnessState, HarnessScope: value.HarnessScope, HarnessFingerprint: value.HarnessFingerprint,
		HarnessLabel: value.HarnessLabel, Protobuf: value.Protobuf}
}

func mustSegmentScope(t *testing.T, id string) retention.RestoreScope {
	t.Helper()
	scope, err := retention.SegmentScope(id)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}
func mustHold(t *testing.T, days int) retention.Hold {
	t.Helper()
	hold, err := retention.NewHold(days)
	if err != nil {
		t.Fatal(err)
	}
	return hold
}

func assertForeignKeys(t *testing.T, database *sql.DB) {
	t.Helper()
	rows, err := database.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowID sql.NullInt64
		var parent string
		var fk int
		if err := rows.Scan(&table, &rowID, &parent, &fk); err != nil {
			t.Fatal(err)
		}
		t.Fatalf("foreign key violation table=%s row=%v parent=%s fk=%d", table, rowID, parent, fk)
	}
}

func restoreTestPayload(codec string, stored []byte, size int, hashText string) ([]byte, error) {
	decoded, err := hex.DecodeString(hashText)
	if err != nil {
		return nil, err
	}
	var hash [sha256.Size]byte
	copy(hash[:], decoded)
	return journal.Restore(journal.Codec(codec), stored, size, hash)
}
