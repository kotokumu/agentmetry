# Synthetic investigation verification

## Scope and fixture

Verification ran on 2026-09-04 against a temporary SQLite database populated
only with synthetic OTLP data. The browser used the built Web bundle served by
the Agentmetry binary. MCP requests used the same running process and database.
No provider transcript, repository metadata, prompt registry, or external
collector participated in the workflow.

The fixture contained:

- `codex/analysis-baseline`, with 7 retained activities;
- `codex/analysis-retry-heavy`, with 18 retained activities and five retry
  episodes;
- trace `00000000000000000000000000000999`, with a received prompt, an
  `AGENTS.md` reference, and a received read/tool output body; and
- trace `00000000000000000000000000000777`, with 1,201 initial spans, a
  900-second root, a 180-second child, 1,200 parallel children, one observed
  error, and one missing parent. Two later synthetic arrivals exercised refresh
  and count changes.

The context body was deliberately identifiable as synthetic:
`# Synthetic received AGENTS.md body`. Its presence proves that Agentmetry can
display a body received through supported telemetry. It does not establish that
reading the file caused a later model request to include it.

---

## Browser workflow

The following sequence completed as one browser workflow:

1. Apply the exact tool condition `Read`. The dashboard returned only matching
   conversations and kept `tool=Read` in generated conversation links.
2. Open `codex/analysis-retry-heavy` and select span
   `0000000000000103`. The activity detail identified `Received tool output`,
   `Received read or tool output`, `Body available`, and received field
   `output`; it separately said that model-input inclusion was unconfirmed.
3. Open trace `00000000000000000000000000000999` through the row's exact
   span link. The requested native span was selected, expanded, and focused.
4. Follow the trace return link. The route restored `tool=Read`, the
   source-qualified conversation, trace ID, span ID, selected row, expanded
   body, and Execution purpose.
5. Remove the investigation condition and open Comparison. Select
   `codex/analysis-baseline` as baseline and
   `codex/analysis-retry-heavy` as current.

The comparison rendered the following rounded values while MCP returned their
unrounded values and the same operands:

| Diagnostic | Baseline | Current | Change |
| --- | ---: | ---: | ---: |
| Initial validation success proxy | 0 / 1 = 0.0% | 0 / 1 = 0.0% | 0.0 pp |
| Rework token share | 2,950 / 8,990 = 32.8% | 14,800 / 20,840 = 71.0% | +38.2 pp |
| Retry-cycle effort share | 61,000 / 100,000 ms = 61.0% | 196,000 / 237,000 ms = 82.7% | +21.7 pp |
| Tool failure rate | 1 / 3 = 33.3% | 4 / 11 = 36.4% | +3.0 pp |
| Recurring loops per 100 validations | 0 / 2 = 0.0 | 1 / 5 = 20.0 | +20.0 |

The Web and MCP paths both reported incomplete current harness fingerprint
coverage. MCP `list_runs`, `get_run_timeline`, and `compare_rework` did not
contain the synthetic body sentinel when content was not requested.

---

## Long-trace and accessibility checks

The long-trace browser workflow showed full extent and retained total separately
from the detailed window. It located an exact span outside the first detail page,
then used **Show selected** to move the window without substituting another span.
The error-only filter returned the single observed failure. Applying a conflicting
kind filter retained the selected body and labelled it as outside the current
filter; clearing the condition restored it. A parent outside the overview cap did
not become a false missing-parent result.

At 200% browser zoom, the selected conversation, purpose controls, harness
coverage, comparison operands, and change labels remained available without
overlap that blocked operation. A 375 by 820 CSS-pixel viewport changed activity
and trace details to one-column layouts while keeping bodies and return controls
reachable. Enter-key operation switched Rework and Comparison and preserved the
pressed state. Focus indicators use textual selection/error/missing states as
well as color. Reduced-motion media rules remove status animation, hover
transforms, and row/control transitions.

The controlled live-update UI tests retain the chosen trace viewport and selected
body when new spans arrive. Browser streaming was not used as the stability
oracle; deterministic delivery in `trace-investigation-controller.test.ts` and
the component live-update tests supplies that assertion, while the browser
workflow verifies the resulting window, filter, selection, and narrow layout.

---

## Validation gates

| Command | Result |
| --- | --- |
| `go test ./... -count=1` | PASS |
| `go test -tags=integration ./... -count=1` | PASS |
| `npm --prefix web test -- --run` | PASS, 23 files and 247 tests |
| `npm --prefix web run build` | PASS |
| `buf lint` | PASS |
| `openspec validate improve-otel-investigation --strict` | PASS |
| `openspec validate project-supported-otel-span-events --strict` | PASS |
| `git diff --check` | PASS |
