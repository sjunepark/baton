# Baton v0.7 issue-task redesign

## Purpose

Redesign Baton as a lightweight GitHub issue and task manager. Baton should
help people and agents create, explicitly enroll, classify, prioritize,
inspect, start, stop, and close tasks without prescribing how a project
branches, creates pull requests, runs CI, reviews changes, merges, or delivers
work.

This plan is the authoritative handoff for the v0.7 redesign. It supersedes
the active direction in `docs/IMPLEMENTATION_PLAN.md` and
`docs/NEXT_SESSION.md`, as well as the downstream-specific direction in
`docs/GOAL-CODA-INTEGRATION-REDESIGN.md`. Those files describe v0.6 and should
be replaced, deleted, or archived during the applicable milestone rather than
used as v0.7 requirements.

## Current state

- v0.6 is released and the working tree was clean before this planning-only
  handoff.
- The current CLI exposes issue, PR, branch, check, review, adoption, and
  sealed-delivery orchestration through 20 top-level commands.
- Target repositories can contain four Baton workflows, `.github/baton.yml`,
  a label manifest, an issue template, generated guidance, required checks,
  a staging branch convention, and an optional delivery ledger.
- The cited adopter failure showed that PR policy can reject project work that
  the user did not intend Baton to manage.
- The current `snapshot` -> `queue` -> `workflow` stack is shaped as a
  dispatcher: it exposes Candidates, Recommendations, action tiers,
  acquisition windows, completeness, deferred work, and execution
  instructions that a standalone issue manager does not need.
- Config and label manifests are the only runtime users of YAML. Removing
  active repository config allows the v0.7 CLI to drop that dependency.
- The first M1/M2 implementation slice now exists in `internal/task`: the
  canonical Task model, fixed-label classification, singular next selection,
  one lifecycle planner, an issue-store seam, and an in-memory adapter. The
  legacy CLI/runtime remains active until the later cutover and M3 deletion.

## Confirmed product decisions

- A **Task** is an open or closed GitHub issue explicitly enrolled in Baton.
- `baton:managed` is the complete enrollment fact. Labels do not require a
  hidden ownership comment, digest, issue-form fingerprint, or body schema.
- Baton never edits an existing issue body. Comments are optional,
  human-readable explanations and never authoritative state.
- Keep the useful eligibility labels `agent:ready-trivial`,
  `agent:ready-bounded`, and `agent:investigate-only`.
- Keep priority and blocker labels where useful. `needs:discussion` and
  `needs-info` are blocker reasons, not delivery states.
- `baton:in-progress` is advisory. `start` does not claim a task atomically,
  create a session, acquire a lease, or guarantee exclusivity across agents.
- Provide a clearing operation for abandoned work and a reversible unenroll
  operation. Do not create stale one-way lifecycle transitions.
- A closed GitHub issue is done. Baton closes only after explicit user or
  command intent and never infers completion from a PR, commit, check, branch,
  or delivery event.
- The CLI remains non-interactive. After project-owned implementation work,
  the skill may ask whether to invoke `close`; when it cannot ask, it leaves
  the Task open.
- Normal Baton use is setup-free, checkout-independent, and works with an
  explicit repository plus GitHub credentials. v0.7 has no active repository
  config or `--config` flag.
- Project-specific implementation and delivery behavior comes from the
  project's own instructions and tools, not Baton.
- Baton has no downstream-orchestrator integration or migration
  responsibility. Its public outputs describe Baton's Task domain only; do
  not add adapters, fixtures, projections, or documentation for a named
  external tool.
- Baton exposes Tasks, not Candidates, Recommendations, Actions, Runs,
  acquisition snapshots, or execution instructions. `next` returns one
  deterministic ready Task or no Task.
- v0.7 is a clean breaking line. Do not retain a legacy orchestration mode,
  no-op compatibility commands, or old JSON projections inside the new core.

## Optional project guidance

v0.7 does not install or monitor an issue-policy workflow:

- Explicit lifecycle commands lazily create the fixed Baton label they need,
  so there is no setup or readiness command.
- The bundled skill and docs may show an optional issue-template example and
  how to enroll/classify a manually or externally created issue.
- Projects copy or adapt that guidance themselves. Baton does not install,
  pin, validate, update, remove, or measure drift for it.
- Baton does not maintain a policy comment. Agents may leave ordinary
  human-readable comments, but comments never affect Task state.
- Background issue validation can be reconsidered only after demonstrated
  demand; it is not part of v0.7.

## Scope

### In scope

- One canonical Task model and one public output vocabulary shaped only by
  Baton's Task domain.
- Issue list/detail/next selection and explicit lifecycle operations.
- Server-side retrieval and filtering of enrolled GitHub issues.
- Fixed built-in label semantics with lazy creation during explicit mutations.
- Safe v0.6 adopter decommissioning.
- Deletion of branch, PR, review, check, delivery, body-policy, and legacy
  compatibility implementation.
- A short bundled skill and concise active documentation.
- A breaking Release Please-managed v0.7 release.

### Out of scope

- Branch naming, worktrees, commits, PR creation or targeting, CI, review,
  merge, promotion, synchronization, or delivery ledgers.
- Atomic claims, session identifiers, leases, heartbeats, local state, or a
  Baton database.
- Required repository workflows or required repository-local config.
- Repository setup, readiness dashboards, managed issue templates, or issue
  validation workflows.
- Downstream-orchestrator adapters, compatibility projections, fixtures, or
  migration work.
- Candidate sets, action tiers, degraded snapshots, in-band execution
  instructions, or generic operation-report protocols.
- GitHub Projects fields, project boards, non-GitHub forges, or a web UI.
- Automatic issue closure or destructive cleanup of adopter branches, issues,
  comments, labels, environments, or ledgers.

## Plan map

- [Product and public contract](docs/plans/baton-v0.7/01-product-contract.md)
  defines the domain language, label semantics, command capabilities, output
  model, optional project guidance, and compatibility boundary.
- [Task core and CLI](docs/plans/baton-v0.7/02-task-core-cli.md) defines the Go
  module seam, GitHub adapter, commands, planners, tests, and runtime deletion.
- [Adopter decommission and migration](docs/plans/baton-v0.7/03-adopter-migration.md)
  defines safe removal of v0.6 repository coupling.
- [Skill, documentation, and release](docs/plans/baton-v0.7/04-skill-docs-release.md)
  defines the human/agent workflow, documentation collapse, distribution,
  validation, and Release Please handoff.

## Milestones

- [ ] **M1 — Freeze the issue-only contract.** Complete the domain, interface,
  output, fixed-label, and contract-test work in
  `01-product-contract.md`.
- [ ] **M2 — Deliver the setup-free Task CLI.** Complete the Task module,
  adapters, lifecycle commands, and validation in
  `02-task-core-cli.md`.
- [ ] **M3 — Delete orchestration and old contracts.** Remove the runtime
  branch/PR/delivery/body-policy paths and legacy projections after
  preserving the exact v0.6 evidence required by migration fixtures.
- [ ] **M4 — Decommission v0.6 adopters safely.** Complete the deterministic
  audit, reviewed removal plan, and non-destructive validation in
  `03-adopter-migration.md`.
- [ ] **M5 — Align skill, docs, and release.** Complete the skill rewrite,
  documentation collapse, distribution validation, and Release Please handoff
  in `04-skill-docs-release.md`.

## Cross-cutting invariants

- Keep deterministic GitHub facts and state transitions in Go; keep semantic
  classification suggestions in the skill and project implementation judgment
  in the project's own instructions.
- Every GitHub mutation has a pure planner or dry-run path, explicit intent,
  structured output, and idempotent already-satisfied behavior.
- An explicit mutation verb applies by default and supports a uniform
  `--dry-run`; do not require a second generic `--apply` confirmation.
- Explicit lifecycle mutations may create the missing default Baton label
  they require; they do not require a prior setup command or create arbitrary
  project taxonomy.
- Use a typed GitHub client. Do not scrape human `gh` output for core facts.
- Keep one Task payload and render human text or JSON at the CLI edge. Do not
  maintain TOON, field-selection, or compatibility renderers.
- CLI commands call the Task module directly. Do not replace `workflow` with a
  new pass-through facade or retain a generic operation-report module.
- Reject unknown arguments and flags before network calls. Do not prompt from
  the CLI or leak raw dependency output.
- Core Task commands never write local files, run git mutations, merge a PR,
  or manage a checkout.
- Preserve unrelated working-tree changes and commit meaningful passing slices
  incrementally during goal execution.

## Validation

- Run `gofmt` on changed Go files.
- Run `go vet ./...` and `go test ./...` after coherent code/contract slices.
- Run the repository's pinned staticcheck command before completing a major
  milestone.
- Exercise CLI help, JSON, definitive empty states, singular next selection,
  idempotent mutations, and unknown-flag errors in tests.
- Use live GitHub integration tests only behind existing explicit environment
  gates.
- Run the repository-required implementation review after each substantial
  slice and apply safe findings before continuing.

## Next implementation slice

The first substantial goal should complete M1 through M3: freeze the contract,
deliver the setup-free issue-only Task CLI, and delete the runtime
orchestration/compatibility path. Stop before changing external adopter
repositories or preparing the release PR; those require the new CLI contract
to be stable first. Delete downstream-specific Baton artifacts during M3;
adapting or validating a downstream orchestrator does not belong to any Baton
milestone.

## Open questions

No product decision blocks implementation. The public command and result
shapes are fixed in `01-product-contract.md`; implementation naming inside the
Task module may change without adding public concepts.

## Progress log

- **2026-07-16 — Task core seam:** Added the issue-only Task domain and tested
  lifecycle precedence, priority selection across modes, bounded detail,
  dry-run/apply parity, idempotent lifecycle operations, lazy fixed-label
  creation, and confirmed partial failures. `go vet ./...`, `go test ./...`,
  and `go test -race ./internal/task` pass. Next: preserve bounded v0.6
  decommission evidence, then add the production GitHub issue adapter.
