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
- GitHub API foundations are implemented for issue-policy apply, PR policy
  issue/commit enrichment, and label sync.
- Read-only queue inspection is implemented with GitHub issue/PR/check/review
  fetching, staging branch health, and a pure next-action classifier. Live
  GitHub validation against Creo succeeds for `doctor`, `queue`, `prs`, and
  `next`; the current Creo queue has no open PR follow-up case to validate that
  ordering live.
- Native worktree leasing is implemented with lease records, branch collision
  protection, dirty release refusal, listing, prune dry-run, and conservative
  prune cleanup behind `--yes`.
- `baton doctor` and `baton complete` are implemented. `complete` records local
  metadata by default and can post a GitHub issue/PR comment only with explicit
  `--comment`.
- Legacy Creo config migration is implemented with `baton migrate-config`.
- Install templates can render a caller-provided trusted Baton install target
  with `baton init --go-install` or a full command with
  `baton init --install-command`.
- The README now reflects the implemented CLI and documents the trusted install
  path required before consuming repositories switch workflows to Baton.
- Read-only queue and PR inspection commands now document and accept explicit
  `--config` paths, matching the automation command contract.
- Installed target-repo templates now state that they are Baton-managed and
  editable in the consuming repository.

## Phase 0 - Repository Scaffold

Goal: create a minimal Go project without implementing policy behavior yet.

Tasks:

- [x] Initialize Go module.
- [x] Add CLI entrypoint stub.
- [x] Add test harness.
- [x] Add config and fixture directories.
- [x] Add CI only after local commands exist.

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

## Phase 2 - Installable Templates

Goal: make Baton usable in another repo without copying scripts.

Tasks:

- [x] Add templates for GitHub workflows, issue template, labels, policy config,
  and issue workflow doc.
- [x] Implement `baton init --dry-run`.
- [x] Implement `baton init --apply`.
- [x] Implement `baton sync-labels`.
- [x] Implement `baton ensure-branch`.

Acceptance:

- [x] A test repo can be initialized.
- [x] Dry-run output is understandable.
- [x] Existing files are not overwritten without explicit confirmation.

## Phase 3 - Read-Only Triage

Goal: make Baton able to tell Codex what needs attention.

Tasks:

- [x] Implement GitHub auth/repo resolution.
- [x] Implement open issue listing and eligibility reasons.
- [x] Implement open PR listing for staging branch.
- [x] Implement check rollup fetching.
- [x] Implement review-thread GraphQL fetching.
- [x] Implement `baton queue --json`.
- [x] Implement `baton prs --json`.
- [x] Implement `baton pr <number> --json`.
- [x] Implement `baton next --json`.

Acceptance:

- [ ] In Creo, Baton identifies open PR follow-up before issue intake.
- [x] Baton can show resolved vs unresolved review threads.
- [x] Baton can report failing checks with detail URLs.
- [x] `next` explains skipped eligible issues that already have active PRs.

Remaining:

- Publish Baton at `github.com/sjunepark/baton` or provide an equivalent
  trusted binary install path before changing Creo workflows. This local Baton
  checkout still has no configured Git remote, so the default `go install` path
  is not yet usable in GitHub Actions until the repository exists remotely.
- Live-validate PR follow-up precedence against Creo when an open agent PR
  exists.

## Phase 4 - Worktree Leasing

Goal: prevent overlapping automations from sharing one checkout.

Tasks:

- [x] Implement native worktree lease backend.
- [x] Implement lease records.
- [x] Implement acquire/release/list/prune dry-run.
- [x] Add dirty and in-use detection.
- [x] Add branch collision checks.
- [x] Return lease JSON for Codex.

Acceptance:

- [x] Two concurrent attempts to lease the same branch cannot both succeed.
- [x] Dirty managed worktrees are not reused.
- [x] Release refuses dirty worktrees by default.
- [x] User's original checkout branch is never changed.

## Phase 5 - Skill Package

Goal: ship a Baton skill that Codex can use in automations.

Tasks:

- [x] Create `skills/baton/SKILL.md`.
- [x] Add concise command and JSON references only if needed.
- [x] Validate skill metadata.
- [x] Test the skill manually on Creo queue inspection.

Acceptance:

- [ ] A fresh Codex session can use the skill to run one safe read-only triage.
- [x] A fresh Codex session can acquire a lease before editing.
- [x] Skill does not duplicate long CLI docs.

Remaining:

- Live-test the skill in a fresh Codex session against Creo after Baton has a
  usable install path.

## Phase 6 - Creo Migration

Goal: consume Baton from Creo.

Tasks:

- [x] Keep current Creo policy files initially.
- [ ] Point Creo workflows to Baton commands.
- [ ] Run policy checks in CI.
- [ ] Update Codex automations to invoke Baton.
- [ ] Add PR follow-up automation.
- [ ] Remove old JS scripts after trial success.

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

- [x] GitHub issue label read/write in a test repo;
- [x] PR review-thread fetch in a test repo;
- [x] check rollup fetch in a test repo.

Live integration env gates:

- Set `BATON_LIVE_GITHUB=1` and `BATON_LIVE_GITHUB_REPO=owner/name`.
- Set `BATON_LIVE_GITHUB_WRITE=1`, `BATON_LIVE_GITHUB_ISSUE=<number>`, and
  optionally `BATON_LIVE_GITHUB_LABEL=<label>` for issue label write tests.
- Set `BATON_LIVE_GITHUB_PR=<number>` for PR check/review-thread tests.
- Set `BATON_LIVE_GITHUB_BRANCH=<branch>` for branch health tests; defaults to
  `agent`.

## Open Decisions

- Resolved: Baton uses `GITHUB_TOKEN`/`GH_TOKEN` first and falls back to
  `gh auth token` when available.
- Resolved: defer Treehouse until native leasing is proven in real automation.
- Resolved: `baton complete` records local metadata by default; GitHub comments
  are opt-in with `--comment --repo owner/name --issue N|--pr N`.
- Resolved: leave GitHub Projects as a human scheduling view in v1; Baton uses
  issues, PRs, labels, checks, reviews, and local leases as its policy surface.

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
- Added GitHub REST client foundations for `issue-policy --apply`,
  `pr-policy --event` issue/commit enrichment, and `sync-labels`.
- Validation: `go test ./...` covers GitHub client request sequencing, commit
  pagination cap detection, label sync planning, and existing policy parity.
- Added real `ensure-branch` git ref inspection and explicit `--apply` execution
  using the tested non-destructive branch planner.
- Validation: `go test ./...` covers a temp local repo plus bare remote where
  Baton publishes `agent` from `origin/main`; `go run ./cmd/baton
  ensure-branch --remote-base <sha> --json` passes.
- Added read-only GitHub issue/PR/check/review-thread fetching, queue
  eligibility classification, and `next` recommendation.
- Validation: `go test ./...` covers queue classification and existing GitHub
  client behavior; `go run ./cmd/baton --help` passes. Live GitHub validation
  remains pending.
- Added native worktree lease acquire/release/list/prune-dry-run with persisted
  JSON records, branch ownership checks, acquire locking, and dirty release
  refusal.
- Validation: `go test ./...` covers lease acquisition into a temp git worktree,
  branch collision refusal, dirty release refusal, keep-dirty release, and prune
  dry-run candidates.
- Added bundled `skills/baton` package with a concise workflow and compact
  command/JSON references.
- Validation: skill metadata is present in `skills/baton/SKILL.md`; full repo
  validation remains `go test ./...`.
- Added `baton doctor` readiness checks and local `baton complete` completion
  metadata recording.
- Validation: `go test ./...`, `baton doctor --json`, and `baton complete
  --summary ... --json` pass. `doctor` currently reports expected warnings for
  missing repo-local Baton config, missing origin remote, and missing
  `GITHUB_TOKEN`/`GH_TOKEN` in this repository.
- Added repository CI for `go test ./...`.
- Added conservative `baton prune --yes` cleanup for clean Baton-managed
  worktrees. Dirty candidates and active leases with a live owner process are
  skipped and reported.
- Validation: `go test ./...` covers prune removal, dirty skip, released
  records, and existing lease behavior.
- Fixed queue classification for `agent:investigate-only` issues so `next`
  returns `issue-investigation` when no PR follow-up or implementation issue is
  available.
- Validation: `go test ./...`; live read-only validation in Creo with GitHub
  auth shows `baton doctor --json` clean, `baton queue --json` listing current
  issues, `baton prs --json` with no open staging PRs, and `baton next --json`
  selecting issue #5 for investigation.
- Added `baton migrate-config` to convert legacy
  `.github/agent-issue-policy.yml` into `.github/baton.yml` with dry-run/apply
  and overwrite protection.
- Validation: `go test ./...`, dry-run migration from Creo's legacy config,
  and apply migration in a temporary directory pass.
- Added explicit opt-in GitHub comments for `baton complete --comment` on issue
  or PR timelines.
- Validation: `go test ./...` covers completion comment body formatting and the
  GitHub issue comment REST endpoint.
- Added staging branch health fetching and `branch-health` next-action
  precedence before issue intake.
- Validation: `go test ./...` covers branch-health classification and GitHub
  ref/check fetching; live `baton next --json --repo open-creo/creo` still
  selects issue #5 for investigation because Creo's current staging branch is
  not red.
- Added GitHub auth fallback through `gh auth token` after env tokens.
- Validation: `go test ./...`; live Creo `baton doctor --json` reports
  `github-auth: ok (gh auth token)`, and `baton next --json --repo
  open-creo/creo` works without `GITHUB_TOKEN`/`GH_TOKEN`.
- Updated the Baton skill command reference for config migration and explicit
  completion comments.
- Recorded Phase 6 prerequisite: this local Baton checkout has no Git remote or
  published install path, so changing Creo GitHub Actions now would create a
  broken `go install` step.
- Added `baton init --go-install` so generated workflows can use a published
  trusted Baton module/version instead of the default placeholder path.
- Validation: `go test ./...` covers rendering the custom install target into
  generated workflow files.
- Added `baton init --install-command` for a full trusted Baton install command,
  including multi-line commands rendered safely into workflow YAML.
- Validation: `go test ./...` covers custom multi-line install command
  rendering.
- Hardened queue JSON so empty `linkedPrs` is emitted as `[]` instead of
  `null`.
- Validation: `go test ./...` covers the empty-array contract.
- Corrected the Go module and default workflow install path to
  `github.com/sjunepark/baton`, matching the authenticated GitHub owner.
- Validation: `go test ./...` passes after the module/import rewrite.
- Added `baton lease --repo` while preserving `--repo-name` as a compatibility
  alias for lease metadata.
- Validation: `go test ./...` passes.
- Added live-gated GitHub integration tests for issue label read/write,
  check-rollup fetch, review-thread fetch, branch-health fetch, and queue reads.
- Validation: default `go test ./...` skips live tests cleanly; read-only live
  subset against `open-creo/creo` passes for branch health and queue fetch.
- Manually validated the Baton skill's read-only triage workflow against Creo:
  `doctor --json` is clean, `queue --json` reports branch health success and
  eligible investigation issues, `prs --json` reports no open staging PRs, and
  `next --json` selects issue #5 for investigation.
- Updated the README from planning-only language to current operator guidance,
  including local validation, target-repo initialization, queue inspection,
  leasing, and the trusted install path prerequisite for GitHub Actions.
- Added `--config` support to `pr`, `checks`, and `review-threads` read-only
  commands with CLI regression coverage for bad explicit config paths; aligned
  help text for `queue`, `prs`, and `next`.
- Added managed-but-editable notes to every installed target-repo template with
  regression coverage across the embedded template set.
- Next slice: publish/configure a trusted Baton install path, then start Creo
  migration wiring.
