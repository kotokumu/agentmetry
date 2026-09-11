package archivefs

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/kotokumu/agentmetry/internal/journal"
)

func TestSegmentRoundTripPreservesRawExportsAndUsesWholeStreamCompression(t *testing.T) {
	store := New(t.TempDir())
	at := time.Date(2026, 9, 10, 1, 2, 3, 4, time.UTC)
	common := bytes.Repeat([]byte(`{"resource":{"attributes":[{"key":"service.name","value":"agentmetry"}]}}`), 4000)
	exports := []Export{
		{ID: 8, PayloadOccurrence: 3, ReceivedAt: at, Signal: "logs", Transport: "grpc", Source: "codex", NormalizerVersion: 7, NormalizationStatus: "projected", HarnessState: "unreported", Protobuf: append(append([]byte(nil), common...), 'a')},
		{ID: 9, PayloadOccurrence: 9, ReceivedAt: at.Add(time.Second), Signal: "logs", Transport: "http/protobuf", Source: "codex", NormalizerVersion: 7, NormalizationStatus: "failed", NormalizationError: "fixture", HarnessState: "captured", HarnessScope: "scope", HarnessFingerprint: "fp", HarnessLabel: "label", Protobuf: append(append([]byte(nil), common...), 'b')},
	}
	installed, err := store.Build(context.Background(), exports)
	if err != nil {
		t.Fatal(err)
	}
	if installed.StoredBytes >= installed.OriginalBytes {
		t.Fatalf("whole-stream archive did not compress: stored=%d original=%d", installed.StoredBytes, installed.OriginalBytes)
	}
	verified, err := store.OpenVerified(context.Background(), installed.SegmentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(verified.Exports) != len(exports) {
		t.Fatalf("got %d exports, want %d", len(verified.Exports), len(exports))
	}
	for index := range exports {
		if !bytes.Equal(verified.Exports[index].Protobuf, exports[index].Protobuf) || verified.Exports[index].ID != exports[index].ID || verified.Exports[index].PayloadOccurrence != exports[index].PayloadOccurrence {
			t.Fatalf("export %d did not round trip", index)
		}
	}
}

func TestWholeSegmentCompressionBeatsIndependentPayloadCompressionForSimilarExports(t *testing.T) {
	store := New(t.TempDir())
	at := time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)
	exports := make([]Export, 200)
	var independentBytes int64
	for index := range exports {
		raw := []byte(fmt.Sprintf(`{"resource":{"service":"codex","host":"local"},"session":"shared-session","event":"response.completed","sequence":%06d,"body":"The common response metadata is repeated across neighboring telemetry exports."}`, index))
		payload, err := journal.Encode(raw)
		if err != nil {
			t.Fatal(err)
		}
		independentBytes += int64(len(payload.Bytes()))
		exports[index] = Export{ID: int64(index + 1), PayloadOccurrence: int64(index + 1), ReceivedAt: at.Add(time.Duration(index) * time.Millisecond), Signal: "logs", Transport: "grpc", Source: "codex", NormalizerVersion: 1, NormalizationStatus: "projected", HarnessState: "unreported", Protobuf: raw}
	}
	installed, err := store.Build(context.Background(), exports)
	if err != nil {
		t.Fatal(err)
	}
	if installed.StoredBytes >= independentBytes {
		t.Fatalf("whole segment=%d bytes, independent payloads=%d bytes", installed.StoredBytes, independentBytes)
	}
}

func TestOpenVerifiedRejectsCorruption(t *testing.T) {
	store := New(t.TempDir())
	installed, err := store.Build(context.Background(), []Export{{ID: 1, PayloadOccurrence: 1, ReceivedAt: time.Now(), Signal: "logs", Transport: "grpc", Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "projected", HarnessState: "unreported", Protobuf: []byte("raw")}})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(installed.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(installed.Path, int64(len(content)-8)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OpenVerified(context.Background(), installed.SegmentID); !errors.Is(err, ErrPayloadCorrupt) {
		t.Fatalf("corrupt segment error=%v, want payload corruption", err)
	}
}

func TestOpenVerifiedClassifiesUnsupportedFormatWithoutDamagingIntegrityAxes(t *testing.T) {
	store := New(t.TempDir())
	exports := []Export{{ID: 1, PayloadOccurrence: 1, ReceivedAt: time.Now().UTC(), Signal: "logs", Transport: "grpc", Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "projected", HarnessState: "unreported", Protobuf: []byte("raw")}}
	manifest, _, err := makeManifest(exports, "")
	if err != nil {
		t.Fatal(err)
	}
	manifest.Version = FormatVersion + 1
	path := filepath.Join(store.Directory(), manifest.SegmentID+".tar.zst")
	writeArchiveFixture(t, path, manifest, exports)
	_, err = store.OpenVerified(context.Background(), manifest.SegmentID)
	if !errors.Is(err, ErrUnsupportedFormat) || errors.Is(err, ErrPayloadCorrupt) || errors.Is(err, ErrMetadataUnverifiable) {
		t.Fatalf("unsupported format classification=%v", err)
	}
}

func TestOpenVerifiedClassifiesMembershipMismatchAsMetadataOnly(t *testing.T) {
	store := New(t.TempDir())
	exports := []Export{{ID: 1, PayloadOccurrence: 1, ReceivedAt: time.Now().UTC(), Signal: "logs", Transport: "grpc", Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "projected", HarnessState: "unreported", Protobuf: []byte("raw")}}
	manifest, _, err := makeManifest(exports, "")
	if err != nil {
		t.Fatal(err)
	}
	manifest.MembershipSHA256 = strings.Repeat("0", 64)
	path := filepath.Join(store.Directory(), manifest.SegmentID+".tar.zst")
	writeArchiveFixture(t, path, manifest, exports)
	_, err = store.OpenVerified(context.Background(), manifest.SegmentID)
	if !errors.Is(err, ErrMetadataUnverifiable) || errors.Is(err, ErrPayloadCorrupt) {
		t.Fatalf("metadata mismatch classification=%v", err)
	}
}

func TestSegmentFramingIsDeterministic(t *testing.T) {
	exports := []Export{{ID: 11, PayloadOccurrence: 7, ReceivedAt: time.Date(2026, 9, 10, 2, 0, 0, 0, time.UTC), Signal: "traces", Transport: "grpc", Source: "unknown", NormalizerVersion: 3, NormalizationStatus: "projected", HarnessState: "unreported", Protobuf: []byte("exact protobuf")}}
	first, err := New(t.TempDir()).Build(context.Background(), exports)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(t.TempDir()).Build(context.Background(), exports)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second.Path)
	if err != nil {
		t.Fatal(err)
	}
	if first.SegmentID != second.SegmentID || first.FileSHA256 != second.FileSHA256 || !bytes.Equal(firstBytes, secondBytes) {
		t.Fatal("same raw exports did not produce deterministic segment bytes")
	}
}

func TestDeletionFileReportsMissingAfterUnlinkWhenDirectorySyncFails(t *testing.T) {
	directory := t.TempDir()
	segmentID := strings.Repeat("a", 64)
	installed := filepath.Join(directory, segmentID+".tar.zst")
	if err := os.WriteFile(installed, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := &DeletionFile{installed: installed, staged: installed + ".deleting", syncDir: syncDirectory}
	if err := file.StageAndSync(); err != nil {
		t.Fatal(err)
	}
	file.syncDir = func(string) error { return errors.New("injected directory sync failure") }
	if err := file.RemoveAndSync(); err == nil {
		t.Fatal("expected the injected sync failure")
	}
	presence, err := file.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if presence != DeletionMissing {
		t.Fatalf("presence=%s, want missing", presence)
	}
}

func TestDeletionFileRefusesToRestoreMissingContent(t *testing.T) {
	directory := t.TempDir()
	segmentID := strings.Repeat("b", 64)
	file := &DeletionFile{installed: filepath.Join(directory, segmentID+".tar.zst"), staged: filepath.Join(directory, segmentID+".tar.zst.deleting"), syncDir: syncDirectory}
	if err := file.RestoreAndSync(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restore missing content error=%v, want os.ErrNotExist", err)
	}
}

func writeArchiveFixture(t *testing.T, path string, manifest Manifest, exports []Export) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(file)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(encoder)
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeEntry(context.Background(), writer, "manifest.json", manifestBytes); err != nil {
		t.Fatal(err)
	}
	for index, member := range manifest.Members {
		if err := writeEntry(context.Background(), writer, member.Entry, exports[index].Protobuf); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
