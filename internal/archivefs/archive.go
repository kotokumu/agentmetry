// Package archivefs owns the durable, raw-only representation of archived
// telemetry exports. A segment is one zstd stream containing a deterministic
// tar archive, so compression can share dictionaries across many exports.
package archivefs

import (
	"archive/tar"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/kotokumu/agentmetry/internal/journal"
	"github.com/kotokumu/agentmetry/internal/retention"
)

const (
	FormatVersion           = 1
	MaxSegmentOriginalBytes = 256 << 20
)

var ErrInvalidSegment = retention.ErrInvalidArchive
var ErrPayloadCorrupt = retention.ErrArchivePayload
var ErrMetadataUnverifiable = retention.ErrArchiveMetadata
var ErrUnsupportedFormat = retention.ErrArchiveFormat

type Export = retention.RawExport

type Member struct {
	ID                  int64  `json:"id"`
	PayloadOccurrence   int64  `json:"payload_occurrence"`
	ReceivedAt          string `json:"received_at"`
	Signal              string `json:"signal"`
	Transport           string `json:"transport"`
	Source              string `json:"source"`
	NormalizerVersion   int    `json:"normalizer_version"`
	NormalizationStatus string `json:"normalization_status"`
	NormalizationError  string `json:"normalization_error,omitempty"`
	HarnessState        string `json:"harness_state"`
	HarnessScope        string `json:"harness_scope,omitempty"`
	HarnessFingerprint  string `json:"harness_fingerprint,omitempty"`
	HarnessLabel        string `json:"harness_label,omitempty"`
	Entry               string `json:"entry"`
	Size                int64  `json:"size"`
	SHA256              string `json:"sha256"`
}

type Manifest struct {
	Version          int      `json:"version"`
	SegmentID        string   `json:"segment_id"`
	Incarnation      string   `json:"incarnation,omitempty"`
	MembershipSHA256 string   `json:"membership_sha256"`
	Members          []Member `json:"members"`
}

type Installed = retention.ArchiveInstall

type Store struct{ directory string }

type DeletionPresence = retention.DeletionPresence

const (
	DeletionInstalled = retention.DeletionInstalled
	DeletionStaged    = retention.DeletionStaged
	DeletionMissing   = retention.DeletionMissing
)

type DeletionFile struct {
	installed string
	staged    string
	syncDir   func(string) error
}

type DeletionHandle = retention.DeletionHandle

func New(directory string) *Store { return &Store{directory: directory} }

func (store *Store) Directory() string { return store.directory }

func (store *Store) DeletionFile(segmentID string) (retention.DeletionHandle, error) {
	if !validSegmentID(segmentID) {
		return nil, fmt.Errorf("%w: invalid segment identity", ErrInvalidSegment)
	}
	installed := filepath.Join(store.directory, segmentID+".tar.zst")
	return &DeletionFile{installed: installed, staged: installed + ".deleting", syncDir: syncDirectory}, nil
}

func (file *DeletionFile) StageAndSync() error {
	presence, err := file.Inspect()
	if err != nil {
		return err
	}
	if presence == DeletionStaged {
		return nil
	}
	if presence == DeletionMissing {
		return os.ErrNotExist
	}
	if err := os.Rename(file.installed, file.staged); err != nil {
		return fmt.Errorf("stage archive deletion: %w", err)
	}
	return file.syncDir(filepath.Dir(file.installed))
}

func (file *DeletionFile) RemoveAndSync() error {
	if err := os.Remove(file.staged); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove staged archive segment: %w", err)
	}
	return file.syncDir(filepath.Dir(file.installed))
}

func (file *DeletionFile) RestoreAndSync() error {
	presence, err := file.Inspect()
	if err != nil {
		return err
	}
	if presence == DeletionInstalled {
		return nil
	}
	if presence == DeletionMissing {
		return os.ErrNotExist
	}
	if err := os.Rename(file.staged, file.installed); err != nil {
		return fmt.Errorf("restore staged archive segment: %w", err)
	}
	return file.syncDir(filepath.Dir(file.installed))
}

func (file *DeletionFile) Inspect() (DeletionPresence, error) {
	installed, installedErr := os.Stat(file.installed)
	staged, stagedErr := os.Stat(file.staged)
	if installedErr != nil && !errors.Is(installedErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect installed archive segment: %w", installedErr)
	}
	if stagedErr != nil && !errors.Is(stagedErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect staged archive segment: %w", stagedErr)
	}
	if installedErr == nil && stagedErr == nil {
		return "", fmt.Errorf("%w: installed and staged archive files both exist", ErrInvalidSegment)
	}
	if installedErr == nil && installed.Mode().IsRegular() {
		return DeletionInstalled, nil
	}
	if stagedErr == nil && staged.Mode().IsRegular() {
		return DeletionStaged, nil
	}
	return DeletionMissing, nil
}

func (store *Store) Build(ctx context.Context, exports []Export) (Installed, error) {
	return store.BuildWithIncarnation(ctx, exports, "")
}

// BuildWithIncarnation keeps content verification deterministic while giving
// each lifecycle publication its own immutable historical segment identity.
// Retrying the same operation with the same incarnation produces identical
// bytes; a later re-archive of the same exports cannot reactivate a terminal
// segment reference.
func (store *Store) BuildWithIncarnation(ctx context.Context, exports []Export, incarnation string) (Installed, error) {
	if len(exports) == 0 {
		return Installed{}, fmt.Errorf("%w: segment has no exports", ErrInvalidSegment)
	}
	exports = append([]Export(nil), exports...)
	sort.SliceStable(exports, func(i, j int) bool {
		if exports[i].ReceivedAt.Equal(exports[j].ReceivedAt) {
			return exports[i].ID < exports[j].ID
		}
		return exports[i].ReceivedAt.Before(exports[j].ReceivedAt)
	})
	manifest, originalBytes, err := makeManifest(exports, incarnation)
	if err != nil {
		return Installed{}, err
	}
	if err := os.MkdirAll(store.directory, 0o700); err != nil {
		return Installed{}, fmt.Errorf("create archive directory: %w", err)
	}
	if err := os.Chmod(store.directory, 0o700); err != nil {
		return Installed{}, fmt.Errorf("protect archive directory: %w", err)
	}
	candidate, err := os.CreateTemp(store.directory, ".segment-*.candidate")
	if err != nil {
		return Installed{}, fmt.Errorf("create archive candidate: %w", err)
	}
	candidatePath := candidate.Name()
	keep := false
	defer func() {
		_ = candidate.Close()
		if !keep {
			_ = os.Remove(candidatePath)
		}
	}()
	if err := candidate.Chmod(0o600); err != nil {
		return Installed{}, fmt.Errorf("protect archive candidate: %w", err)
	}
	encoder, err := zstd.NewWriter(candidate, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	if err != nil {
		return Installed{}, fmt.Errorf("create archive compressor: %w", err)
	}
	tarWriter := tar.NewWriter(encoder)
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return Installed{}, fmt.Errorf("encode archive manifest: %w", err)
	}
	if err := writeEntry(ctx, tarWriter, "manifest.json", manifestJSON); err != nil {
		return Installed{}, err
	}
	for index, exported := range exports {
		if err := writeEntry(ctx, tarWriter, manifest.Members[index].Entry, exported.Protobuf); err != nil {
			return Installed{}, err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return Installed{}, fmt.Errorf("finish archive tar: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return Installed{}, fmt.Errorf("finish archive compression: %w", err)
	}
	if err := candidate.Sync(); err != nil {
		return Installed{}, fmt.Errorf("sync archive candidate: %w", err)
	}
	if err := candidate.Close(); err != nil {
		return Installed{}, fmt.Errorf("close archive candidate: %w", err)
	}
	verification, err := verifyFile(ctx, candidatePath)
	if err != nil {
		return Installed{}, err
	}
	if verification.Manifest.SegmentID != manifest.SegmentID || verification.Manifest.MembershipSHA256 != manifest.MembershipSHA256 {
		return Installed{}, fmt.Errorf("%w: candidate identity changed during verification", ErrInvalidSegment)
	}
	finalPath := filepath.Join(store.directory, manifest.SegmentID+".tar.zst")
	if _, err := os.Stat(finalPath); err == nil {
		return Installed{}, fmt.Errorf("%w: segment %s already exists", ErrInvalidSegment, manifest.SegmentID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Installed{}, fmt.Errorf("inspect archive destination: %w", err)
	}
	if err := os.Rename(candidatePath, finalPath); err != nil {
		return Installed{}, fmt.Errorf("install archive segment: %w", err)
	}
	keep = true
	if err := syncDirectory(store.directory); err != nil {
		return Installed{}, err
	}
	return Installed{SegmentID: manifest.SegmentID, Path: finalPath, FileSHA256: verification.FileSHA256,
		MembershipSHA256: manifest.MembershipSHA256, StoredBytes: verification.StoredBytes,
		OriginalBytes: originalBytes, ExportCount: len(exports)}, nil
}

func writeEntry(ctx context.Context, writer *tar.Writer, name string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	header := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(content)), ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR}
	if err := writer.WriteHeader(header); err != nil {
		return fmt.Errorf("write archive entry %s: %w", name, err)
	}
	if _, err := writer.Write(content); err != nil {
		return fmt.Errorf("write archive entry payload %s: %w", name, err)
	}
	return nil
}

func makeManifest(exports []Export, incarnation string) (Manifest, int64, error) {
	members := make([]Member, len(exports))
	seen := make(map[int64]struct{}, len(exports))
	var originalBytes int64
	receiveDate := exports[0].ReceivedAt.UTC().Format("2006-01-02")
	for index, exported := range exports {
		if exported.ID <= 0 || len(exported.Protobuf) > journal.MaxPayloadBytes {
			return Manifest{}, 0, fmt.Errorf("%w: export %d has invalid identity or payload size", ErrInvalidSegment, exported.ID)
		}
		if exported.ReceivedAt.UTC().Format("2006-01-02") != receiveDate {
			return Manifest{}, 0, fmt.Errorf("%w: exports cross UTC receive dates", ErrInvalidSegment)
		}
		if _, duplicate := seen[exported.ID]; duplicate {
			return Manifest{}, 0, fmt.Errorf("%w: duplicate export %d", ErrInvalidSegment, exported.ID)
		}
		seen[exported.ID] = struct{}{}
		hash := sha256.Sum256(exported.Protobuf)
		members[index] = Member{ID: exported.ID, PayloadOccurrence: exported.PayloadOccurrence,
			ReceivedAt: exported.ReceivedAt.UTC().Format(time.RFC3339Nano), Signal: exported.Signal,
			Transport: exported.Transport, Source: exported.Source, NormalizerVersion: exported.NormalizerVersion,
			NormalizationStatus: exported.NormalizationStatus, NormalizationError: exported.NormalizationError,
			HarnessState: exported.HarnessState, HarnessScope: exported.HarnessScope,
			HarnessFingerprint: exported.HarnessFingerprint, HarnessLabel: exported.HarnessLabel,
			Entry: "exports/" + strconv.FormatInt(exported.ID, 10) + ".pb", Size: int64(len(exported.Protobuf)), SHA256: hex.EncodeToString(hash[:])}
		originalBytes += int64(len(exported.Protobuf))
		if originalBytes > MaxSegmentOriginalBytes {
			return Manifest{}, 0, fmt.Errorf("%w: original bytes exceed %d", ErrInvalidSegment, MaxSegmentOriginalBytes)
		}
	}
	membershipJSON, err := json.Marshal(members)
	if err != nil {
		return Manifest{}, 0, fmt.Errorf("encode archive membership: %w", err)
	}
	membershipHash := sha256.Sum256(membershipJSON)
	segmentHash := segmentIdentity(incarnation, membershipJSON)
	return Manifest{Version: FormatVersion, SegmentID: hex.EncodeToString(segmentHash[:]),
		Incarnation: incarnation, MembershipSHA256: hex.EncodeToString(membershipHash[:]), Members: members}, originalBytes, nil
}

func segmentIdentity(incarnation string, membershipJSON []byte) [sha256.Size]byte {
	identity := []byte("agentmetry-archive-v1\n")
	if incarnation != "" {
		identity = append(identity, []byte("incarnation="+incarnation+"\n")...)
	}
	return sha256.Sum256(append(identity, membershipJSON...))
}

type verifiedFile struct {
	Manifest    Manifest
	Exports     []Export
	FileSHA256  string
	StoredBytes int64
}

func (store *Store) OpenVerified(ctx context.Context, segmentID string) (retention.VerifiedArchive, error) {
	if !validSegmentID(segmentID) {
		return retention.VerifiedArchive{}, fmt.Errorf("%w: invalid segment identity", ErrInvalidSegment)
	}
	verified, err := verifyFile(ctx, filepath.Join(store.directory, segmentID+".tar.zst"))
	if err != nil {
		return retention.VerifiedArchive{}, err
	}
	if verified.Manifest.SegmentID != segmentID {
		return retention.VerifiedArchive{}, fmt.Errorf("%w: file name and manifest identity differ", ErrMetadataUnverifiable)
	}
	members := make([]retention.VerifiedArchiveMember, len(verified.Manifest.Members))
	for index, member := range verified.Manifest.Members {
		members[index] = retention.VerifiedArchiveMember{ID: member.ID, ReceivedAt: member.ReceivedAt}
	}
	return retention.VerifiedArchive{MembershipSHA256: verified.Manifest.MembershipSHA256, Members: members,
		Exports: verified.Exports, FileSHA256: verified.FileSHA256, StoredBytes: verified.StoredBytes}, nil
}

// Remove deletes an installed segment and durably records the directory
// update. Lifecycle authority must already have moved away from the file.
func (store *Store) Remove(segmentID string) error {
	if !validSegmentID(segmentID) {
		return fmt.Errorf("%w: invalid segment identity", ErrInvalidSegment)
	}
	if err := os.Remove(filepath.Join(store.directory, segmentID+".tar.zst")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove archive segment: %w", err)
	}
	return syncDirectory(store.directory)
}

func verifyFile(ctx context.Context, path string) (verifiedFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return verifiedFile{}, fmt.Errorf("%w: open archive segment: %v", ErrPayloadCorrupt, err)
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return verifiedFile{}, fmt.Errorf("stat archive segment: %w", err)
	}
	hasher := sha256.New()
	reader := bufio.NewReader(io.TeeReader(file, hasher))
	decoder, err := zstd.NewReader(reader, zstd.WithDecoderMaxMemory(64<<20))
	if err != nil {
		return verifiedFile{}, fmt.Errorf("%w: open zstd stream: %v", ErrPayloadCorrupt, err)
	}
	defer decoder.Close()
	tarReader := tar.NewReader(decoder)
	header, err := tarReader.Next()
	if err != nil {
		return verifiedFile{}, fmt.Errorf("%w: unreadable manifest", errors.Join(ErrPayloadCorrupt, ErrMetadataUnverifiable))
	}
	if header.Name != "manifest.json" || header.Size <= 0 || header.Size > 4<<20 {
		return verifiedFile{}, fmt.Errorf("%w: missing or oversized manifest", ErrMetadataUnverifiable)
	}
	manifestBytes, err := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
	if err != nil || int64(len(manifestBytes)) != header.Size {
		return verifiedFile{}, fmt.Errorf("%w: incomplete manifest", errors.Join(ErrPayloadCorrupt, ErrMetadataUnverifiable))
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil || !validSegmentID(manifest.SegmentID) {
		return verifiedFile{}, fmt.Errorf("%w: malformed manifest", ErrMetadataUnverifiable)
	}
	if manifest.Version != FormatVersion {
		return verifiedFile{}, fmt.Errorf("%w: version %d", ErrUnsupportedFormat, manifest.Version)
	}
	membershipJSON, _ := json.Marshal(manifest.Members)
	membershipHash := sha256.Sum256(membershipJSON)
	if hex.EncodeToString(membershipHash[:]) != manifest.MembershipSHA256 {
		return verifiedFile{}, fmt.Errorf("%w: membership digest mismatch", ErrMetadataUnverifiable)
	}
	segmentHash := segmentIdentity(manifest.Incarnation, membershipJSON)
	if hex.EncodeToString(segmentHash[:]) != manifest.SegmentID {
		return verifiedFile{}, fmt.Errorf("%w: segment identity mismatch", ErrMetadataUnverifiable)
	}
	exports := make([]Export, len(manifest.Members))
	var totalOriginal int64
	for index, member := range manifest.Members {
		if member.Size < 0 || member.Size > journal.MaxPayloadBytes || member.Entry != "exports/"+strconv.FormatInt(member.ID, 10)+".pb" {
			return verifiedFile{}, fmt.Errorf("%w: invalid member framing", ErrMetadataUnverifiable)
		}
		totalOriginal += member.Size
		if totalOriginal > MaxSegmentOriginalBytes {
			return verifiedFile{}, fmt.Errorf("%w: segment exceeds original-byte bound", ErrMetadataUnverifiable)
		}
		header, err := tarReader.Next()
		if err != nil || header.Name != member.Entry || header.Size != member.Size {
			return verifiedFile{}, fmt.Errorf("%w: member order or length mismatch", ErrMetadataUnverifiable)
		}
		payload, err := io.ReadAll(io.LimitReader(tarReader, member.Size+1))
		if err != nil || int64(len(payload)) != member.Size {
			return verifiedFile{}, fmt.Errorf("%w: incomplete member %d", ErrPayloadCorrupt, member.ID)
		}
		hash := sha256.Sum256(payload)
		if hex.EncodeToString(hash[:]) != member.SHA256 {
			return verifiedFile{}, fmt.Errorf("%w: member %d digest mismatch", ErrPayloadCorrupt, member.ID)
		}
		receivedAt, err := time.Parse(time.RFC3339Nano, member.ReceivedAt)
		if err != nil {
			return verifiedFile{}, fmt.Errorf("%w: member %d receive time", ErrMetadataUnverifiable, member.ID)
		}
		exports[index] = Export{ID: member.ID, PayloadOccurrence: member.PayloadOccurrence, ReceivedAt: receivedAt,
			Signal: member.Signal, Transport: member.Transport, Source: member.Source,
			NormalizerVersion: member.NormalizerVersion, NormalizationStatus: member.NormalizationStatus,
			NormalizationError: member.NormalizationError, HarnessState: member.HarnessState,
			HarnessScope: member.HarnessScope, HarnessFingerprint: member.HarnessFingerprint,
			HarnessLabel: member.HarnessLabel, Protobuf: payload}
	}
	if trailing, err := tarReader.Next(); err != io.EOF || trailing != nil {
		return verifiedFile{}, fmt.Errorf("%w: unexpected trailing archive entry", ErrMetadataUnverifiable)
	}
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return verifiedFile{}, fmt.Errorf("%w: finish file digest: %v", ErrPayloadCorrupt, err)
	}
	return verifiedFile{Manifest: manifest, Exports: exports, FileSHA256: hex.EncodeToString(hasher.Sum(nil)), StoredBytes: stat.Size()}, nil
}

func validSegmentID(value string) bool {
	if len(value) != sha256.Size*2 || strings.ContainsAny(value, `/\\`) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open archive directory for sync: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync archive directory: %w", err)
	}
	return nil
}
