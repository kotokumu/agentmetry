# Product Specification Rules

`openspec/specs/` is the SSOT for guarantees observable and verifiable by consumers and for the concept definitions needed
to interpret them. Do not place implementation structures, research notes, Unresolved Decisions, or defect records here.

## 1. Artifact Responsibilities

| Artifact | Question answered | Must not contain |
|---|---|---|
| proposal | Why change? Which outcomes and scope are addressed? | Technical approach or detailed guarantees |
| model | Which concepts, states, relationships, and constraints support the guarantees? | Unconfirmed normative rules or implementation design |
| spec | What does the subject guarantee? | Implementation methods, incidental behavior of the current implementation, or Unresolved Decisions |
| design | How is the specification realized? | Redefined requirements |
| tasks | What is implemented and verified, and in which order? | New requirements or design decisions |

Structure a main spec in the following order. The Conceptual Model is the reader's overview of the specification, not merely
an analysis artifact. Omit it only when every Requirement can be interpreted without capability-specific terms, states,
classifications, value domains, relationships, or invariants.

```text
Purpose → Conceptual Model (when needed) → Requirements
```

Place a Requirement-specific external SSOT in that Requirement's `References`. Sources for the Conceptual Model may appear
under `### References` in the same section. Do not use an independent `## References` section that archive cannot preserve.

---

## 2. Capability Boundaries and Paths

Define a capability as a coherent responsibility from the consumer's perspective. Consumers include users, callers, external
systems, other components, and automated processes. Search existing capabilities before creating one.

| Subject | Decision criterion | Location |
|---|---|---|
| Observable capability | Its consumer, trigger, and result can be explained | The capability that owns the guarantee |
| Reusable product rule | Multiple capabilities depend on the same meaning | The capability that most strongly defines the meaning |
| Implementation-only structure | Consumers have no contract that depends on it | Design or code |

Do not make a technical layer, data structure, framework, or shared utility into a capability solely because it exists.

A capability ID is the path relative to `openspec/specs/` through the parent directory of `spec.md`. Every segment MUST use
kebab-case. Nested paths are allowed.

```text
openspec/specs/platform/search/spec.md
→ platform/search
```

---

## 3. Conceptual Model

The Conceptual Model gives a compact overview of the concepts, states, classifications, relationships, and constraints needed
to read the Requirements. A reader MUST be able to understand the vocabulary and state space before reading Scenarios. It is
not a place to copy a glossary or physical data definitions.

Define only concepts that meet at least one of these conditions:

- Multiple Requirements reference the concept.
- The concept has states, classifications, a value set, units, or identity rules.
- Relationships or invariants among concepts change the meaning of behavior.
- Synonyms or a term with multiple meanings change the interpretation of Requirements.
- Its meaning cannot be read unambiguously without inferring it backward from Scenarios.

Do not make tables, columns, DTOs, API payloads, classes, or packages into a Conceptual Model. Design owns the mapping between
concepts and physical structures.

| Layer | Defines | Does not define |
|---|---|---|
| Conceptual Model | Meaning, identity, state space, classification values, relationships, structural invariants, value ranges, and units | Transition triggers, operation procedures, or Side Effects |
| Requirement | Applicability, input acceptance, decisions, transitions, operational invariants, results, Side Effects, and failure guarantees | First definitions of concepts or classification values |
| Scenario | Observable results for concrete preconditions and actions | Normative rules absent from the Requirement |

Each concept MUST have exactly one owning capability. The capability that most strongly defines its meaning, invariants, and
lifecycle owns it. Other capabilities MUST NOT redefine it. Reference a guarantee through its Requirement ID. When another
capability must reference the concept itself, define it in the owner as `### Concept: <stable-kebab-case-slug>` and reference
it as `[related] [[concept:<owner-capability-path>/<concept-slug>]]`. Do not assign a Concept ID when no cross-capability
reference needs one.

---

## 4. Requirement

A Requirement ID is `<capability-path>/<requirement-slug>`. Write a cross-reference as
`[[<capability-path>/<requirement-slug>]]`. Every slug and path segment MUST use kebab-case.

```markdown
### Requirement: <stable-kebab-case-slug>

<subject> MUST <core normative guarantee>.

- **Preconditions**: <prior state, authorization, and existence conditions required for applicability>
- **Input and Acceptance**: <inputs, accepted ranges, defaults, and acceptance and rejection conditions>
- **Behavior Rules**: <decisions, calculations, state transitions, and observable outputs>
- **Invariants**:
  - <subject> MUST <condition that remains true before and after every permitted result>.
- **Side Effects**: <changes to related state, history, and notifications, and what remains unchanged>
- **Concurrency and Idempotency**: <concurrent execution, retries, duplication, and atomicity>
- **Failure Handling**: <failure outcomes observable to consumers>
- **References**: <another SSOT on which the Requirement depends>

#### Scenario: <concrete behavior> [<tag>]

- **GIVEN** <consumer and prior state>
- **WHEN** <interaction or event>
- **THEN** <observable result>
```

The preceding code is an original example of the required format. Use only the blocks that are needed and preserve the order
shown above. Bullets within one block MUST use the same classification axis.

Separate Requirements when their guarantees can change or be verified independently. Do not separate them when only the entry
point or technical path differs and the guarantee from the consumer's perspective is the same.

Write a non-functional requirement as a Requirement when its subject, conditions, measurement method, threshold, and failure
guarantee are confirmed. Do not make unquantified adjectives such as "fast," "safe," or "large-scale" into normative rules.

---

## 5. Selecting a Specification Representation

Choose a representation from the structure of the rule. Do not default to prose or expand every rule into Scenarios. Each
normative rule has one owner. Use more than one representation only for distinct rule structures. When representations
interact, refer to named partitions, states, or results rather than repeat their rules. Use concise normative prose when none
of these structures is present.

| Rule structure | Representation | Normative location |
|---|---|---|
| Numeric, date/time, version, size, or other continuous or ordered domain has different results by range | Partition Table | `Input and Acceptance` for acceptance rules; `Behavior Rules` for result rules |
| A result depends on a combination of conditions | Decision Table | `Behavior Rules` |
| A concept moves through a lifecycle | State Transition Table | `Behavior Rules`; state names and meanings remain in the Conceptual Model |
| A condition must always remain true | Invariant | Conceptual Model for concept validity; Requirement `Invariants` for a guarantee an operation must preserve |
| A concrete use demonstrates or verifies a rule | Scenario | After the complete normative Requirement |

In this methodology, a Partition Table has this column structure:

| Partition | Condition or range | Acceptance or result |
|---|---|---|

Its conditions MUST state inclusive and exclusive boundaries unambiguously. Relevant partitions MUST have no unintended gap or
overlap. Define the value's meaning, unit, precision, and time basis in the Conceptual Model when they are not already canonical.

A Decision Table has this column structure:

| Rule | Condition: <first condition> | Condition: <second condition> | Output or response | Side Effects |
|---|---|---|---|---|

Add one condition column for each independent condition. Include every reachable combination that produces a distinct
guarantee. State the default or otherwise result when it exists. Do not manufacture combinations prohibited by the Conceptual Model.

A State Transition Table has this column structure:

| Current state | Trigger or event | Guard | Next state | Output or Side Effects |
|---|---|---|---|---|

Define each state once in the Conceptual Model. The table owns permitted transitions and their results. State explicitly whether
an unlisted transition is rejected, ignored, or outside the capability's contract.

When a normative table is used, it owns results, Side Effects, and failures that vary by row. The dedicated Requirement blocks
own only rules common to every applicable row. Do not copy the same rule between a table and a block. Write each Invariant as
`<subject> MUST <condition that remains true>.` An Invariant is not a successful path or an input validation rule.

---

## 6. Scenario

Every Requirement MUST have at least one representative Scenario. When the Requirement permits a successful result, it MUST
have at least one `happy` Scenario. Add other perspectives only when they change the guarantee.

| Tag | Subject |
|---|---|
| `happy` | Primary successful result |
| `error` | Input rejection, unmet precondition, or dependency failure |
| `boundary` | Empty, zero, maximum, deadline boundary, all items, or partial items |
| `permission` | Unauthenticated, insufficient permission, or outside authorized scope |
| `concurrency` | Concurrent updates, conflicts, or reversed order |
| `idempotency` | Retry or duplication |
| `compatibility` | Existing consumers, existing data, or contract compatibility |

GIVEN describes state that exists before execution, WHEN describes a consumer action or event, and THEN describes an externally
observable result. A Scenario is a concrete example derived from the Requirement; it is not the sole location of a partition,
decision rule, transition, or Invariant. Do not make an internal function call or a write to a specific table the only expected result.

---

## 7. External Contracts and Implementation SSOTs

| Information | SSOT | Treatment in a spec |
|---|---|---|
| Product rules, state transitions, and invariants | Main spec | Describe in the Conceptual Model or a Requirement |
| API types, statuses, and error codes | OpenAPI or IDL | Reference from the Requirement's `References` |
| Data types, constraints, and indexes | Migration or schema | Describe only the conceptual meaning and reference the physical definition |
| UI placement, components, and visual state | Design system or UI artifact | Describe only consumer-observable operations and results |
| File, message, or event field contract | Machine-readable schema | Reference the existing SSOT |
| Defects, implementation divergence, and research notes | Issue tracker or audit record | Do not include in a spec |

When no machine-readable interface SSOT exists, `openspec/templates/interface-contract.md` may be copied to a management
location outside OpenSpec and made the authoritative source. That document is not an OpenSpec capability. Place observable
guarantees provided through the interface in the Requirement of the capability that owns them.

Choose the reference type from `[related]`, `[interface]`, `[api]`, `[data]`, `[code]`, `[decision]`, `[policy]`,
or `[external]`. A reference does not replace a normative rule; it connects the normative rule to another SSOT.

---

## 8. Deltas and Publication

Only the following level-two sections are allowed in a delta spec:

- `## Purpose`: Use only for a new capability.
- `## ADDED Requirements`
- `## MODIFIED Requirements`
- `## REMOVED Requirements`
- `## RENAMED Requirements`

`MODIFIED` contains the complete updated Requirement. OpenSpec 1.12.0 archive does not apply custom sections to a main spec.
Do not add `## References` or a Conceptual Model delta.

Write Conceptual Model changes as complete replacement text in `Main Spec Conceptual Model Replacements` in `model.md`. For
each changed capability, use its complete capability path as the level-three heading and place replacement content without the
`## Conceptual Model` heading in a `markdown` fence. Write `None.` only when no affected capability needs a change. Write
`REMOVE` instead of a fence to remove the entire Conceptual Model section. Every capability listed here MUST have a Requirement
delta at the same path.

Use the following for publication:

```bash
node tools/archive-change.mjs <change-name>
```

The publication command rejects unknown delta sections, stages the Conceptual Model, archives Requirement deltas, and strictly
validates all main specs after publication. A direct `openspec archive` does not apply the Conceptual Model.
