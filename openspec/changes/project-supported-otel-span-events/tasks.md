## 1. Provider evidence and behavior design

- [ ] 1.1 Add pinned valid-OTLP fixtures for each supported Claude Code and Codex event name and verify fixtures state source version, exact fields, identity, time, content absence, and usage authority.
- [ ] 1.2 Specify provider-separated mapping tables and tests for supported versus raw-only events; verify unsupported and changed revisions create no event activity.
- [ ] 1.3 Complete conceptual, responsibility, interface, and test-design reviews for event identity, provider mapping, deduplication, and coverage before runtime construction.

## 2. Canonical projection and storage

- [ ] 2.1 Add the optional provider span-event mapping capability and build it with table-driven tests that prove Claude/Codex rules cannot cross.
- [ ] 2.2 Add deterministic versioned event identity and a non-contributing canonical event value; verify repeated processing and same-name/same-time distinct ordinals.
- [ ] 2.3 Persist event activities and their related parent identity in an additive SQLite migration; verify atomic journal/projection commits, idempotent upserts, and native span anchor precedence.
- [ ] 2.4 Correlate only outcomes with documented shared operation identities; verify parent/log/event fixtures produce at most one outcome and missing identities remain uncounted evidence.

## 3. Query and transport

- [ ] 3.1 Return event identity, source event name, timestamp, related trace/span, content-not-reported, and projection-version coverage in query results; verify body and token fields are absent.
- [ ] 3.2 Add compatible Connect, HTTP, MCP, and Web mappings; verify parity from one temporary SQLite fixture and unchanged reads by older clients.
- [ ] 3.3 Preserve native-span navigation when a related event exists; verify exact span anchors never select an event or correlated log.

## 4. Existing raw scope and documentation

- [ ] 4.1 Keep existing exports raw-only in the first version and surface new-only coverage; verify an older journal row is unchanged and does not imply event projection.
- [ ] 4.2 Document source versions, supported fields, raw-only fields, event identity, deduplication, and retained-export scope in the source telemetry and API guides; verify every claim links to a pinned fixture or implementation.
- [ ] 4.3 Design any retained-export replay as a separate opt-in change before implementation; verify its proposed cursor, transactional batch, deterministic upsert, rollback, and coverage behavior against the replay scenarios.

## 5. Validation

- [ ] 5.1 Run focused provider, ingestion, SQLite, query, transport, and Web tests and verify all pass.
- [ ] 5.2 Run `go test ./...`, `go test -tags=integration ./...`, `npm --prefix web test -- --run`, and `npm --prefix web run build`; resolve regressions and record the actual results.
- [ ] 5.3 Run `openspec validate project-supported-otel-span-events --strict` and verify implementation and documentation match every checked task.
