package compaction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kotokumu/agentmetry/internal/journal"
)

type migrationPhase string

const (
	manifestFormatVersion                = 1
	phaseValidated        migrationPhase = "validated"
	phaseSourcePreserved  migrationPhase = "source-preserved"
	phaseInstalled        migrationPhase = "installed"
	phaseVerified         migrationPhase = "verified"
)

type migrationManifest struct {
	FormatVersion    int            `json:"formatVersion"`
	TargetGeneration int            `json:"targetGeneration"`
	Phase            migrationPhase `json:"phase"`
	Source           string         `json:"source"`
	Candidate        string         `json:"candidate"`
	Backup           string         `json:"backup"`
}

func installValidated(result Result, sourcePath string) error {
	if result.CandidatePath != sourcePath+".compacting" {
		return fmt.Errorf("candidate %q does not belong to source %q", result.CandidatePath, sourcePath)
	}
	manifest := migrationManifest{
		FormatVersion: manifestFormatVersion, TargetGeneration: CurrentStorageGeneration, Phase: phaseValidated,
		Source: sourcePath, Candidate: result.CandidatePath, Backup: sourcePath + ".pre-compaction",
	}
	if _, err := os.Stat(manifest.Backup); err == nil {
		return fmt.Errorf("compaction backup already exists: %s", manifest.Backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := writeManifest(manifest); err != nil {
		return err
	}
	if err := os.Rename(manifest.Source, manifest.Backup); err != nil {
		return recoverAfterError(manifest.Source, fmt.Errorf("preserve legacy database: %w", err))
	}
	if err := syncDirectory(manifest.Source); err != nil {
		return recoverAfterError(manifest.Source, err)
	}
	manifest.Phase = phaseSourcePreserved
	if err := writeManifest(manifest); err != nil {
		return recoverAfterError(manifest.Source, err)
	}
	if err := os.Rename(manifest.Candidate, manifest.Source); err != nil {
		return recoverAfterError(manifest.Source, fmt.Errorf("install compact database: %w", err))
	}
	if err := syncDirectory(manifest.Source); err != nil {
		return recoverAfterError(manifest.Source, err)
	}
	manifest.Phase = phaseInstalled
	if err := writeManifest(manifest); err != nil {
		return recoverAfterError(manifest.Source, err)
	}
	if err := validateInstalledDatabase(manifest.Source, manifest.TargetGeneration); err != nil {
		return recoverAfterError(manifest.Source, err)
	}
	manifest.Phase = phaseVerified
	if err := writeManifest(manifest); err != nil {
		return recoverAfterError(manifest.Source, err)
	}
	return finalizeReplacement(manifest)
}

// recoverOwned resolves every durable manifest state before normal Store.Open can
// create or use the canonical path.
func recoverOwned(sourcePath string) error {
	manifest, err := readManifest(sourcePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if manifest.Source != sourcePath || manifest.Candidate != sourcePath+".compacting" || manifest.Backup != sourcePath+".pre-compaction" {
		return fmt.Errorf("migration manifest paths do not match %s", sourcePath)
	}
	sourceExists := fileExists(manifest.Source)
	backupExists := fileExists(manifest.Backup)
	switch manifest.Phase {
	case phaseValidated:
		if !sourceExists && backupExists {
			if err := os.Rename(manifest.Backup, manifest.Source); err != nil {
				return fmt.Errorf("recover preserved legacy database: %w", err)
			}
		}
		if !fileExists(manifest.Source) {
			return fmt.Errorf("migration recovery found no authoritative database")
		}
		if err := removeTemporaryDatabaseFamily(manifest.Candidate); err != nil {
			return err
		}
		return removeManifest(manifest.Source)
	case phaseSourcePreserved, phaseInstalled, phaseVerified:
		if !sourceExists {
			if !backupExists {
				return fmt.Errorf("migration recovery found neither installed nor legacy database")
			}
			if err := os.Rename(manifest.Backup, manifest.Source); err != nil {
				return fmt.Errorf("restore legacy database: %w", err)
			}
			_ = removeTemporaryDatabaseFamily(manifest.Candidate)
			return removeManifest(manifest.Source)
		}
		if err := validateInstalledDatabase(manifest.Source, manifest.TargetGeneration); err != nil {
			if !backupExists {
				return fmt.Errorf("installed database invalid and no legacy backup remains: %w", err)
			}
			return restoreBackup(manifest, err)
		}
		return finalizeReplacement(manifest)
	default:
		return fmt.Errorf("unknown migration phase %q", manifest.Phase)
	}
}

func validateInstalledDatabase(path string, expectedGeneration int) error {
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return err
	}
	defer database.Close()
	var integrity string
	if err := database.QueryRowContext(context.Background(), "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return fmt.Errorf("installed database integrity check: %q: %w", integrity, err)
	}
	var version int
	if err := database.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&version); err != nil || version != expectedGeneration {
		return fmt.Errorf("installed storage generation %d, want %d: %w", version, expectedGeneration, err)
	}
	rows, err := database.Query(`SELECT payload_protobuf, payload_codec, payload_size, payload_sha256 FROM otlp_exports ORDER BY id`)
	if err != nil {
		return fmt.Errorf("read installed journal: %w", err)
	}
	defer rows.Close()
	ordinal := 0
	for rows.Next() {
		ordinal++
		var stored []byte
		var codecText, hashText string
		var size int
		if err := rows.Scan(&stored, &codecText, &size, &hashText); err != nil {
			return err
		}
		hash, err := parseHash(hashText)
		if err != nil {
			return err
		}
		if _, err := journal.Restore(journal.Codec(codecText), stored, size, hash); err != nil {
			return fmt.Errorf("verify installed journal export %d: %w", ordinal, err)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func finalizeReplacement(manifest migrationManifest) error {
	if fileExists(manifest.Backup) {
		if err := removeDatabaseFamily(manifest.Backup); err != nil {
			return fmt.Errorf("remove validated legacy database: %w", err)
		}
	}
	if err := removeTemporaryDatabaseFamily(manifest.Candidate); err != nil {
		return err
	}
	return removeManifest(manifest.Source)
}

func restoreBackup(manifest migrationManifest, cause error) error {
	failedPath := manifest.Candidate + ".failed"
	_ = removeTemporaryDatabaseFamily(failedPath)
	if err := os.Rename(manifest.Source, failedPath); err != nil {
		return errors.Join(cause, fmt.Errorf("preserve invalid installed database: %w", err))
	}
	if err := os.Rename(manifest.Backup, manifest.Source); err != nil {
		return errors.Join(cause, fmt.Errorf("restore legacy database: %w", err))
	}
	if err := syncDirectory(manifest.Source); err != nil {
		return errors.Join(cause, err)
	}
	_ = removeTemporaryDatabaseFamily(failedPath)
	_ = removeTemporaryDatabaseFamily(manifest.Candidate)
	if err := removeManifest(manifest.Source); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func recoverAfterError(sourcePath string, cause error) error {
	return errors.Join(cause, recoverOwned(sourcePath))
}

func manifestPath(sourcePath string) string { return sourcePath + ".migration.json" }

func writeManifest(manifest migrationManifest) error {
	payload, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	path := manifestPath(manifest.Source)
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create migration manifest: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("install migration manifest: %w", err)
	}
	return syncDirectory(path)
}

func readManifest(sourcePath string) (migrationManifest, error) {
	payload, err := os.ReadFile(manifestPath(sourcePath))
	if err != nil {
		return migrationManifest{}, err
	}
	var manifest migrationManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return migrationManifest{}, fmt.Errorf("decode migration manifest: %w", err)
	}
	if manifest.FormatVersion != manifestFormatVersion {
		return migrationManifest{}, fmt.Errorf("unsupported migration manifest format %d", manifest.FormatVersion)
	}
	if manifest.TargetGeneration > CurrentStorageGeneration {
		return migrationManifest{}, fmt.Errorf("migration target generation %d is newer than supported %d", manifest.TargetGeneration, CurrentStorageGeneration)
	}
	if manifest.TargetGeneration <= 0 {
		return migrationManifest{}, fmt.Errorf("invalid migration target generation %d", manifest.TargetGeneration)
	}
	return manifest, nil
}

func removeManifest(sourcePath string) error {
	for _, path := range []string{manifestPath(sourcePath), manifestPath(sourcePath) + ".tmp"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return syncDirectory(sourcePath)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open database directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync database directory: %w", err)
	}
	return nil
}
