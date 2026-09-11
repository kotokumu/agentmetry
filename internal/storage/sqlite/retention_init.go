package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

func initializeRetentionCatalog(ctx context.Context, database *sql.DB) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin retention catalog initialization: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO retention_policy
  (id, enabled, archive_days, delete_days, revision, updated_at)
VALUES (1, 0, NULL, NULL, 0, '1970-01-01T00:00:00Z')`); err != nil {
		return fmt.Errorf("initialize retention policy: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO retained_exports (id, received_at, state)
SELECT id, received_at, 'active' FROM otlp_exports`); err != nil {
		return fmt.Errorf("initialize retained export identities: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit retention catalog initialization: %w", err)
	}
	return nil
}
