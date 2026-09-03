# Structured condition Web implementation

2026-09-04. Task 5.2.

`investigation-conditions.ts` is the shared condition codec for URL, drafts and saved conditions. It retains relative range, source, search, observed failure, inclusive elapsed bounds in milliseconds, exact model and exact tool. Unknown saved fields and invalid bounds are rejected. Zero is preserved; model/tool obey the server limit of 200 UTF-8 bytes.

The condition editor displays the last applied conditions separately from the form draft. App applies a validated draft only after ListSessions succeeds and positively acknowledges every non-default structured condition. Unsupported or failed requests leave the prior URL/conditions in place with an explicit error. History restores all conditions; live list reads carry the same predicates. Existing source, range and search controls also verify acknowledgement when structured conditions are active. Relative time remains a condition evaluated by the read API, never a saved timestamp/result snapshot.

Behavior verification:

- Codec scaffold initially failed all 8 assertions (lost conditions and accepted invalid inputs); implementation passes all 8.
- Condition form initially had no invalid-draft error; implementation passes the draft/last-applied behavior test.
- Client acknowledgement initially accepted absent/mismatched support; implementation passes absent, partial, mismatched and complete echo cases.
- App 26 tests PASS, including unsupported server, later supported echo, full AND conditions, invalid draft, and history back. Related client/controller/app set before the added app case: 65 tests PASS.

On direct URL requests the panel identifies unconfirmed conditions until the read succeeds, and a failed read explains the requested conditions were not applied. Body filtering and structured conversation matching are separate concerns. Saved-filter lifecycle is recorded by its implementation owner.
