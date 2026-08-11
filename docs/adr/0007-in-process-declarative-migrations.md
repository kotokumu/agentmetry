# ADR 0007: In-Process Declarative SQLite Migrations

- Status: Accepted
- Date: 2026-08-11

## Context

Each Agentmetry installation owns a local SQLite database. Users must not install or invoke a migration CLI, and desktop, standalone, and container distributions must keep the same single Go server binary. The application therefore needs to converge an existing database to its desired schema during startup.

## Decision

Agentmetry embeds an Atlas HCL schema and uses the Atlas Go SQLite driver in-process:

1. Inspect the current `main` schema through the existing `modernc.org/sqlite` `*sql.DB`.
2. Evaluate the embedded HCL into the desired schema.
3. Compute the Atlas schema diff.
4. Reject changes outside the automatic safety policy.
5. Apply the accepted changes in a transaction before opening network listeners.
6. Inspect again and require an empty diff.

There is no Atlas executable, child process, runtime download, or CGO dependency.

## Automatic Safety Policy

The startup migrator automatically permits:

- adding a table;
- adding an index;
- adding a nullable column; and
- adding a non-null column only when it has a database default.

It rejects table or column removal, rename, type changes, constraint changes, foreign-key changes, and any change that requires a SQLite table rebuild. Rejected changes stop startup with a diagnostic that identifies the unsafe change.

Data transformations and intentional destructive changes require a separately reviewed, embedded transition. They are not inferred from desired state because a declarative diff cannot reliably distinguish a rename from deletion plus addition.

## Responsibilities

| Component | Responsibility |
|---|---|
| Embedded HCL | The complete desired SQLite schema |
| Atlas SQLite driver | Inspect, diff, plan, and apply schema changes |
| Safety policy | Decide whether a computed change may run unattended |
| Store startup | Configure SQLite, converge the schema, then accept traffic |
| Explicit transition | Perform reviewed data transformations not expressible as a safe additive diff |

## Test Specification

1. A new database converges to the complete desired schema.
2. An older database receives additive tables, indexes, and columns while retaining its rows.
3. Opening an already-current database is idempotent.
4. A destructive or rebuilding diff is rejected before any schema change is committed.
5. Failure to converge prevents the store and server from opening.
6. The build and test suite work with `CGO_ENABLED=0`.

## Consequences

- Local databases upgrade automatically without a separate operational step.
- The schema is reviewable as one desired-state artifact instead of being scattered across imperative `CREATE IF NOT EXISTS` statements.
- Safe automatic evolution is intentionally narrower than everything SQLite or Atlas can execute.
- Complex migrations remain explicit, reviewable application code or embedded SQL.
