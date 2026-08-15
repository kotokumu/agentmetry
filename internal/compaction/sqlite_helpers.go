package compaction

import (
	"context"
	"database/sql"
	"fmt"
	"os"
)

func copyPlanUsageSnapshots(ctx context.Context, source *sql.Tx, candidatePath string) (int64, error) {
	exists, err := columnExistsTx(ctx, source, "plan_usage_snapshots", "raw_json")
	if err != nil || !exists {
		return 0, err
	}
	rows, err := source.QueryContext(ctx, `SELECT source, account_id, plan, window_id,
window_duration_minutes, used_percent, resets_at, captured_at, authority, raw_json
FROM plan_usage_snapshots ORDER BY id`)
	if err != nil {
		return 0, fmt.Errorf("read legacy plan usage: %w", err)
	}
	defer rows.Close()
	destination, err := sql.Open("sqlite", candidatePath)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	transaction, err := destination.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer transaction.Rollback()
	var count int64
	for rows.Next() {
		var sourceID, accountID, plan, windowID, capturedAt, authority, rawJSON string
		var resetsAt sql.NullString
		var duration int
		var used float64
		if err := rows.Scan(&sourceID, &accountID, &plan, &windowID, &duration, &used, &resetsAt, &capturedAt, &authority, &rawJSON); err != nil {
			return 0, err
		}
		result, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO plan_usage_snapshots (
source, account_id, plan, window_id, window_duration_minutes, used_percent,
resets_at, captured_at, authority, raw_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			sourceID, accountID, plan, windowID, duration, used, resetsAt, capturedAt, authority, rawJSON)
		if err != nil {
			return 0, fmt.Errorf("copy plan usage snapshot: %w", err)
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		count += inserted
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := transaction.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}

func removeDatabaseFamily(path string) error {
	for _, member := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(member); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func removeTemporaryDatabaseFamily(path string) error {
	if err := removeDatabaseFamily(path); err != nil {
		return err
	}
	if err := os.Remove(path + ".lock"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func requireCleanDatabaseFamily(path string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		member := path + suffix
		info, err := os.Stat(member)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Size() != 0 {
			return fmt.Errorf("database sidecar %s still contains %d bytes", member, info.Size())
		}
		if err := os.Remove(member); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove empty database sidecar %s: %w", member, err)
		}
	}
	return nil
}

func tableExists(ctx context.Context, database *sql.DB, table string) (bool, error) {
	var exists int
	err := database.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)", table).Scan(&exists)
	return exists != 0, err
}

func columnExists(ctx context.Context, database *sql.DB, table, column string) (bool, error) {
	rows, err := database.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return scanColumn(rows, column)
}

func columnExistsTx(ctx context.Context, transaction *sql.Tx, table, column string) (bool, error) {
	rows, err := transaction.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return scanColumn(rows, column)
}

func scanColumn(rows *sql.Rows, column string) (bool, error) {
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
