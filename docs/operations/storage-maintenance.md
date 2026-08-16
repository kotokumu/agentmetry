# Storage Maintenance

Agentmetry stores accepted OTLP payloads in a lossless SQLite journal and
derives dashboard projections from that journal. The database is local to the
application profile and must be treated as sensitive because telemetry can
contain prompts, responses, tool details, file paths, and command output.

## Automatic migration

On startup, Agentmetry checks the database storage generation before accepting
new telemetry. Older databases are migrated while the dashboard displays
maintenance progress. The migration rebuilds current projections from the
journal, verifies restored payload hashes and derived row counts, and preserves
the original database if validation fails.

Fresh installs and databases already at the current generation skip replay.

## Manual compaction

Quit every Agentmetry process that uses the target database, then run:

```sh
agentmetry -compact-database -database /absolute/path/to/agentmetry.db
```

The command streams the journal into a sibling candidate database, regenerates
current semantic projections, verifies every restored payload and SHA-256 hash
plus derived row counts, and replaces the source only after validation passes.
Do not interrupt the process intentionally during replacement.

## Interrupted replacement

A durable manifest records the replacement phase. On the next launch,
Agentmetry examines the source, candidate, and backup files and completes or
rolls back the replacement according to the last verified phase. Do not rename
or delete these sibling files before recovery has run.

## Backup and restore

Stop Agentmetry before copying a database and its SQLite sidecar files. Keep the
database and any sibling migration manifest together. Restore them only to a
trusted local path, then start the current Agentmetry release and allow its
migration gate to complete before sending telemetry.

Direct SQL writes are unsupported. The physical schema is internal and may
change between releases; use the product APIs and documented import/export
surfaces for integrations.
