# Agentmetry Documentation

This directory contains the product architecture, operating procedures,
accepted decisions, and implementation designs for Agentmetry.

## Start here

- [Product architecture](architecture.md) — runtime components, boundaries,
  data flow, persistence, and delivery profiles
- [API contract](design/api-proto-contract.md) — protobuf-first read contract
- [Storage maintenance](operations/storage-maintenance.md) — automatic
  migrations, manual compaction, recovery, and backup precautions
- [Contributing](../CONTRIBUTING.md) — local development, tests, and release
  workflow
- [Security policy](../SECURITY.md) — supported versions, local trust boundary,
  and vulnerability reporting

## Architecture decision records

Files under [`adr/`](adr/) record decisions and their original context. An ADR
may retain terminology from the period in which it was written. Its status line
determines whether it is current, proposed, or superseded; the current runtime
topology is always described by [architecture.md](architecture.md).

Key accepted decisions include:

- [Lossless OTLP journal and canonical observations](adr/0006-lossless-otlp-storage.md)
- [In-process SQLite migrations](adr/0007-in-process-declarative-migrations.md)
- [Source profile registry](adr/0008-source-profile-plugins.md)
- [Desktop build architecture](adr/0010-desktop-build-architecture.md)
- [CI build validation](adr/0012-ci-build-validation.md)
- [Signed desktop updates](adr/0014-desktop-auto-update.md)

## Design records

Files under [`design/`](design/) document implemented behavior and focused
design work. They supplement the architecture document; they do not replace
the public API contract or release notes.

## Archive

[`archive/`](archive/) contains early feasibility and architecture records.
These files explain how the product reached its present design and are not
current requirements.
