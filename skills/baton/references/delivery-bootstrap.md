# Delivery Bootstrap

Bootstrap the sealed delivery ledger only through the generated
`Delivery Recorder` workflow. Its repository-scoped concurrency group serializes
bootstrap with routine recording. Configure required reviewers on the
`baton-delivery-bootstrap` environment before the first dispatch; environment
approval is the apply-consent gate.

## 1. Initialize

1. Create and lock a repository issue carrying the reserved
   `baton:delivery-state` label.
2. Dispatch `Delivery Recorder` in `bootstrap-initialize` mode with the ledger
   issue, stable ledger ID, exact reviewed staging SHA, and one fixed RFC3339
   observation time.
3. Review the emitted JSON plan before approving apply.
4. Copy the returned repository, issue, and checkpoint identities into the
   optional `delivery` config block with `authority: shadow`, then commit that
   config through normal review.

Initialization stops on an unlocked issue, incomplete newest-comment
discovery, an existing checkpoint marker, or any identity mismatch.

## 2. Migrate in shadow mode

After the reviewed locator is active, dispatch `Delivery Recorder` in
`bootstrap-migrate` mode with either the last acknowledged promotion or an
explicit staging boundary.

Review every source fact, relationship, ambiguity, ownership backfill,
staged-work record, and open-promotion shadow comparison. Missing ownership,
history caps, mismatched boundaries, out-of-staging merges, ambiguous ancestry,
or any ledger/legacy projection mismatch block apply. Approve the environment
only when the emitted plan is complete and exact.

When the reviewed promotion corrects the initialized base boundary, apply
commits that exact genesis checkpoint before ownership backfills and staged
records. The operation report exposes that boundary update separately.

If apply returns a partial operation report, stop and repair it. Partial
bootstrap is not cutover.

## 3. Cut over separately

1. Confirm the recorder uses the exact released Baton pin.
2. After the shadow config reaches the default branch, manually dispatch
   `record` mode and require `complete: true`.
3. Confirm every shadow comparison reports `matches: true` and no ambiguity
   remains.
4. Change `delivery.authority` from `shadow` to `sealed` through a separate
   normal review.

Before merging any open managed work PR, rerun PR Policy so its latest trusted
append-only evidence names the exact revision and durable issue set. Historical
already-merged work is covered by the reviewed bootstrap records.

## 4. Roll over only after drain

Routine appends stop at 250 active references, with one additional reserve for
synchronization and one for a promotion seal. After promotion consumes the
active window and no pending recheck batch remains, create and lock a different
delivery-state issue. Run
`bootstrap-initialize` with the current locator still configured, the existing
ledger ID, exact committed coverage staging SHA, and a fixed observation time.
Review and apply the initialization v2 plan, then repin the returned successor
locator through normal review. The predecessor is frozen with the successor's
exact identity and digest; do not resume routine recording against it.

Never remove ledger records or infer that incomplete acquisition means no
record exists. Repair incomplete state from the trusted workflow's reviewed
dry-run output for `baton delivery-bootstrap` or `baton delivery-record`.
