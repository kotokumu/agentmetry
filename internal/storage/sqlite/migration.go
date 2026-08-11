package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

	"ariga.io/atlas/sql/schema"
	atlassqlite "ariga.io/atlas/sql/sqlite"
)

// desiredSchemaHCL is the complete desired state of Agentmetry's local database.
//
//go:embed schema.hcl
var desiredSchemaHCL []byte

// convergeSchema brings the local database to the embedded desired state before
// the store starts accepting telemetry. Only additive changes that are safe for
// unattended startup are allowed.
func convergeSchema(ctx context.Context, database *sql.DB) error {
	driver, err := atlassqlite.Open(database)
	if err != nil {
		return fmt.Errorf("open Atlas SQLite driver: %w", err)
	}

	current, err := driver.InspectSchema(ctx, "main", nil)
	if err != nil {
		return fmt.Errorf("inspect current SQLite schema: %w", err)
	}
	desired, err := evaluateDesiredSchema()
	if err != nil {
		return err
	}
	changes, err := driver.SchemaDiff(current, desired)
	if err != nil {
		return fmt.Errorf("diff SQLite schema: %w", err)
	}
	if len(changes) == 0 {
		return nil
	}
	if err := validateAutomaticChanges(changes); err != nil {
		return err
	}

	transaction, err := atlassqlite.OpenTx(ctx, database, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite schema migration: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	transactionDriver, err := atlassqlite.Open(transaction)
	if err != nil {
		return fmt.Errorf("open transactional Atlas SQLite driver: %w", err)
	}
	if err := transactionDriver.ApplyChanges(ctx, changes); err != nil {
		return fmt.Errorf("apply SQLite schema migration: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit SQLite schema migration: %w", err)
	}

	return verifyConverged(ctx, database, desired)
}

func evaluateDesiredSchema() (*schema.Schema, error) {
	var desired schema.Schema
	if err := atlassqlite.EvalHCLBytes(desiredSchemaHCL, &desired, nil); err != nil {
		return nil, fmt.Errorf("evaluate embedded SQLite schema: %w", err)
	}
	return &desired, nil
}

func verifyConverged(ctx context.Context, database *sql.DB, desired *schema.Schema) error {
	driver, err := atlassqlite.Open(database)
	if err != nil {
		return fmt.Errorf("reopen Atlas SQLite driver: %w", err)
	}
	current, err := driver.InspectSchema(ctx, "main", nil)
	if err != nil {
		return fmt.Errorf("verify current SQLite schema: %w", err)
	}
	remaining, err := driver.SchemaDiff(current, desired)
	if err != nil {
		return fmt.Errorf("verify SQLite schema diff: %w", err)
	}
	if len(remaining) != 0 {
		return fmt.Errorf("SQLite schema did not converge: %d change(s) remain", len(remaining))
	}
	return nil
}

func validateAutomaticChanges(changes []schema.Change) error {
	for _, change := range changes {
		if err := validateAutomaticChange(change); err != nil {
			return fmt.Errorf("unsafe automatic SQLite migration: %w", err)
		}
	}
	return nil
}

func validateAutomaticChange(change schema.Change) error {
	switch change := change.(type) {
	case *schema.AddTable, *schema.AddIndex:
		return nil
	case *schema.ModifyTable:
		for _, nested := range change.Changes {
			switch nested := nested.(type) {
			case *schema.AddIndex:
				continue
			case *schema.AddColumn:
				if nested.C.Type.Null || nested.C.Default != nil {
					continue
				}
				return fmt.Errorf("column %s.%s is NOT NULL and has no default", change.T.Name, nested.C.Name)
			case *schema.ModifyColumn:
				return fmt.Errorf(
					"column %s.%s modification (%d) requires an explicit migration (from type %q to %q)",
					change.T.Name,
					nested.From.Name,
					nested.Change,
					nested.From.Type.Raw,
					nested.To.Type.Raw,
				)
			default:
				return fmt.Errorf("change %T on table %s requires an explicit migration", nested, change.T.Name)
			}
		}
		return nil
	default:
		return fmt.Errorf("change %T requires an explicit migration", change)
	}
}
