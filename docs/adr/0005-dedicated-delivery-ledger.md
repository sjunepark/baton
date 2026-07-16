# Store delivery state in one dedicated GitHub issue

> Historical: superseded by v0.7. Baton no longer owns delivery state or a
> delivery ledger.

Status: Superseded by v0.7.

Baton will store delivery authority in one locked, repository-scoped GitHub
issue rather than on each work pull request or in Git ancestry. A reviewed
bootstrap pins the issue and one mutable checkpoint comment by GitHub node ID
and database ID. Append-only comments on that issue hold versioned delivery
records; the checkpoint is the bounded index and the final commit point for
promotion delivery. Slice 4 defined and fixtured these contracts; Slices 5 and
6 implemented bootstrap, shadow comparison, sealed policy authority, and the
removal of ancestry/history readers.

## Threat model and trust

The contract protects against untrusted pull-request code and authors,
unrelated comments, delivery retries, stale events, partial writes, and
accidental record edits or deletion. Repository administrators, maintainers
who can edit other users' issue comments, and default-branch workflows granted
`issues: write` are trusted administrators; GitHub does not expose the last
editor as the comment author, so this design cannot distinguish a malicious
trusted administrator from Baton. A compromised trusted workflow can corrupt
the ledger and requires rollback from audit evidence.

Authoritative comments must be authored by `github-actions[bot]` with actor
type `Bot`, contain exactly one supported Baton marker, and match the pinned
repository, issue, ledger, and comment identities. Actor identity alone is not
sufficient: the installed writer must check out or install trusted
default-branch/released Baton code, and doctor will audit that workflow shape.
All ledger writers share one exact repository concurrency group configured
with `queue: max`; promotion-policy readers join that group once dual writes
begin so a check cannot straddle a record batch. Concurrency limits parallel mutation but is not an
event-delivery guarantee, so queue overflow and missed runs still require
repair. GitHub documents issue/comment writes as
requiring write permission and Actions concurrency as repository-scoped
serialization: [issue comment endpoints](https://docs.github.com/en/rest/issues/comments),
[token permissions](https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens),
and [workflow concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency).

## Location and bounded discovery

The repository policy will pin a `DeliveryStoreLocator` containing GitHub host,
repository node ID, ledger issue number and node ID, and checkpoint comment
database and node IDs. Normal reads fetch the issue and checkpoint directly, then fetch only
the record comments referenced by the checkpoint or its active sealed plan.
The checkpoint may reference at most 252 active records: 250 routine records,
one synchronization reserve, and one promotion-seal reserve that drains the
window. Missing references, an exceeded cap, or ambiguous locator identity make delivery incomplete and
fail managed promotion closed. Unmanaged pull requests do not read the store.

Bootstrap may discover candidates with the reserved `baton:delivery-state`
label and a versioned issue marker, but it must enumerate at most two matches
and require exactly one reviewed choice before pinning it. Discovery is never a
runtime authority. Append retries and repair walk comments backward to the last
checkpoint update and stop after 1,000 comments; failing to reach that boundary
is incomplete, not proof that no record exists. Reconciliation walks closed
pull requests in descending update order to the committed coverage observation
time, also with a 1,000-item ceiling. No active decision scans all closed
staging pull requests or all historical ledger comments.

The checkpoint marker is `baton-delivery-checkpoint:v1`; immutable record
comments use `baton-delivery-record:v1`. Every document carries a schema
version, kind, repository and ledger identities, and a deterministic SHA-256
digest over its canonical JSON with the digest field empty. Each immutable
record also carries a monotonic sequence, the previous committed record digest,
and a deterministic retry identity. The checkpoint commits the current
sequence and head digest.

The checkpoint also carries a digested `StagingCoverage` watermark: exact
staging SHA, delivery-record boundary, cursor digest, writer run, and observed
time. The trusted delivery-recorder workflow may advance it only after it has
reconciled the exact staging transition and appended every required managed
work record. It uses trusted code with `contents: read`, `pull-requests: read`,
and `issues: write`. A missed or canceled recorder run leaves the watermark on
the prior staging SHA, so a promotion cannot be sealed as manual-only by
mistake. Promotion plans bind the exact coverage digest, and the active seal
must be the active record-chain head.

Referenced comment IDs and digests make deletion a missing-record failure and
editing a digest or identity failure. Repeated sequence, retry identity, or
comment identity is a duplicate-record failure; an equivalent retry is a
writer no-op only when the existing committed record is found before creating
another comment. Unreferenced comments have no authority. An ambiguous create
is recovered by the bounded retry lookup or reported for repair. Hash chaining
commits ordering and detects alteration inside the retained active window; it
is not a tamper-proof archival system for a malicious trusted administrator.

## Delivery contracts

Before merge, PR Policy appends versioned evidence for the exact PR node,
base/work branches, base/head revisions, prose digest, policy schema, durable
issue references, decision, writer, and digest. A later rejected result is a
new record rather than an edit. Delivery accepts only the latest trusted
pre-merge evidence and rechecks the PR revision and merge event against it.

`StagedWorkRecord` snapshots the repository full name and node ID, work pull
request number and node ID, exact staging base/head and observed merge revision,
merge time, staging branch, and durable managed-issue references. Each issue
reference contains its number, node ID, and ownership-record digest. The record
preconditions bind the pull request to the same repository, managed work shape,
merged state, exact head, and then-current delivery cursor. Later pull-request
or issue text and label edits cannot change the snapshot.

`PromotionCursor` contains its own version, kind, monotonically advancing
position, through-sequence and through-digest, consumed sealed-plan digest, and
digest. Its identity is derived from the prior cursor plus the sealed plan, not
from the commit GitHub produces on the base branch. Merge, squash, and rebase
promotion therefore consume the same staged records even though their
resulting base commits differ. The checkpoint retains one exact staged-work
comment pointer for a non-genesis cursor so the through-digest remains
verifiable without enumerating consumed history.

`PromotionPlanRecord` is append-only and binds the managed promotion pull
request, exact base SHA, head SHA, current cursor digest, ordered included
staged-work pointers, ordered exclusion pointers, and a retry identity derived
from those facts. A draft is a pure in-memory calculation with no authority.
The trusted PR-policy workflow re-reads the pull request, cursor, and records,
then writes and indexes the seal using trusted code with `contents: read`,
`pull-requests: read`, and `issues: write`. The active checkpoint pointer makes
that exact record sealed. A later synchronization, pull-request head change,
base change, cursor change, or record change makes it stale; policy writes a
new seal and rebinds the active pointer rather than editing the old record. A
stale or orphan seal is non-authoritative and blocks transition until repaired
or superseded.

The checkpoint has one active-plan pointer, so sealed authority permits exactly
one open managed promotion. When an overlapping promotion event is observed,
PR Policy uses `checks: write` to re-request any prior successful policy check;
both revisions then fail closed until only one managed promotion remains.

Record retry identities exclude append sequence, prior chain digest, workflow
run ID, and record-write timestamp, so a later attempt at the same semantic
facts finds the same identity even after another committed append. The full
record digest retains placement, provenance, and timestamp, so distinct stored
bytes remain distinguishable.

`BaseIntegrationRecord` binds an acknowledged base tip to the staging revision,
cursor, and promotion plan that produced it, and records whether the evidence
came from a promotion or a base-to-staging synchronization. After a promotion,
the resulting base tip may be a merge, squash, or rebase result and need not be
an ancestor of staging. The promotion's recorded staging head must still remain
in the current staging lineage; a rewrite is `diverged`, not a sync task. Later
movement of base beyond the acknowledged tip is direct-base work pending
synchronization. Synchronization is deliberately
ancestry-preserving: its reviewed merge must make the acknowledged base
revision an ancestor of the recorded staging revision. Doctor will reject
staging rules that make that contract impossible. Promotion delivery itself
does not depend on ancestry.

The checkpoint keeps two bounded references: `baseIntegration` selects the
current acknowledged relationship, while `promotionIntegration` retains the
latest cursor-producing promotion result. Promotion consumption sets both to
the same immutable record. Synchronization advances only `baseIntegration`,
keeps active staged work and the cursor, and clears a stale seal. A reviewed
genesis boundary binds the initial acknowledged base SHA and staging coverage;
raw ancestry may propose that boundary but is never authority by itself.

Synchronization is accepted only for an exact same-repository merged PR from
base into staging. Both its pre-merge staging revision and base head must be
ancestors of the result. Squash/rebase synchronization therefore fails closed;
merge commits must be enabled and staging must not require linear history.

Synchronization also commits an ordered pending promotion-recheck batch bound
to its exact base-integration record. The recorder drains that batch before any
later delivery mutation and clears it with a second checkpoint update only
after every target is closed or superseded, already running, or successfully
re-requested. A process failure therefore cannot silently lose rechecks.

`ExclusionRecord` is the only way to omit staged work that was reverted before
promotion. The future `baton delivery-exclude` apply path is owned by a manual,
trusted workflow. It names the staged-record digest, promotion pull request and
head, current cursor, reason, requesting workflow run and time, and an
approving human review's stable ID, node ID, time, and exact repository URL.
The review must be submitted for that exact promotion head after the request.
Promotion policy
validates and seals that exact record. Baton will not infer semantic reverts
from commit messages and will not add generic cancelled or superseded states.

## Durable lifecycle and retry commitment

Promotion proceeds `draft -> sealed -> consumed` without mutating a plan
record. A draft has no durable identity. A seal is an immutable record selected
by the checkpoint. Consumption is represented by a later checkpoint whose
cursor names the sealed-plan digest and whose base-integration pointer names
the observed promotion result.

The post-merge transition first reads and preflights the pinned store, exact
promotion revision, seal, staged records, exclusions, managed-issue ownership,
issue states, and intended next checkpoint. It then applies recoverable issue
changes (closing open delivered issues and removing their awaiting-review
index), writes or reuses the base-integration record, and updates the
checkpoint cursor last. The checkpoint update advances the cursor, binds base
integration, removes consumed active records, and clears the active seal in
one final document. A failure before that update is retryable from the same
seal and operation report. A committed cursor naming that plan makes every
duplicate delivery event a total no-op: Baton does not re-read or re-close
issues, so a later human reopen is respected.

GitHub does not reliably expose whether a manually completed linear promotion
used squash or rebase. The integration record always binds the exact promotion
PR, pre-merge base, staging head, resulting base commit, plan, and cursor. Its
method is `unknown` when GitHub supplies no trustworthy method evidence; Baton
never guesses audit metadata, and delivery selection does not depend on it.

GitHub issue comments do not provide compare-and-swap. Trusted writers
therefore serialize through the shared concurrency group and re-read the
checkpoint immediately before mutation. A generation, digest, cursor, base,
head, or retry mismatch refuses the write. This prevents stale Baton events;
out-of-band trusted-administrator races remain inside the stated trust boundary.

## Migration and failure behavior

Bootstrap is an explicit dry-run/apply workflow. It chooses and pins the ledger,
requires a reviewed genesis cursor when history is ambiguous, backfills issue
ownership first, and reports every inferred staged relationship and
precondition. Partial bootstrap never enables the new reader. The initialized
locator is first committed with `delivery.authority: shadow`, which permits
bootstrap and recording but disables policy, transition, and recommendation
readers. Only a separately reviewed change to `sealed` enables those readers.

Deleting the pinned checkpoint cannot be repaired in place because a new
comment has a new identity. Recovery requires a reviewed locator repin and
config migration. Repair also cannot reconstruct a lost staged snapshot from
later-mutated pull-request prose; without the exact retained bytes or audit
evidence, the adopter must perform a reviewed rebootstrap with an explicit new
genesis boundary.

Dual-write initially appends records while bootstrap shadow-read compares
complete ledger plans with bounded ancestry containment and blocks on any
mismatch. Before cutover, rollback removes the locator or leaves authority in
shadow; ledger comments remain audit evidence. Repair is a
read-only planner by default and requires explicit apply to recreate missing
indexes or records from authoritative GitHub facts. It never invents a cursor,
rewrites an immutable record, or treats an orphan as committed without review.

Final cutover requires a pinned valid locator, complete ownership backfill,
valid checkpoint and active window, shadow agreement, compatible workflows and
permissions, explicit `delivery.authority: sealed`, and documented adopter
migration. After cutover, corrupt,
missing, truncated, duplicate, stale, or over-cap state fails only managed
promotion and delivery paths closed. Slice 6 removed the ancestry reader after
the documented migration gate rather than retaining a second authority.

After an active window is fully consumed and no recheck batch is pending, a
reviewed rollover may create a genesis checkpoint in a different locked issue.
The successor binds the predecessor locator and digest; after successor
creation, the predecessor is frozen with the exact successor locator and
digest. Runtime writers reject frozen checkpoints until config is reviewed and
repinned.

## Rejected alternatives

Baton-authored markers on each work pull request provide good local snapshots
but no bounded authoritative enumeration. A label or search index would still
require scanning an unbounded set of merged or edited pull requests and could
silently omit deleted markers. Those markers may be diagnostic mirrors, not
delivery authority.

An ancestry-preserving-only contract is compact but cannot preserve staged
work identity across squash or rebase promotion, snapshot issue relationships,
represent manual-only promotion or explicit exclusion, or make issue closure
idempotent. Ancestry remains a useful synchronization precondition only.

A Git branch/file ledger would provide immutable Git objects, but it adds
repository-content mutation, branch/ruleset conflicts, merge races, and a
second checkout lifecycle that Baton explicitly does not own. Actions
artifacts, caches, variables, and discussions were rejected because they are
expiring, size-constrained, non-auditable, or not universally available.

The chosen contracts earn an `internal/delivery` seam: it owns record schemas,
canonical marker parsing, completeness, sealing, cursor and integration
invariants, and pure transition planning. The GitHub adapter owns exact issue
and comment transport only; workflow orchestration owns trusted acquisition and
mutation order.
