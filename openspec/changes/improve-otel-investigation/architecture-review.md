# Investigation architecture review

## Verdict

**PASS — architecture construction gate.** The accepted design and `construction-plan.md` preserve the existing query, SQLite, transport, and Web boundaries. All three findings below have concrete resolutions in the construction contract and corresponding invariant tests. No unresolved architecture blocker remains. This verdict approves the planned boundaries; implementation still requires the listed behavior checks and final review.

Inputs: `OTEL-INVESTIGATION-1`, accepted design and delta specs, frozen minimal model, independently authored S1–S14, and content/compatibility audits. Baseline code is `ed6e5038d82d67298c8a569ad67f955caadd5cc9`. The user has authorized implementation of this scope.

---

## Findings

| Module / Boundary | Problem | Dependency or Responsibility Issue | Suggested Direction | Severity |
|---|---|---|---|---|
| Shared comparison / SQLite canonical grouping | Different requested IDs can resolve to one canonical conversation. Comparing request strings alone can admit a self-comparison. | Storage owns canonical membership; comparison owns eligibility. Both must use the same resolved facts. | Resolve both identities, timings, token totals, analyses, and harness facts through one read transaction. Return canonical subjects and reject equal resolved identities in the shared comparison rule. | Medium |
| Trace query / Web selection | Logs can carry the same trace/span pair as a stored span. Independent first-match logic can select different records. | Query resolution and presentation must agree about the exact evidence target. | Resolve the native stored span only: both query and UI match `signal=trace` with the exact trace/span pair. If only correlated logs remain, return the typed target error. This needs no new selected-activity wire field. | Medium |
| Content interpretation / transport | The existing Content string does not uniquely preserve its originating field, and retained attributes can contain ciphertext or body references. | Provider interpretation must not migrate into each UI/transport or expose the full attribute map as a shortcut. | Derive only verified content evidence from existing received projection fields in one owner. Pass bounded typed metadata, keep unknown provenance unknown, and apply existing body opt-in to every body-bearing field. | Medium |

These are resolved construction-contract findings, not assertions that unimplemented code contains defects. `construction-plan.md` assigns the same-transaction pair read to SQLite and canonical eligibility to query, fixes native-span-only selection in the result/compatibility rules, and constrains `DescribeActivityContent` to derived metadata without I/O or complete attribute forwarding. Its invariant tests cover canonical aliases and shared span/log IDs; its content tests cover provider-specific reference, input, redaction, and absence.

---

## Scenario coverage

| Scenario | Boundary assessment | Required construction evidence |
|---|---|---|
| S1, S7: exact evidence, return, keyboard | Navigation owns origin/view/selection/focus; trace query owns address resolution. Components render the selected identity without inferring another target. | Beyond-page and missing-target tests; browser return/focus and 200% zoom. |
| S2: full conversation predicates | Query conditions express conversation-level meaning; SQLite applies them after canonical grouping and before paging. Model/tool predicates may match different members/activities. | A matching conversation beyond the first page; mixed-member model/tool match; missing duration/outcome. |
| S3, S13, S14: saved and applied conditions | Web condition validation is shared by URL and saved state. Local profile persistence is confined to the Web boundary. Draft, active query, and persisted definitions remain distinct. | Version/unsupported-condition rejection; persistence failure; relative range reapplication; back/forward. |
| S4, S5: pair eligibility and measurement | Fixed rules belong in Go query, not protobuf/MCP or browser arithmetic. Existing aggregate comparison keeps its own contract. | Canonical alias, source/time rejection, missing versus zero operands, exact values/deltas across adapters. |
| S6, S10, S11: content and unsupported events | Verified projection interpretation is separate from raw retention. Unknown or raw-only data does not trigger retrieval or new ingestion behavior. | Separate provider fixtures and explicit redaction/reference/unknown states; existing raw retention behavior. |
| S8, S10: long trace and missing topology | Metadata overview owns full retained extent; bounded detail owns the selected range/page. Filters must not redefine trace totals or make hidden records missing. Parent relationships remain received facts. | 1200-record overview without body materialization; filtered/range counts versus full counts; missing parents and target outside visibility. |
| S9: live commits | SQLite owns read consistency; Web investigation state owns stable reading position. Pair reads must not call independently committed single-run readers. | Live commit between comparison reads; stable selected body/range during updates; response counts coherent with their snapshot. |
| S12, S13: consumer compatibility | Additive query operations and typed transport mapping preserve existing readers and `compare_runs`; unsupported new server methods remain visibly unavailable. | Existing aggregate requests and content defaults; old-server unavailable state; generated API and MCP discovery checks. |

No scenario supports another database abstraction, generalized metric registry, global UI state framework, or external content resolver.

---

## Baseline evidence

- [Query read interfaces](https://github.com/kotokumu/agentmetry/blob/main/internal/query/api.go) and [trace contract](https://github.com/kotokumu/agentmetry/blob/main/internal/query/trace.go) are infrastructure-independent contracts.
- [SQLite read methods](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/api.go) resolve canonical groups and read rework facts; [trace reads](https://github.com/kotokumu/agentmetry/blob/main/internal/storage/sqlite/trace.go) already use a read transaction and a stable span/log union order.
- [Activity](https://github.com/kotokumu/agentmetry/blob/main/internal/query/overview.go) keeps received attributes out of ordinary JSON serialization. The content audit identifies which verified facts can support a derived evidence response.
- [Connect](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/connectapi/server.go) and [MCP](https://github.com/kotokumu/agentmetry/blob/main/internal/transport/mcpserver/server.go) already consume query readers. [Navigation](https://github.com/kotokumu/agentmetry/blob/main/web/src/app/navigation.ts) owns browser return state.
