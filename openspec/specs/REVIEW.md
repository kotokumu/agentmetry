# Specification Review

This checklist reviews semantic quality at the stage where each result exists. Apply only the section named by the current
artifact rule. `openspec validate` handles OpenSpec syntax validation.

## 1. Priority

| Priority | Classification | Approval condition |
|---|---|---|
| P0 | Incorrect normative rule, contradiction, unresolved decision encoded as normative, SSOT boundary violation, or information loss during publication | MUST be fixed |
| P1 | Omission or ambiguity that prevents reproducible implementation or verification | SHOULD normally be fixed |
| P2 | Readability, maintainability, or risk of future misunderstanding | Fix or record the reason |

---

## 2. Problem and Scope

- Do the problem, intended outcomes, and success criteria in the proposal correspond?
- Do In Scope and Out of Scope define the boundary from the consumer's perspective?
- Does the proposal avoid fixing a technical approach as an outcome or requirement?
- Can the reason for creating a capability be explained by how it differs from existing capabilities?
- Does every segment of a nested capability path use kebab-case?

---

## 3. Conceptual Model

- Are key subjects, states, classifications, values, units, and relationships used by Requirements defined beforehand?
- Are identity and uniqueness clear where they are needed?
- Are state spaces kept distinct from transition conditions?
- Is each concept defined by only one capability?
- Does the model avoid copying physical databases, API payloads, DTOs, classes, or file structures into the Conceptual Model?
- Are there no remaining synonyms, terms with multiple meanings, or unquantified adjectives?
- Does the model's `Unresolved Decisions` contain no item that could change the specification?
- When a main spec changes, does the model contain the complete replacement text?
- Does the model avoid adding concepts or diagrams merely because the template contains a field for them?

---

## 4. Requirement

- Does each Requirement have one guarantee that can change and be verified independently?
- Does the core normative guarantee contain MUST and make the conditions and guarantee clear?
- Do items within each block use the same classification axis?
- Are acceptance conditions, state transitions, outputs, Side Effects, and failure guarantees complete where needed?
- Are concurrency, idempotency, permission, and compatibility contractual where relevant?
- Does each non-functional requirement define its subject, conditions, measurement method, and threshold?
- Are implementation details, incidental behavior of the current implementation, defects, temporary notes, and Unresolved Decisions excluded?
- Does every Requirement ID use the correct path, including nested capabilities?

---

## 5. Representation Selection

- Does each continuous or ordered domain with different outcomes use a Partition Table with explicit boundaries and no unintended gap or overlap?
- Does each relevant combination of conditions use a Decision Table that covers every reachable distinct result?
- Does each lifecycle rule use a State Transition Table with current state, trigger, guard, next state, and result where applicable?
- Does each normative rule have one owner, without repeating row-specific or cross-cutting rules between tables and blocks?
- Is every always-true rule written as an Invariant and placed according to whether it defines concept validity or an operation's guarantee?
- Are state meanings and value domains defined in the Conceptual Model while transitions and operation results remain in Requirements?
- Are normative tables and Invariants present in the published main spec rather than only in `model.md` or Scenarios?
- Does the specification omit representations that do not match an actual rule structure?

---

## 6. Scenario

- Does every Requirement have a representative Scenario and, when success is permitted, a `happy` Scenario?
- Are error, boundary, permission, concurrency, idempotency, and compatibility covered where they affect the guarantee?
- Does GIVEN describe a consumer and prior state, WHEN an interaction or event, and THEN an observable result?
- Can the input and expected result be constructed unambiguously from each Scenario?
- Does each Scenario avoid introducing normative rules or undefined terms absent from the Requirement?
- Does each Scenario illustrate a rule without replacing or needlessly repeating its normative table or Invariant?
- Does the specification avoid adding perspectives that do not exist in the project merely to match the template?

---

## 7. SSOT and Interfaces

- Does the specification avoid duplicating existing SSOTs for API, data, UI, message, and external contracts?
- Can related Requirements and other SSOTs be traced from the Requirement's `References`?
- Does each cross-capability Concept reference resolve to one Concept ID in its owning capability?
- Are interface details kept distinct from observable guarantees provided through that interface?
- When a machine-readable interface is the authoritative source, does the main spec avoid copying its fields and types?

---

## 8. Workflow and Publication

### 8-1. Before Design Completes

- Are the proposal's capability and Requirement impacts reflected in delta specs?
- Can every difference between the model's Requirement Candidates and the actual Requirements be explained?
- Does every required Conceptual Model overview appear in `Main Spec Conceptual Model Replacements` as complete publication text?
- Is design created after all delta specs without redefining WHAT?
- Are the delta spec's level-two sections limited to Purpose and standard Requirement operations?
- Does design own the mapping between specification concepts and implementation structures without changing either the Conceptual Model or Requirements?

### 8-2. Before Apply

- Does every implementation task identify its Requirement or state that it is design-only?
- Does every task identify the observable test or check that proves completion?
- Are tasks dependency-ordered and free of new requirements, design decisions, or unresolved specification decisions?

### 8-3. Before Publication

- Does every capability with a Conceptual Model replacement have a Requirement delta at the same path?
- Does apply avoid updating the main spec's Conceptual Model early?
- Are all tasks complete and are their verification results recorded?
- Does `model.md` contain no Unresolved Decision that can change the specification?
- Does `openspec validate <change-name> --strict` succeed?
- Does publication use `node tools/archive-change.mjs <change-name>`?

### 8-4. After Publication

- Did the Conceptual Model replacement and Requirement delta publish to the same main spec?
- Are concepts, terminology, and Requirement IDs consistent in the published main specs?
- Did publication reject unknown delta sections instead of silently discarding them?
- Does `openspec validate --specs --strict` succeed?
