# Goal: Coda-Informed Baton Redesign

Status: Complete — Slices 1-8 are implemented, reviewed, and validated.

Historical note: this goal records the v0.5 redesign baseline. Current public
contracts are `nextCandidates` v3, `queueSnapshot` v2, and
`repositorySnapshot` v2; current ownership and delivery behavior is documented
in [the v0.6.0 adopter update](adopter-updates/v0.6.0.md). Statements below are
preserved as the state and decisions of their original slices, not current
runtime authority.

## Current State

- At the time of this historical goal, Coda's adapter and UI contracts were
  frozen in producer-backed golden
  fixtures for `nextCandidates` v2, `queueSnapshot` v1, structured error v1,
  and issue, pull-request, branch, tied-selection, and no-work variants.
- Queue-backed commands (`queue`, `prs`, and `next`) now resolve config, git
  root, configured remote, branch policy, and GitHub identity as one context.
  Conflicting flag, environment, or remote identities fail before GitHub I/O.
- Repository errors redact remote credentials; invalid API-path identities fail
  closed; remote/API hosts must agree; non-repository git failures retain the
  `localGit` exit category.
- All command orchestration now runs behind workflow or focused domain seams;
  the CLI parses, dispatches, and renders. Repository-file operations,
  transition, branch planning/apply, home, doctor, and PR dashboard composition
  no longer depend on CLI-owned I/O, clocks, clients, or global working state.
- Event, flag, and environment repository identities are cross-checked before
  GitHub clients or writes. Issue-policy comment reads also complete before its
  first mutation. GitHub transport returns neutral facts; credentials, git
  execution, and structured error rendering have one implementation each.
- Slice 5 separates strict current/legacy YAML wires from one compiled
  Repository Policy. Unknown fields, unsupported versions, invalid Git refs,
  unstable markers, duplicate controlled labels, mapping gaps, and managed
  path collisions fail closed; obsolete automation keys are migration-only and
  are no longer emitted or used at runtime.
- Slice 3 now threads one caller-cancellable, 30-second default bound through
  CLI workflows, credential discovery, GitHub HTTP/GraphQL, repository
  resolution, and git commands. Current REST and GraphQL collections paginate;
  check/status and issue-policy reads cannot hide failures or markers after
  item 100.
- PR check enrichment uses four bounded workers and preserves source order.
  GitHub primary/secondary rate limits retain reset/retry metadata. Review
  actors use GraphQL actor types and exact known bot identities, so bot-like
  human logins remain human and unavailable actor types remain explicit.
- Slice 3 adds a neutral, head-bound acquisition model for effective branch
  rules, app-specific required checks, submitted and requested reviews, review
  threads, mergeability, branch revisions, and final revision verification.
  Missing, truncated, rate-limited, unknown, or stale facts degrade the result;
  `next` refuses to project a recommendation from that acquisition.
- GitHub's successful 250-commit listing cap is distinct from Baton deadlines,
  cancellation, or page failures, which return an error and no partial commit
  listing. Check-run acquisition distinguishes an exact 1,000-result response
  from an upstream result that reports additional capped entries.
- Required checks preserve GitHub App identity and conservatively combine a
  check run and commit status with the same required context. Neutral and
  skipped conclusions use GitHub's successful-required-check semantics.
- Slice 4 adds `repositorySnapshot` v1 as the preferred one-acquisition
  observation. Typed Outcome, Action, and revision-bound Candidate identities
  distinguish advice from execution; degraded facts cannot imply an Action.
- Recommendation policy now uses required checks, classic protection plus
  rulesets, reviews, threads, draft state, and mergeability. App-specific check
  identities win over duplicate generic contexts while independently required
  App identities remain distinct.
- `queueSnapshot` v1 and `nextCandidates` v2 are pure compatibility projections
  from the same snapshot workflow. Coda's old fixtures remain maintained, and
  producer-backed v1 fixtures cover issue, PR, branch, degraded, tied-choice,
  and single-ready-PR human-disposition shapes.
- Slice 4 is a compatible feature addition (`feat:` / SemVer minor). Removing
  the legacy projections remains a separately reviewed future major change.
- Built-in and installed policy now agree on `needs-info`. Config, labels,
  issue forms, workflow guidance, trusted policy workflows, and command
  defaults render or resolve from the compiled policy, including custom
  remotes, branches, prefixes, headings, labels, and manifest paths.
- Repository-file reconciliation v2 returns exact content, full diffs,
  ownership, conflicts, digested preconditions, stable plan identity, and
  per-file outcomes. Apply preflights the whole change set, rejects symlinks
  and duplicate targets, stages safely, and atomically replaces each file.
- One `operationReport` v1 now covers repository files, labels, issue policy,
  work-item transition, and branch reconciliation. Partial effects
  survive in the single structured error object; retryability reflects typed
  upstream metadata. Label, issue, file, and branch applies reject stale state,
  while issue retries accept only idempotent progress already authorized by
  the reviewed decision.
- Slice 5 changes the public repository reconciliation plan from v1 to v2 and
  is therefore a deliberate breaking public change (`feat!:`). Under Baton's
  configured pre-1.0 Release Please policy, that breaking change advances the
  minor version. Additive operation reports on existing result/error contracts
  remain compatible additions.
- Slice 6 defines GitHub-authoritative Work Item State separately from Agent
  Mode and routes queue, recommendations, and PR dashboards through one
  classifier. Open work PRs are active; merged staging work is awaiting review;
  other gates are blocked; closed issues are terminal.
- Merged work PR events now produce deterministic `workItemTransitionPlan` v4
  operations. `pr-transition` requires an explicit dry-run/apply gate, verifies
  current PR content and revision plus every referenced issue's durable Baton
  ownership before its first write, skips already-satisfied or closed issues,
  and preserves partial effects in `operationReport` v1.
- Generated transition automation checks out trusted base code, grants only
  issue-write plus read permissions, and installs an exact released Baton
  SemVer target. Arbitrary install-command overrides cannot reach this mutation
  workflow.
- Merged staging-PR history is a conservative fail-safe if event delivery or
  label mutation fails. History acquisition failure degrades snapshots and
  blocks recommendations; an issue with historical staged work is not
  re-admitted merely by removing its awaiting-review label.
- Slice 7 removes the released `baton complete` command, completion JSON v1,
  and the write-only local completion ledger. Maintained Coda does not invoke
  them and continues to own terminal Run state; agents return summaries and
  validation through the caller while GitHub owns work-item completion.
- Global CLI usage now renders from the same ordered command-help catalog as
  subcommand help. The bundled skill, active docs, requirements, architecture,
  interactive reference, and v0.5.0 adopter migration note use one ownership
  vocabulary and no longer instruct agents to create duplicate completion
  state.
- The combined public changes are breaking. Release Please ownership is
  verified; use `feat!:` with a `BREAKING CHANGE:` footer. Baton's configured
  pre-1.0 policy advances the current v0.4.4 release line to v0.5.0 rather than
  v1.0.0. Release Please retains ownership of generated version and changelog
  artifacts.
- Slice 8 confirms maintained Coda records Baton recommendations only as
  display/enrichment. Run creation remains a separate explicit Job trigger,
  Baton refresh after execution is enrichment, and one-active-Run-per-Job is
  execution protection rather than a repository Candidate claim.
- Candidate claims remain deliberately unimplemented. Recommendations are
  advisory, current unattended dispatch must be singleton per repository, and
  an automatic dispatcher must exist before Baton evaluates official GitHub
  conditional-mutation primitives or promises a shared claim contract.
- Current validation passes `go vet ./...`, `go test ./...`, `go test -race
  ./...`, pinned Staticcheck v0.7.0, Coda's 14 adapter/UI tests, and `git diff
  --check`. Existing Coda JSON projections remain compatible.

Work one slice at a time. Update this file in place: check completed work,
record current decisions and blockers, and replace stale next actions instead of
appending session logs.

## Objective

Make Baton a trustworthy GitHub policy, repository-observation, recommendation,
and adoption tool with a stable CLI JSON interface for Coda and other callers.
Preserve Baton's well-tested deterministic modules while rebuilding the runtime
seams around them.

The completed system must return complete or explicitly degraded facts, never
mistake a recommendation for execution state, keep target-repository config as
the actual policy source of truth, prevent completed work from re-entering the
queue, and make every mutation reviewable before it is applied.

## Ownership And Language

- Coda owns Projects, Jobs, Runs, worktree leases, process execution, retries,
  cancellation, provenance, Artifacts, and Attention Items.
- Baton owns GitHub fact acquisition, policy decisions, repository
  recommendations, target-repository reconciliation, and any future
  candidate-level coordination protocol.
- GitHub owns live issue and pull-request state and semantic work completion.
- Baton emits snapshots, recommendations, outcomes, actions, candidates,
  reasons, plans, and operation results.
- Do not use Coda's Job, Run, Progress, Artifact, or Attention terms for Baton
  state.

## Confirmed Direction

- Keep Go and the single-binary CLI.
- Preserve the pure issue-policy, PR-policy, queue, branch-plan, and label-plan
  logic unless a correctness fix requires changing it.
- Keep Coda integration behind versioned CLI JSON. Do not import either
  repository's internals into the other.
- Add a single repository snapshot contract; preserve `nextCandidates` v2 and
  `queueSnapshot` v1 as compatibility projections during Coda migration.
- Separate recommendation outcome from requested action.
- Keep checkout lifecycle, scheduling, process management, and execution
  ledgers outside Baton.
- Treat target repository root and GitHub repository identity as one validated
  context.
- Prefer pure planners and explicit preconditions for every mutation.
- Remove speculative configuration and local state that does not pass the
  deletion test.
- Do not rewrite in another language or add a CLI framework merely to split
  files.

## Required Invariants

1. A repository snapshot represents one bounded acquisition and includes its
   repository identity, acquisition time, completeness, and warnings.
2. Recommendation outcome is distinct from action. Waiting, human choice,
   blocked, idle, and degraded states cannot imply mutation.
3. Every candidate has a stable typed identity: repository plus issue, pull
   request, or branch identity and relevant revision facts.
4. Missing, truncated, stale, or failed GitHub facts are explicit; Baton never
   silently classifies incomplete facts as safe success.
5. GitHub work merged to the staging branch cannot become implementation-ready
   again while awaiting promotion or human review.
6. Target-repository config is strictly decoded, fully validated, and compiled
   once before policy or reconciliation behavior uses it.
7. Generated workflows, issue forms, labels, guidance, and command defaults
   derive from the same compiled repository model or are intentionally fixed.
8. Dry-run output contains enough content or diff information to review the
   exact mutation.
9. Apply validates all consent, conflicts, and plan preconditions before the
   first write; partial external effects are returned as structured outcomes.
10. Public JSON, exit codes, config, generated files, and CLI flags change only
    through intentional versioned migrations.
11. Baton does not create or manage caller worktrees, execute Codex work, or
    maintain a second Coda-style Run ledger.
12. Candidate claims are not implemented until a real dispatcher needs them
    and the available GitHub coordination primitive has been verified.

## Target Module Shape

Names may be refined, but responsibility must remain concentrated at these
seams:

- CLI module: parse, dispatch, select output format, and render.
- Workflow module: resolve repository context, orchestrate one use case, and
  return a result or typed error.
- Repository snapshot module: acquire complete GitHub facts and derive one
  typed recommendation.
- Compiled policy module: strictly decode, default, cross-validate, and expose
  runtime policy.
- Reconciliation module: render desired repository resources, compute semantic
  diffs, validate preconditions, and apply an approved plan.
- GitHub adapter: own REST/GraphQL transport, pagination, timeouts, actor
  metadata, upstream error details, and partial operation outcomes.
- Git adapter: inspect repository identity and refs, bind plans to reviewed
  revisions, and apply only current plans.
- Existing pure policy and planning modules remain behind these seams.

Avoid one giant application interface, generic repositories for every I/O
operation, or one shallow command object per CLI command.

## Slice 1: Contract Baseline And Repository Context

Purpose: protect the current Coda integration before changing behavior.

- [x] Add golden fixtures for `nextCandidates` v2, `queueSnapshot` v1,
      structured error v1, and every issue/PR/branch candidate variant Coda
      consumes.
- [x] Add compatibility tests that decode those fixtures through consumer-like
      assertions without importing Coda source.
- [x] Document additive-change rules, version-bump rules, and the migration
      window for public JSON contracts.
- [x] Introduce one resolved repository context containing explicit local root,
      GitHub `owner/name`, config path, remote, and branch policy.
- [x] Validate that an inferred local remote and explicit `--repo` do not
      silently identify different repositories. Define an explicit override
      only if a verified workflow needs it.
- [x] Characterize current CLI text/JSON/TOON and exit behavior before moving
      code.
- [x] Reinspect Coda's `lib/coda/baton.ex` and contract tests before freezing
      fixtures; its repository may have advanced since this plan was written.

Exit criteria:

- Existing Coda fixtures pass unchanged.
- Repository/config mismatch fails with a typed, actionable error.
- No user-visible behavior changes without a fixture or migration decision.

## Slice 2: Deep Workflow Seam And Typed Errors

Purpose: make orchestration testable without flags, global environment, or
output buffers.

- [x] Move config discovery, target resolution, GitHub fetch sequencing,
      dashboard composition, mutation orchestration, and clock use out of
      `internal/cli/cli.go` into workflow modules.
- [x] Keep the CLI module limited to parsing, dispatch, and presentation.
- [x] Stop the GitHub adapter from returning `queue`, `policy`, or `labels`
      implementation types; convert transport facts at the consuming workflow
      seam.
- [x] Centralize credential discovery and git command execution instead of
      duplicating them in home, doctor, CLI, and adapter modules.
- [x] Define typed application and upstream errors with stable category,
      retryability, HTTP status, request ID, retry-after, and safe diagnostic
      details.
- [x] Centralize JSON/text/TOON error rendering while preserving current error
      v1 fixtures.
- [x] Add workflow-level tests using concrete test adapters only where behavior
      actually varies.

Exit criteria:

- CLI tests cover parsing and rendering; workflow tests cover orchestration.
- The GitHub and git adapters no longer own Baton recommendation semantics.
- Existing public contracts remain compatible.

## Slice 3: Complete And Bounded GitHub Facts

Purpose: make every downstream decision depend on complete or explicitly
incomplete facts.

- [x] Thread context and bounded deadlines through GitHub and git operations.
- [x] Paginate check runs, policy comments, issues, pull requests, labels,
      review threads, and any other collection used for policy or selection.
- [x] Preserve commit-listing cap behavior while distinguishing GitHub's cap
      from Baton's own bounded acquisition.
- [x] Replace login substring bot detection with exact identities and GitHub
      actor/type metadata; add adversarial human-login tests.
- [x] Acquire required-check, review-thread, requested-review, mergeability, and
      relevant branch facts needed by recommendation policy.
- [x] Use bounded concurrency for independent per-PR enrichment while
      preserving deterministic output order.
- [x] Represent partial acquisition, unknown facts, rate limiting, and stale
      facts explicitly instead of collapsing them into success or generic
      failure.
- [x] Add over-100-item, timeout, cancellation, rate-limit, and partial-failure
      fixtures.

Exit criteria:

- A failing check or owned policy comment cannot disappear because it was
  beyond the first page.
- Human review priority cannot be inverted by a username substring.
- Recommendation code receives a complete fact set or an explicit degraded
  input.

## Slice 4: Typed Recommendation And Unified Snapshot

Purpose: replace Coda's sequential `next` plus `queue` composition with one
coherent versioned observation.

- [x] Define `repositorySnapshot` schema v1 and record the decision in an ADR.
- [x] Add `baton snapshot --repo owner/name --json` using the resolved local
      repository context.
- [x] Include repository identity, acquisition start/end time, completeness,
      warnings, queue facts, branch/PR facts, and one recommendation.
- [x] Model recommendation outcome separately from action. Initial outcomes:
      `actionable`, `human_choice_required`, `waiting`, `blocked`, `idle`, and
      `degraded`.
- [x] Keep actions typed separately, including issue implementation,
      investigation, PR follow-up, and branch health.
- [x] Replace “every open PR is follow-up” with policy based on required checks,
      review state, and whether agent mutation is actually useful.
- [x] Make pending checks return waiting, green/no-feedback PRs return human or
      idle as appropriate, and incomplete facts return degraded.
- [x] Keep `baton next` v2 and `baton queue` v1 as tested projections from the
      same snapshot implementation during migration.
- [x] Add a Coda adopter note with old/new fixtures and an explicit mapping from
      Baton outcome/action to Coda's external snapshot display.

Exit criteria:

- One Baton invocation supplies the facts Coda currently reads from two.
- A recommendation cannot be mistaken for a running Coda Run.
- Existing Coda contracts remain available until a separately reviewed removal.

## Slice 5: Strict Compiled Policy And Repository Reconciliation

Purpose: make config the real source of policy truth and adoption safe to
review.

- [x] Split current and legacy wire decoding from compiled runtime policy.
- [x] Reject unknown YAML fields, unsupported versions, duplicate controlled
      labels, unmapped implementation labels, invalid section references, and
      unstable or empty policy markers.
- [x] Remove `automation.prefer_pr_followup_before_issue_intake` and
      `automation.allow_merge` unless this goal first defines and tests their
      real semantics.
- [x] Resolve the `needs-info` versus `agent:blocked` default-policy drift and
      assert semantic equality between built-in defaults and installed config.
- [x] Make commands stop embedding `"."`, `origin`, `main`, `agent`, or the
      default labels when compiled policy should supply them.
- [x] Render workflows, issue form, label manifest, workflow guidance, and
      config defaults from one compiled repository model.
- [x] Make custom base/staging branches, work prefix, form headings, labels,
      manifest path, and remote propagate everywhere they are documented.
- [x] Deepen install planning into reconciliation with full content/diff,
      ownership, conflicts, preconditions, and per-file operation results.
- [x] Define one structured operation-result model for all multi-step mutation
      workflows, including label sync, issue policy, and replacement completion
      reporting, so failures preserve already-applied external effects.
- [x] Preflight the entire apply before writing; use atomic per-file replacement
      and report any unavoidable partial external effects.
- [x] Make existing `init`, `migrate-config`, `sync-labels`, and
      `ensure-branch` use the reconcile/planner modules rather than parallel
      mutation paths.
- [x] Bind branch apply plans to reviewed SHAs and fail stale plans before git
      mutation.

Exit criteria:

- Config typos and unsupported schemas fail closed.
- A custom repository model produces internally consistent managed files.
- Dry-run is reviewable, and a later conflict cannot cause earlier files to be
  written before refusal.

## Slice 6: GitHub Work-Item Transitions

Purpose: prevent completed staging work from returning to implementation intake
without giving Baton ownership of Coda Runs.

- [x] Define the GitHub-authoritative work-item states and legal transitions:
      ready, active work PR, awaiting review/on staging, blocked, and promoted or
      closed.
- [x] Keep form-derived work kind and agent mode distinct from workflow state.
- [x] Extract one issue-readiness classifier used by queue snapshots, PR
      dashboards, and recommendations.
- [x] Remove the duplicated readiness logic currently maintained in CLI
      dashboard code.
- [x] Decide and document how a merged work PR moves referenced issues to the
      existing review-blocked state; prefer an event-driven pure planner plus an
      explicitly gated workflow mutation.
- [x] Ensure generated policy workflows execute released, pinned Baton code;
      never execute pull-request-modified repository code for trusted policy
      decisions.
- [x] Handle work PR close-without-merge, reopen, replacement PR, promotion,
      and manual issue closure explicitly.
- [x] Make transition writes idempotent and return planned, applied, skipped,
      and failed operations.
- [x] Add GitHub event fixtures and transition-table tests for every edge.

Exit criteria:

- Merging a work PR to staging cannot make its issue eligible again.
- Queue and PR dashboard readiness cannot disagree.
- Coda Run success remains execution provenance, not semantic issue completion.

## Slice 7: Remove Redundant Completion State And Diet The Surface

Purpose: remove shallow or misleading interfaces after replacement behavior is
available.

- [x] Remove Baton's write-only local completion ledger and its second-resolution
      IDs; do not integrate it with Coda's ledger.
- [x] Replace any semantic use of `baton complete` with the explicit GitHub
      transition/reporting path, and leave execution completion to the caller.
- [x] Update the bundled skill so Coda-owned Runs and caller-owned execution do
      not create duplicate Baton completion state.
- [x] Remove dead compatibility paths only after fixtures and adopter notes show
      that no maintained caller uses them.
- [x] Consolidate command help, public contract references, and generated docs
      around one Baton vocabulary without forcing Coda to share internal terms.
- [x] Update README, architecture, requirements, CLI/config/output/skill specs,
      interactive docs, templates, and adopter notes to describe one current
      system.
- [x] Classify the final public change with Release Please and document every
      Coda migration step before release.

Exit criteria:

- No Baton module writes local state that no maintained command reads.
- Active docs and skill instructions describe the implemented contract only.
- Coda remains compatible through the documented migration path.

## Slice 8: Candidate Claim Decision — Do Not Start Automatically

Stop after Slice 7 and decide from an implemented Coda dispatch design.

- [x] Confirm whether Coda or another maintained caller will automatically turn
      Baton recommendations into Runs.
- [x] If all dispatch remains human-selected and serialized, document that
      constraint and do not add a claim interface.
- [x] If automatic dispatch is accepted, inspect official GitHub capabilities
      for conditional mutation before promising atomic claims.
      Not triggered: no maintained automatic dispatcher is accepted or
      implemented.
- [x] Design claims around repository/candidate identity and snapshot
      preconditions, with holder correlation, expiry, renew, release, and
      idempotent completion semantics.
      Deferred as one public contract prerequisite in ADR 0003; no speculative
      schema or command surface was added.
- [x] Treat Coda's one-active-Run-per-Job rule as execution protection, not a
      repository candidate claim.
- [x] Fail honestly if GitHub cannot provide the required compare-and-set
      semantics; require a singleton dispatcher rather than describing a
      best-effort label as atomic.
      Current policy requires a singleton without claiming GitHub provides CAS.

Exit criteria if claims are implemented:

- Two independent dispatchers cannot both receive a successful claim for the
  same candidate.
- Claim loss or expiry is visible to Coda without corrupting Coda Run history.
- Standalone Baton and Coda use the same public coordination contract.

## Scope Exclusions

- Editing Coda source from this Baton goal. Baton should produce fixtures and
  migration notes; Coda changes belong to a separately authorized Coda slice.
- Worktree leasing, cleanup, or process execution.
- A hosted multi-user coordinator or non-GitHub forge.
- Automatic merge.
- Generic plugin systems or adapters without a second real implementation.
- Maintaining old and new internal execution paths after migration.

## Validation Strategy

At each slice boundary:

```sh
gofmt -w <changed-go-files>
go vet ./...
go test ./...
```

Run `go test -race ./...` for concurrency, context, claim, or bounded parallel
fetch changes. Run the repository-pinned Staticcheck command before completing
the goal. Add live GitHub tests only behind the existing explicit environment
gate.

Public-contract slices also require:

- golden JSON fixtures;
- text and TOON compatibility checks where maintained;
- config/template semantic parity tests;
- Coda consumer fixture tests;
- adopter migration notes;
- a deliberate SemVer/Conventional Commit classification.

## Per-Slice Operating Rules

- Start with the first incomplete slice; do not mix later product changes into
  an earlier correctness slice.
- Reproduce each bug with structured evidence or a failing fixture before
  fixing it.
- Keep this plan current and concise after every slice.
- Preserve unrelated working-tree changes.
- Run the implementation-review path of `$code-review` after every slice;
  include the diet lens for new types, schemas, commands, or compatibility
  paths.
- Use review subagents for public contracts, GitHub mutation safety,
  concurrency, or cross-repository compatibility.
- Apply obvious safe review fixes, revalidate, and record larger findings here.
- Do not stage, commit, push, release, or edit `../coda` unless the user
  explicitly asks.

## Files To Inspect First

- `internal/cli/cli.go` — current orchestration, presentation, and public
  command surface.
- `internal/queue/queue.go` — eligibility and recommendation behavior to
  preserve and correct.
- `internal/gh/client.go` and `internal/gh/read.go` — transport, pagination,
  completeness, and mutation behavior.
- `internal/config/config.go` and `internal/install/install.go` — compiled
  policy and reconciliation starting points.
- `docs/OUTPUT_SPEC.md`, `docs/CONFIG_SPEC.md`, and
  `skills/baton/references/json-contracts.md` — maintained public contract.

## Suggested `/goal` Prompt

```text
Implement docs/GOAL-CODA-INTEGRATION-REDESIGN.md from the first incomplete
slice. Work one slice at a time, keep the plan updated in place, preserve the
existing Coda JSON contracts until their documented migration, reproduce bugs
before fixing them, validate each slice, and run the required code review. Do
not edit ../coda, stage, commit, push, or release unless I explicitly ask.
```
