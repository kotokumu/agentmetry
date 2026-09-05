# Specification Analysis: <!-- change name -->

<!--
Use only the representations needed to understand the Requirements and decide their boundaries.
Do not invent concepts to fill in irrelevant tables or sections. Delete optional sections that are unnecessary.
-->

## 1. Boundary

| Item | Decision | Evidence |
|---|---|---|
| Product capability | <!-- existing/new capability --> | <!-- existing spec or source --> |
| Change classification | <!-- behavior change / pure implementation --> | <!-- reason --> |
| Included behavior | <!-- scope --> | <!-- proposal/source --> |
| Excluded behavior | <!-- non-goal --> | <!-- proposal/source --> |

---

## 2. Consumers and Observable Events

<!-- Complete this section when the specification changes. For a pure implementation change, write "Not applicable" and explain why. -->

| Consumer / actor | Trigger / prior state | Interaction or event | Observable result |
|---|---|---|---|
| <!-- caller/user/system/process --> | <!-- trigger/state --> | <!-- action/event --> | <!-- result --> |

---

## 3. Concept Analysis

<!--
Record the decisions and evidence needed to select the published Conceptual Model. Do not duplicate its complete
reader-facing definitions here. Readers must not have to reconstruct terminology or state space from Scenarios.
-->

| Concept or change | Specification decision | Evidence | Owner capability and rationale |
|---|---|---|---|
| <!-- canonical term or relationship --> | <!-- decision needed to interpret Requirements --> | <!-- existing spec or source --> | <!-- single owner + reason --> |

### 3-1. Supporting Models (Optional)

<!--
Add representations only when the concept list alone does not permit a unique interpretation of the Requirements.
Candidates: state-definition, classification, or value-domain tables; relationship diagrams; and structural invariants.
Put acceptance partitions, condition combinations, transition rules, operational invariants, outputs, and Side Effects in Requirements.
Do not repeat the complete publication content or create diagrams of physical tables, DTOs, API payloads, or classes.
-->

---

## 4. Main Spec Conceptual Model Replacements

<!--
Write `None.` only when every changed Requirement can be interpreted from the existing main spec without a new or changed
concept, state, classification, value domain, relationship, unit, identity rule, or structural invariant.
For each capability with changes, add a level-three heading containing its path in code formatting. Immediately after it,
write the complete reader-facing overview as replacement text, excluding the `## Conceptual Model` heading, in a markdown fence.
Do not write a diff or ellipsis. Each capability also requires a Requirement delta at the same path.
To delete the entire Conceptual Model section, write `REMOVE` instead of a markdown fence.
`tools/archive-change.mjs` applies this content to the main spec and archives it together with the Requirement delta.
-->

None.

---

## 5. Requirement Candidates

| Requirement slug | Actor and event | Guarantee | Concepts used | Normative representations | Scenario tags |
|---|---|---|---|---|---|
| <!-- kebab-case --> | <!-- actor + trigger/action --> | <!-- observable contract --> | <!-- canonical concepts --> | <!-- Partition / Decision / State Transition / Invariant / prose --> | <!-- happy/error/boundary/etc. --> |

---

## 6. Unresolved Decisions

<!--
The SSOT for the current unresolved state. Resolve every item in the proposal's Decisions Required or carry it forward here.
Resolve all items before creating specs, and write exactly `None.` before publication. Do not treat a recommendation as a decision.
-->

None.

---

## 7. Sources

- <!-- authoritative source used for the model -->
