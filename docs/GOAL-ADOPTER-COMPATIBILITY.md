# Goal: Collision-Free Baton Adoption

Status: Complete — Slices 1-10 are implemented, reviewed, and validated.

## Current State

Slice 1 froze adopter collisions and made invalid existing config stop init
reconciliation. Slice 2 replaced target-first behavior with one PR ownership
classifier, durable trusted issue records, versioned policy/transition flows,
and conservative all-target workflow routing. Queue, dashboards, references,
and transitions now consume explicit ownership before enrichment or writes.
Slice 3 removed implementation, skip, and trivial labels from work-PR merge
authority, retired the all-trivial config rule behind a decode-only migration,
and bound commit facts to the PR event head revision. Slice 4 selected one
dedicated GitHub issue as the bounded delivery ledger, defined its checkpoint,
coverage, record, cursor, sealing, integration, exclusion, retry, and migration
contracts, and froze them behind pure adapter-shaped fixtures. Slice 5 added a
trusted recorder, reviewed bootstrap, bounded reconciliation, durable ownership
backfill, and final promotion-check reacquisition. Slice 6 added an explicit
shadow/sealed migration gate, sealed promotion authority, durable work
transitions, bounded ledger recommendation facts, and removed the ancestry and
unbounded closed-PR readers. Slice 7 made the post-merge transition the explicit
delivery authority: it closes sealed-plan issues, removes the awaiting-review
index, records base integration, and commits the cursor last. Duplicate events
after that commit are total no-ops. Slice 8 added revision-bound integration
facts, reviewed ancestry-preserving base-to-staging synchronization, promotion
gating, sync-first recommendations, and merge-setting/linear-history doctor
checks. Slice 9 turned doctor into a live, authenticated adoption compatibility
gate covering exact managed content, workflow trust, Actions policy, required
checks, repository rules, queues, ownership, and delivery readiness. Slice 10
consolidated the current vocabulary and public contracts, documented one v0.6
migration path, retired stale schema/helper/fixture names, made merged-work PR
dashboards ledger-backed, and completed the Release Please audit.

## Objective

Make Baton coexist safely with ordinary repository development after
`baton init` is applied. Baton may manage explicitly opted-in issues, work PRs,
and promotions, but it must not treat a branch target by itself as proof of
Baton ownership.

The completed system must let maintainers merge ordinary PRs to the base branch,
synchronize those changes back to staging, and use non-Baton PRs without Baton
blocking, recommending, or mutating them. Promotion and issue completion must
remain correct across the repository's supported merge strategies.

## Problem Statement

Current behavior has several adoption collisions:

- Every PR targeting the staging branch is classified as Baton work.
- Queue and transition paths inherit that broad classification.
- Installed workflows start Go setup and Baton installation for unrelated PRs.
- Direct base-branch work can leave staging behind with no supported sync flow.
- Promotion evidence is derived from `base...head` Git ancestry and mutable PR
  text, so squash/rebase merges and later edits can produce incorrect delivery
  facts.
- Passing promotion policy does not guarantee GitHub will close the expected
  issues.
- `doctor` can report readiness without checking the GitHub settings and
  installed automation that determine whether Baton will coexist safely.
- Invalid existing config can be replaced with rendered defaults during init
  planning instead of failing as a config error.

Three bounded fixes already present at plan creation must remain covered by
tests: config rejects identical base/staging branches, init resolves the
repository root from a nested directory, and direct-base PR policy returns
without GitHub fact acquisition.

## Ownership Contract

Use one pure ownership classifier everywhere.

- **Baton issue intake candidate:** an issue whose current body matches the
  configured form fingerprint. Trusted issue-policy automation may create the
  durable ownership record and initial controlled labels; the fingerprint has
  no merge or delivery authority by itself.
- **Managed issue:** an issue with a durable, versioned ownership record written
  by trusted Baton issue-policy automation after the configured form fingerprint
  first matches. Later body or label edits do not silently revoke ownership.
  Coincidental labels or headings alone never establish durable ownership.
- **Managed work PR:** a same-repository PR whose head starts with the configured
  work-branch prefix and whose base is the configured staging branch.
- **Misrouted managed work PR:** a same-repository PR using the reserved
  work-branch prefix but targeting another branch. Baton may reject this because
  the prefix is the explicit opt-in signal. Fork branch names never establish
  ownership in the target repository.
- **Managed promotion PR:** a same-repository PR from the configured staging
  branch to the configured base branch.
- **Indeterminate managed intent:** a prefix/base/head combination that looks
  managed but lacks repository identity needed to prove it. Baton fails this
  event closed instead of silently treating missing facts as unmanaged.
- **Unmanaged PR:** every other PR, including dependency updates, manual feature
  PRs, fork PRs, direct base PRs, and base-to-staging synchronization PRs. Baton
  returns a successful no-op without GitHub reads, queue candidates, or
  lifecycle mutations.

Do not add a `staging_is_exclusive` mode. Explicit ownership is the single
supported model.

## Confirmed Direction

- Keep deterministic ownership, policy, delivery, and transition planning in
  Go. Generated workflows may use a conservative candidate prefilter to skip
  obvious non-candidates before setup, but the Go classifier remains
  authoritative and the prefilter must have no false negatives while generated
  files match compiled policy.
- Reserve the configured work-branch prefix as the PR opt-in marker. Do not add
  another config flag unless implementation proves the prefix insufficient.
- Treat issue labels as intake and scheduling facts, not durable merge
  authority. A label edit must not silently stale a required PR check.
- Keep the awaiting-review label as a query index, but back it with a durable,
  versioned delivery record rather than an unbounded scan of merged PR history.
- Derive promotion contents from staging delivery records and a staging cursor,
  not from comparison of the promotion base and head revisions.
- Close delivered issues through the trusted post-merge transition workflow.
  PR-body closing keywords are presentation, not the completion mechanism.
- Support direct base changes by detecting when base is not contained in
  staging or acknowledged by the latest promotion record, then requiring a
  normal, human-reviewed base-to-staging sync PR.
- Do not automate merges. Baton may report or plan the required synchronization.
- Make merge-queue incompatibility an explicit `doctor` failure. Treat
  `merge_group` support as a separate future goal after an adopter requires it
  and official GitHub facts provide stable membership; never infer membership
  from undocumented ref-name parsing.
- Keep config decoding strict. Defaults apply only when config is absent.

## Required Invariants

1. Unmanaged issues and PRs produce no Baton policy error, recommendation, or
   mutation. The sole pre-ownership exception is trusted issue intake creating
   a record for a current form candidate.
2. Every policy, queue, history, and transition decision uses the same
   authoritative ownership classifier. Generated workflow prefilters are
   conservative transport filters with parity/no-false-negative tests, not a
   second classifier.
3. Using the reserved work-branch prefix is explicit Baton intent and cannot
   bypass policy by targeting another branch.
4. A direct base change cannot be promoted over accidentally. Baton
   distinguishes an acknowledged promotion result from unincorporated base
   work, reports synchronization as required only for the latter, and managed
   promotion policy fails until synchronization completes.
5. Promotion results do not change when the base branch uses merge commits,
   squash merges, or rebase merges that Baton declares supported.
6. Work-to-issue relationships used for delivery are snapshotted by trusted
   automation and do not depend on later PR title/body edits.
7. Missing, corrupt, truncated, stale, or contradictory delivery records fail
   closed for managed promotion while leaving unrelated PRs alone.
8. A merged promotion has one idempotent transition plan that records delivery,
   closes exactly the delivered open issues, and preserves partial effects in
   an operation report.
9. `doctor: ready` means the live repository configuration, workflows, labels,
   branch relationship, authentication, and merge settings are compatible with
   Baton's declared behavior.
10. Public config, JSON, exit-code, and generated-workflow changes receive an
    explicit migration and SemVer decision.
11. Once a completed promotion transition commits its cursor, duplicate event
    delivery is a no-op and does not re-close an issue a human later reopens.

## Target Module Shape

- The existing policy module owns the small pure ownership interface and PR
  policy decisions. Do not create a pass-through package solely to rename the
  classifier.
- The workflow module acquires facts only after ownership is known and
  orchestrates policy or transition use cases.
- A delivery module owns versioned staged-work records, promotion cursors,
  promotion-plan state, base-integration facts, completeness, and transition
  planning behind one interface after the storage ADR earns that seam.
- The GitHub adapter supplies transport facts and comment/issue mutations; it
  does not decide ownership or delivery semantics.
- Queue and snapshot modules consume already-classified managed resources.
- Doctor composes local and live GitHub compatibility checks; it does not
  silently repair settings.

## Slice 1: Freeze Collision Regressions And Strict Init Failure

- [x] Add end-user fixtures for dependency/manual PRs into staging, ordinary
      PRs into base, base-to-staging sync PRs, fork PRs, managed work PRs,
      misrouted prefixed PRs, and staging-to-base promotions.
- [x] Characterize current policy JSON, queue candidates, transition plans, and
      installed workflow behavior for those fixtures before changing them.
- [x] Change install reconciliation to fall back to defaults only when
      `errors.Is(err, config.ErrConfigNotFound)`. Parse or validation errors must
      stop planning with a typed config error.
- [x] Add regression tests proving `init --yes` cannot overwrite an invalid
      existing config or render workflows from unrelated defaults.
- [x] Record the expected public-contract/SemVer impact of the planned ownership
      and config changes.

Exit criteria: invalid config is never converted into a default-policy install
plan, and every collision scenario has a failing or characterization fixture.

### Slice 1 Contract And SemVer Decision

- Strict invalid-config init failure is a safety bug fix (`fix:` / patch). It
  changes an erroneous destructive path into the existing typed config-error
  path (exit 3) without changing config, JSON, or operation-report schemas.
  Missing config still bootstraps compiled defaults.
- The planned ownership cutover is breaking public behavior (`feat!:`). It
  changes `prPolicyDecision.flow` meanings/values, queue and transition
  inclusion, and generated workflow triggers and job prefilters, all of which
  are declared public SemVer surfaces. Under the repository's pre-1.0 release
  policy, that requires a minor release plus an adopter migration note.
- Accept-but-ignore migration of
  `pr_policy.reject_all_trivial_multi_issue_prs` may preserve strict decoding
  during the rollout, but changing its runtime effect and eventually removing
  the wire field remain part of the breaking ownership/config migration. The
  removal must not ship as an undocumented patch cleanup.

## Slice 2: Centralize Explicit Resource Ownership

- [x] Replace target-first PR classification with the ownership contract above.
      Include repository identity in the classifier input where it matters.
- [x] Return an explicit unmanaged/no-op result for ordinary PRs. Preserve a
      distinct failing result for misrouted prefixed work and indeterminate
      managed intent.
- [x] Define a versioned managed-issue ownership record written by trusted issue
      policy when the form first matches. Specify trusted author, stable issue
      identity, duplicate/edit/delete handling, and explicit unmanage behavior
      (or state that unmanage is unsupported in this goal).
- [x] Dual-write that ownership record without immediately removing the current
      form-fingerprint read. Mark the fingerprint read as a bounded migration
      path and expose its source in diagnostics.
- [x] Expose one managed-issue ownership decision and use it in issue policy,
      queue intake, and referenced-issue validation. Labels alone must not make
      an issue eligible.
- [x] Route PR policy, queue/snapshot acquisition, merged-work handling, history,
      and transition planning through the same classifier.
- [x] Filter unmanaged PRs before check/review enrichment and before queue
      recommendation construction.
- [x] Delete duplicated branch/head heuristics once callers use the central
      classifier.
- [x] Change the generated PR workflow to trigger on all PR targets so
      same-repository prefixed work cannot bypass policy on an unlisted branch.
      Add a job-level conservative candidate prefilter that skips obvious
      non-candidates before checkout/setup/install, then lets Go make the final
      ownership decision.
- [x] Apply the same conservative prefilter to transition automation. Generated
      prefilter drift from compiled config must be detected in this slice, not
      deferred to the final doctor work.
- [x] Add table-driven tests at the classifier interface and workflow tests
      proving unmanaged events construct no GitHub client. Add generated-YAML
      parity tests proving the prefilter has no false negatives for every
      managed, misrouted, and indeterminate fixture.
- [x] Update public flow fixtures, workflow docs, adopter migration notes, and
      SemVer classification for this slice before considering it releasable.

Exit criteria: all fixtures agree on ownership across policy, queue, and
transition paths; unmanaged resources are observable no-ops; generated
prefilters cannot skip a candidate the Go classifier would manage or reject.

## Slice 3: Remove Mutable Intake State From Merge Authority

- [x] Limit managed work-PR policy to stable PR/repository facts: ownership,
      issue references, forbidden early-closing keywords, and durable commit
      rules.
- [x] Validate referenced resources through their durable managed-issue records
      (with the explicitly temporary legacy read during migration), not current
      form headings or labels.
- [x] Do not require current implementation/skip/trivial labels for an already
      opened PR to remain mergeable. Re-check durable ownership at transition
      preflight before any issue mutation.
- [x] Move any useful label-based restriction to issue recommendation/intake.
      Remove `reject_all_trivial_multi_issue_prs` if it has no durable intake
      role. During the documented config migration window, accept-but-ignore
      the old wire field so strict decoding does not strand existing adopters;
      remove it only in the declared breaking version.
- [x] Remove issue-label fetches from PR policy once no stable rule consumes
      them.
- [x] Prove with tests that issue label edits do not require a cross-resource PR
      check rerun, issue-body edits do not revoke durable ownership, and neither
      can mutate an unrelated PR decision.
- [x] Update config fixtures, migration docs, JSON/TOON fixtures, and SemVer
      classification in this slice.

Exit criteria: issue labels guide which work Baton starts, while PR merge policy
depends on durable ownership and facts bound to that PR event/revision.

## Slice 4: Decide Delivery Storage And Contracts

Write an ADR before implementation. Compare at least Baton-authored PR marker
records, a dedicated GitHub state resource, and an ancestry-preserving-only
contract. Use the delivery module/seam only if the chosen design earns it.

The ADR must define:

- discovery and bounded enumeration without scanning all closed staging PRs;
- the trusted author/actor and exact record location;
- stable record identity, version, digest/hash chaining if used, and duplicate,
  edit, and deletion detection under the stated threat model;
- a `StagedWorkRecord` that snapshots repository, PR, merge revision, durable
  managed-issue references, and preconditions;
- a `PromotionCursor` independent of the commit identity produced on base by
  merge, squash, or rebase promotion;
- a promotion-plan lifecycle: `draft -> sealed at exact base/head/cursor ->
  consumed`, including which trusted workflow writes it, required permissions,
  synchronization rebinding, stale-head behavior, and retry identity;
- a `BaseIntegrationRecord` or equivalent fact that distinguishes the base tip
  produced by an acknowledged promotion from later direct-base work;
- an explicit user-reviewed exclusion mechanism for staged work reverted before
  promotion. Do not infer semantic reverts from commit messages or add generic
  cancelled/superseded states without an owning command/workflow;
- durable commit order: preflight all operations, apply recoverable issue
  changes, and advance/commit the promotion cursor last. Once committed,
  duplicate delivery is a complete no-op so later human reopen is respected;
- bootstrap, shadow-read, rollback, repair, and final cutover rules.

- [x] Record the selected contracts, invariants, failure behavior, and rejected
      designs in the ADR.
- [x] Add pure contract/parser tests and storage-adapter fixtures without
      changing production promotion authority.
- [x] Prove fixtures can represent merge/squash/rebase promotion, manual-only
      promotion, explicit pre-promotion exclusion, duplicate records, edited or
      deleted records, and stale sealed plans.

Exit criteria: storage discovery/authenticity, promotion sealing, base
integration, revert exclusion, retry commitment, and migration are decided and
fixture-backed before any production dual write.

## Slice 5: Bootstrap And Dual-Write Delivery Records

- [x] Add trusted work-merge dual-write of staged-work records while keeping the
      current promotion reader authoritative.
- [x] Add a dry-run bootstrap plan for existing managed issues, awaiting-review
      items, merged staging work, and the last acknowledged promotion. Require
      an explicit genesis cursor/boundary when history cannot prove one.
- [x] Make bootstrap output show every inferred issue/PR relationship, source
      fact, ambiguity, and exact planned record. Ambiguity blocks apply.
- [x] Apply bootstrap only through explicit consent and stale preconditions;
      preserve partial outcomes in `operationReport`.
- [x] Backfill durable managed-issue ownership records before relying on them;
      retain the legacy fingerprint read only for the documented migration
      window.
- [x] When staged-work records are written or repaired, re-evaluate open managed
      promotions whose observed heads contain that work so a race cannot leave
      a stale successful check.
- [x] Bound new record reads and expose completeness, but do not remove the old
      reader yet.
- [x] Update public contracts, bootstrap docs, adopter notes, and SemVer
      classification for this slice.

Exit criteria: new events dual-write records, existing adopters have a reviewed
bootstrap path, and no production decision depends solely on the new ledger.

## Slice 6: Shadow-Read, Cut Over, And Remove Unbounded History

- [x] Compute new promotion plans in shadow mode and compare them with current
      ancestry results across fixtures and bootstrapped repositories. Mismatches
      are explicit and block cutover.
- [x] Have trusted promotion policy write an append-only sealed plan tied to the
      exact PR base SHA, head SHA, cursor, included/excluded records, and digest.
      A later synchronize/record change produces a new seal and invalidates the
      old one.
- [x] Cut policy and transition readers over only when ownership records,
      staged-work records, cursor, and the matching sealed plan are complete.
- [x] Replace `FetchPromotionHistoryContext(baseSHA, headSHA, ...)` and mutable
      PR-body association reads with the chosen delivery interface.
- [x] Remove the unbounded closed-staging-PR scan from repository snapshots and
      use bounded authoritative transition state instead.
- [x] Keep a read-only diagnostic/repair planner; remove temporary dual reads
      after the documented rollout window rather than preserving two permanent
      authorities.
- [x] Test delayed record writes, edited PR/issue bodies, missing/duplicate
      records, stale cursor/seal, cap/partial failure, and repeated/manual-only
      promotions.
- [x] Update public fixtures, migration state, adopter notes, and SemVer
      classification for cutover/removal.

Exit criteria: promotion authority is the complete sealed delivery plan; the
same records are selected regardless of the previous base merge method; no
active recommendation scans all closed staging PRs.

## Slice 7: Make Promotion Completion Explicit And Idempotent

- [x] Extend the pure transition planner so a merged managed promotion consumes
      its sealed promotion plan and produces record-delivery plus issue-close
      operations.
- [x] Add the minimum GitHub adapter mutations needed to close issues and write
      delivery records. Keep all operations idempotent.
- [x] Preflight the current PR identity/revision, promotion plan digest, cursor,
      issue state, and existing delivery records before the first write.
- [x] Close exactly the still-open delivered Baton issues, update/remove the
      awaiting-review index as chosen by the ADR, and skip already-satisfied
      operations.
- [x] Advance the promotion cursor/commit record last. If it is already
      committed, return a complete no-op without inspecting or re-closing issues
      that a human may have reopened afterward.
- [x] Preserve partial mutations in `operationReport` and make the existing
      event command safely retryable.
- [x] Stop requiring promotion PR closing keywords for correctness. If rendered
      for human readability, verify they agree with the sealed plan but do not
      delegate closure to GitHub's keyword behavior.
- [x] Test duplicate event delivery, partial failure/retry, manually closed or
      reopened issues before and after durable commit, stale plans, and
      promotion with no managed work.
- [x] Update public transition fixtures, permissions docs, adopter note, and
      SemVer classification in this slice.

Exit criteria: a successful post-merge transition, not PR prose or repository
auto-close settings, is the authority that delivered managed issues are closed.

## Slice 8: Support Base-To-Staging Synchronization

- [x] Derive complete base-integration facts from observed base/staging SHAs,
      the latest committed promotion result, and the chosen integration record:
      integrated, direct-base-work-pending, diverged, or unknown.
- [x] Do not equate raw `base SHA is an ancestor of staging` with integration.
      A base tip produced by a recorded merge/squash/rebase promotion is already
      acknowledged even when that commit is absent from staging.
- [x] Require ancestry-preserving merge for base-to-staging synchronization, or
      choose and record another verifiable mechanism in the ADR. If ancestry is
      required, doctor must verify staging rules permit it and explicitly reject
      squash/rebase-only synchronization.
- [x] Make managed promotion policy fail on unincorporated direct-base work and
      bind the decision to observed base/head/integration-record revisions.
- [x] Make snapshot/queue return a `sync-staging` repository action for that
      state. Ordering is: incomplete delivery repair first, then sync, then
      existing PR follow-up, then new issue intake.
- [x] Give instructions for a normal, human-reviewed base-to-staging sync PR.
      Do not push, merge, or rewrite staging automatically.
- [x] Recognize that sync PR as non-work/non-promotion and ensure the conservative
      workflow prefilter cannot make setup/install failures block it.
- [x] Test ordinary direct-base work followed by sync, plus post-promotion
      readiness for merge/squash/rebase promotions. Also test incompatible
      squash/rebase sync and linear-history staging rules.
- [x] Update public outcome fixtures, user flow, adopter note, and SemVer
      classification for this slice.

Exit criteria: direct-base development has a verifiable recovery path, while a
normal completed promotion never creates a false sync requirement.

## Slice 9: Turn Doctor Into An Adoption Compatibility Gate

- [x] Resolve and call the live GitHub repository with discovered credentials;
      token discovery alone is not an auth check.
- [x] Check default branch, base/staging relationship, managed labels, installed
      ownership/delivery record readiness, workflow presence and trusted
      checkout shape, pinned Baton version,
      workflow permissions/triggers, required checks, merge methods, linear
      history assumptions selected by the ADR, and merge-queue state.
- [x] Check whether Actions and the installed workflows are enabled and whether
      repository/organization allowed-actions policy permits the generated
      actions. Report unavailable permission/runner-policy facts as degraded or
      blocked rather than assuming execution will work.
- [x] Treat active merge queues as blocked with precise remediation. Do not add
      `merge_group` parsing or support in this goal.
- [x] Reuse reconciliation output for managed-file drift. Distinguish a harmless
      editable-document warning from security/policy workflow incompatibility.
- [x] Keep statuses actionable: `ready` means compatible, `degraded` means Baton
      remains safe with reduced capability, and `blocked` means applying Baton
      policy would collide or be unreliable.
- [x] Add fixture-backed doctor tests for common adopter repositories, including
      ordinary direct-base development, rulesets, missing workflows, drifted
      trusted checkout, unsupported queues, and insufficient token permissions.
- [x] Align `baton adopt`, the bundled skill, and user flows so adoption does not
      finish while doctor is blocked.

Exit criteria: a maintainer can use doctor to decide whether Baton is safe to
enable in an existing project, not merely whether local files and refs exist.

## Slice 10: Consolidate Documentation And Release Classification

- [x] Update CLI, config, output, requirements, architecture, skill, interactive
      docs, and generated guidance from one ownership vocabulary.
- [x] Add an adopter migration note explaining unmanaged PR behavior, reserved
      work-prefix intent, base-to-staging sync, delivery records, explicit issue
      closure, doctor blockers, and any removed config fields.
- [x] Add golden JSON/TOON fixtures for changed public results and preserve only
      compatibility paths with an explicit staged-removal need.
- [x] Audit the per-slice Release Please classifications for config, JSON,
      exit-code, and generated-workflow changes. Do not defer a breaking-change
      decision to this final slice or edit generated release artifacts manually.
- [x] Remove obsolete promotion-history code, tests, docs, and config after the
      replacement contracts are validated.

Exit criteria: adopters have one documented migration path and the repository
contains no active docs describing branch-target ownership or ancestry-derived
delivery as current behavior.

## Completion Record

The ten slice definitions above are the durable completion record. Per-run
progress notes and stale next actions were removed after completion; detailed
design rationale remains in the linked ADRs and public contract documents.

## Scope Exclusions

- Making the staging branch exclusive to Baton.
- Managing non-form issues or unprefixed ordinary PRs.
- Automatic PR merge, staging rewrite, or base-to-staging synchronization.
- Worktree leasing, execution scheduling, or cleanup.
- A hosted database or generic forge abstraction without a second real adopter.
- Silently weakening required checks to accommodate unsupported merge queues.

## Validation Strategy

At each slice boundary:

```sh
gofmt -w <changed-go-files>
go vet ./...
go test ./...
git diff --check
```

Run `go test -race ./...` for concurrent acquisition or transition changes and
the repository-pinned Staticcheck command before completing the goal. Live
GitHub tests remain behind an explicit environment gate.

Public-contract or generated-workflow slices also require golden fixtures,
semantic template tests, adopter documentation, and an explicit SemVer review.
Mutation slices require pure planner coverage, stale-precondition tests,
idempotent retry tests, and structured partial-operation results.

## Per-Slice Operating Rules

- Start with the first incomplete slice and keep this file current.
- Reproduce collision behavior with fixture or E2E-style evidence before fixing
  it.
- Preserve unrelated working-tree changes.
- Run the implementation-review path of `$code-review` after every slice; use
  the diet lens for new records, config, commands, or compatibility paths.
- Use review subagents for cross-module ownership, public contracts, GitHub
  mutation safety, workflow security, and delivery-ledger design.
- Apply obvious safe review fixes, revalidate, and record larger decisions here.
- Do not stage, commit, push, merge, release, or mutate a live adopter
  repository unless the user explicitly asks.

## Files To Inspect First

- `internal/policy/pr.go` and `internal/policy/issue.go` — current ownership and
  policy assumptions.
- `internal/workflow/pull_request_policy.go` and
  `internal/workflow/pull_request_transition.go` — fact acquisition and trusted
  transition orchestration.
- `internal/workflow/repository_facts.go`, `internal/queue/queue.go`, and
  `internal/workitem/readiness.go` — recommendation and delivery-ledger
  coupling.
- `internal/gh/delivery.go`, `internal/gh/pull_request_facts.go`, and
  `internal/gh/read.go` — bounded delivery, branch-rule, event, and transport
  facts.
- `internal/install/render.go`, `internal/install/install.go`, and
  `internal/install/templates/.github/workflows/` — installed collision surface.
- `internal/doctor/doctor.go` and `internal/config/config.go` — adoption gates
  and strict policy loading.
- `docs/adr/`, `docs/CONFIG_SPEC.md`, `docs/CLI_SPEC.md`,
  `docs/OUTPUT_SPEC.md`, and `skills/baton/` — durable decisions and public
  contracts.
