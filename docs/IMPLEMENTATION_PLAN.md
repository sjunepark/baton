# Implementation Plan

## Current State

- Phase 0 is implemented: Go module, CLI entrypoint, test harness, config and
  fixture directories, help/version smoke path.
- The first Phase 1 policy parity slice is implemented: legacy/Baton config
  loading, issue policy decisions, PR policy decisions, branch setup planner,
  event payload parsing, and label manifest parsing are covered by Go unit tests
  adapted from the Creo reference behavior.
- The first Phase 2 install slice is implemented: Baton can preview or apply
  the standard target-repo template set with overwrite protection.
- GitHub API writes, live PR enrichment, queue inspection, label sync writes,
  and worktree leasing are not implemented yet.

## Phase 0 - Repository Scaffold

Goal: create a minimal Go project without implementing policy behavior yet.

Tasks:

- [x] Initialize Go module.
- [x] Add CLI entrypoint stub.
- [x] Add test harness.
- [x] Add config and fixture directories.
- Add CI only after local commands exist.

Acceptance:

- [x] `go test ./...` passes.
- [x] CLI can print version/help.

## Phase 1 - Policy Parity

Goal: extract current Creo issue and PR policy behavior.

Tasks:

- [x] Port issue policy parser and decision logic.
- [x] Port PR policy parser and decision logic.
- [x] Port branch setup planner.
- [x] Port label manifest parser.
- [x] Copy/adapt Creo tests as Go table tests.
- [x] Add event fixture tests.

Acceptance:

- [x] Go tests match current Creo JS behavior for:
  - issue form detection;
  - required section blocking;
  - controlled label removal;
  - work PR validation;
  - promotion PR validation;
  - noisy commit subject rejection;
  - commit listing cap fail-closed;
  - branch setup plans.

Remaining:

- Add event fixture tests.

## Phase 2 - Installable Templates

Goal: make Baton usable in another repo without copying scripts.

Tasks:

- [x] Add templates for GitHub workflows, issue template, labels, policy config,
  and issue workflow doc.
- [x] Implement `baton init --dry-run`.
- [x] Implement `baton init --apply`.
- Implement `baton sync-labels`.
- Implement `baton ensure-branch`.

Acceptance:

- [x] A test repo can be initialized.
- [x] Dry-run output is understandable.
- [x] Existing files are not overwritten without explicit confirmation.

Remaining:

- Implement GitHub-backed `baton sync-labels`.
- Wire `baton ensure-branch --apply` to real non-destructive git commands.
- Implement GitHub write support for the installed `issue-policy --apply`
  workflow path.

## Phase 3 - Read-Only Triage

Goal: make Baton able to tell Codex what needs attention.

Tasks:

- Implement GitHub auth/repo resolution.
- Implement open issue listing and eligibility reasons.
- Implement open PR listing for staging branch.
- Implement check rollup fetching.
- Implement review-thread GraphQL fetching.
- Implement `baton queue --json`.
- Implement `baton prs --json`.
- Implement `baton pr <number> --json`.
- Implement `baton next --json`.

Acceptance:

- In Creo, Baton identifies open PR follow-up before issue intake.
- Baton can show resolved vs unresolved Greptile/CodeRabbit/human threads.
- Baton can report failing checks with detail URLs.
- `next` explains skipped eligible issues that already have active PRs.

## Phase 4 - Worktree Leasing

Goal: prevent overlapping automations from sharing one checkout.

Tasks:

- Implement native worktree lease backend.
- Implement lease records.
- Implement acquire/release/list/prune dry-run.
- Add dirty and in-use detection.
- Add branch collision checks.
- Return lease JSON for Codex.

Acceptance:

- Two concurrent attempts to lease the same branch cannot both succeed.
- Dirty managed worktrees are not reused.
- Release refuses dirty worktrees by default.
- User's original checkout branch is never changed.

## Phase 5 - Skill Package

Goal: ship a Baton skill that Codex can use in automations.

Tasks:

- Create `skills/baton/SKILL.md`.
- Add concise command and JSON references only if needed.
- Validate skill metadata.
- Test the skill manually on Creo queue inspection.

Acceptance:

- A fresh Codex session can use the skill to run one safe read-only triage.
- A fresh Codex session can acquire a lease before editing.
- Skill does not duplicate long CLI docs.

## Phase 6 - Creo Migration

Goal: consume Baton from Creo.

Tasks:

- Keep current Creo policy files initially.
- Point Creo workflows to Baton commands.
- Run policy checks in CI.
- Update Codex automations to invoke Baton.
- Add PR follow-up automation.
- Remove old JS scripts after trial success.

Acceptance:

- Creo issue policy still applies labels correctly.
- Creo PR policy still fails/passes as before.
- Existing open PRs are discoverable by `baton next`.
- Codex automations operate in Baton leases.

## Validation Matrix

Unit:

- config validation;
- issue policy decisions;
- PR policy decisions;
- reference/closing keyword extraction;
- branch setup plans;
- lease planner.

Integration, local:

- worktree acquire/release in a temp git repo;
- init dry-run against temp repo;
- template install in temp repo.

Integration, live gated:

- GitHub issue label read/write in a test repo;
- PR review-thread fetch in a test repo;
- check rollup fetch in a test repo.

## Open Decisions

- Whether to use GitHub CLI for auth fallback or implement token/env auth only
  in v1.
- Whether to support Treehouse as a v1 backend or defer until native leasing is
  proven.
- Whether `baton complete` should write GitHub comments directly or only produce
  suggested comment text in v1.
- Whether Baton should manage GitHub Project fields or leave Projects as a
  human scheduling view in v1.

## Progress Log

### 2026-06-22

- Initialized Go implementation with a `cmd/baton` CLI, config loader, pure
  policy engines, branch planner, label manifest parser, and Go tests.
- Validation: `go test ./...`, `go run ./cmd/baton --help`, and
  `go run ./cmd/baton version` pass.
- Added GitHub issue/PR event payload parsing, `baton init --dry-run`,
  `baton init --apply`, and embedded target-repo templates.
- Validation: `go test ./...`, `go run ./cmd/baton init --dry-run --json`,
  event-based `issue-policy --json`, and event-based `pr-policy --json` pass.
- Next slice: implement GitHub write/client foundations for `issue-policy
  --apply`, `pr-policy --event` issue/commit enrichment, and `sync-labels`.
