# ADR 0008: Source Profile Plugin Registry

- Status: Accepted
- Date: 2026-08-11

## Requirement Summary

Claude Code, Codex, and future AI agents share OTLP transport structures but use different event names, identifiers, token fields, and agent relationships. Agentmetry must add producer semantics without coupling OTLP admission, storage, dashboard, or MCP code to a specific producer.

The desktop, standalone, and container distributions must remain cross-platform single Go server binaries.

## Decision

Agentmetry uses a statically linked source-profile plugin registry. A plugin implements the public `sourceplugin.Plugin` Go interface and owns only producer detection and semantic event normalization. The composition root explicitly registers the first-party Claude Code and Codex plugins.

This ADR does not use Go's runtime `plugin` package. Runtime shared-library loading is incompatible with the Windows target and weakens single-artifact reproducibility. A future third-party runtime extension mechanism may use WASM or a versioned process protocol without changing the source-profile contract.

## Conceptual Model

```text
Decoded OTLP record
  -> Source Plugin Registry
       first matching plugin
       ├── Claude Code plugin
       ├── Codex plugin
       └── generic fallback
  -> Profiled event
       source ID
       canonical event name and aliases
       enriched attributes
  -> Common canonical derivation
  -> Journal / observations / read models
```

The original resource, scope, record name, attributes, and complete payload remain in canonical observation JSON. Plugin output is a projection, not a replacement for source evidence.

OTLP does not define an agent session hierarchy. Trace, log/event, and metric records remain distinct signals. Native OTLP trace/span identity stays separate from producer conversation identity:

- Claude Code `session.id` and Codex `conversation.id` are source attributes, not OTLP span fields.
- Source plugins alias those values to `gen_ai.conversation.id`; the current `session_id` / `run_id` projection is a compatibility name.
- Plugins never derive conversation identity from `trace_id`, or rewrite source conversation identity into trace identity.
- A shared trace ID creates correlation evidence but does not merge two source conversations.

The full signal and identity model is defined in [Grand Architecture section 3.3](../grand-architecture.md#33-otlp-signals-and-identity-semantics).

## Interface

```go
type Event struct {
    Name       string
    Attributes map[string]any
}

type Plugin interface {
    ID() string
    Match(Event) bool
    Normalize(Event) Event
}

type ProfiledEvent struct {
    Source string
    Event
}
```

The registry evaluates plugins in registration order. The first match wins. If none matches, it returns source `unknown` and a cloned generic event. Plugins receive owned attribute maps and must not mutate OTLP input data.

## Responsibility Assignment

| Component | Responsibility |
|---|---|
| OTLP receiver | Decode, journal, and acknowledge valid exports |
| Plugin registry | Select exactly one source profile and provide the generic fallback |
| Claude Code plugin | Interpret Claude session, prompt, agent, cache-creation, cost, and event aliases |
| Codex plugin | Interpret Codex event, collaboration, encrypted content, and tool aliases |
| Canonical derivation | Build source-neutral activities and token usage from normalized aliases |
| Observation builder | Persist selected source ID and complete source evidence |
| Composition root | Choose and order the plugins included in the product |

## SOLID Risks

- **SRP:** plugins do not receive storage, UI, network, or query dependencies.
- **OCP:** a new producer adds a package and one composition-root registration.
- **LSP:** every plugin returns the same event contract and preserves the input evidence.
- **ISP:** the interface has only detection and event normalization behavior.
- **DIP:** OTLP normalization depends on the source contract, not Claude or Codex packages.

The main risk is growing the plugin into a producer-specific pipeline framework. Signal decoding, persistence, replay, and querying remain outside plugins.

## Test Specification

1. The registry returns the first matching plugin and source ID.
2. Unknown sources use the generic fallback without mutating input attributes.
3. Codex fixtures retain their existing event, tool, delegation, and encrypted-content behavior through the plugin contract.
4. Claude fixtures map `session.id`, `prompt.id`, agent relationships, cache creation, model, and cost into common aliases.
5. One mixed OTLP export can normalize records from different registered sources.
6. Canonical observations persist the selected source while retaining raw payload and attributes.
7. Existing OTLP HTTP/gRPC, dashboard, MCP, and storage tests remain green.

## Consequences

- First-party plugins are replaceable modules but remain compiled into the product.
- A source package can evolve and be fixture-tested independently of the receiver and database.
- Users still install one executable and configure only OTLP for baseline ingestion.
- Installing an arbitrary third-party binary plugin at runtime is intentionally not supported by this PoC.
