# Delivery bootstrap

Delivery bootstrap is explicit and review-gated. It never edits the adopter's
checkout. Planning is the temporary shadow-read phase; committing the reviewed
locator after a complete bootstrap is the sealed-authority cutover.

Bootstrap planning and apply must run through the generated `Delivery Recorder`
workflow. Its repository-scoped concurrency group prevents the routine recorder
from committing a checkpoint between the reviewed plan and apply. Configure
required reviewers on the `baton-delivery-bootstrap` GitHub environment before
the first dispatch; the environment approval is the apply consent gate.

## 1. Initialize the pinned store

Create a repository issue with the reserved `baton:delivery-state` label, lock
it, and choose the exact staging SHA for the reviewed starting boundary:

Dispatch `Delivery Recorder` with mode `bootstrap-initialize` and supply the
ledger issue, ledger ID, full staging SHA, and one fixed RFC3339 `observed_at`.
The plan job emits the exact JSON plan. Inspect it, then approve the
`baton-delivery-bootstrap` environment. The apply job uses the same workflow
run ID and passes the unchanged plan ID automatically.

Initialization refuses an unlocked issue, incomplete newest-100 discovery, or
an existing checkpoint marker. Apply returns the exact repository, issue, and
checkpoint identities. Copy them into the optional `delivery` config block
with `authority: shadow` and commit that change through normal review.

## 2. Review historical inference

After the locator is active, preview migration with the last acknowledged
promotion or an explicit staging boundary:

Dispatch the same workflow with mode `bootstrap-migrate` and either
`genesis_promotion` or `genesis_staging_sha`.

Review every source fact, relationship, ambiguity, ownership backfill,
staged-work record, and open-promotion shadow comparison. An awaiting-review
issue must have exactly one inferred post-genesis work PR. The bounded ledger
projection must match the bounded ancestry-containment projection for every
open managed promotion. A mismatch, history cap, missing ownership, mismatched
checkpoint boundary, out-of-staging merge, or ambiguous ancestry order blocks
apply.
Approve the environment only if the emitted plan is correct.

Ownership comments are written before staged records. Each immutable record
append and checkpoint commit has a separate operation result. Promotion-policy
rechecks run after every checkpoint commit succeeds; if a later bootstrap
operation fails, the partial-return path still attempts those rechecks. Stop
and review the operation report after any partial result; partial bootstrap is
not cutover.

## 3. Confirm cutover readiness

Confirm the recorder uses the exact released Baton pin, then manually dispatch
it in `record` mode after the config change reaches the default branch. The
recorder's `complete` result must be true. A complete scan also advances the
checkpoint coverage to the exact observed staging head when no managed record
is missing. Confirm every shadow comparison reports `matches: true` and the
plan has no ambiguity. Then change `delivery.authority` from `shadow` to
`sealed` through a separate normal review.

With sealed authority, PR Policy writes and activates the exact sealed plan;
policy, work transition, and recommendations read the bounded ledger without
an ancestry/body fallback. If bootstrap or reconciliation is incomplete,
remove no records and repair with `delivery-bootstrap --dry-run` or
`delivery-record --dry-run`; incomplete state fails closed.

## SemVer

Bootstrap itself was additive, but the authority cutover changes promotion and
recommendation behavior and bumps public policy/transition JSON to v3. The
cumulative change is a breaking `feat!:` and therefore a pre-1.0 minor release
under Baton's Release Please policy. Release Please owns the manifest,
changelog, tag, release, and pinned adopter-target updates.
