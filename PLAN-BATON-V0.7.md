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

- M1 through M4 are complete. The public CLI is the setup-free Task surface,
  backed by the canonical Task module and narrow typed GitHub issue adapter.
- Exact v0.5.0, v0.5.1, and v0.6.0 decommission evidence is isolated under
  `testdata/migration/`, with conservative inventories and a required-check-
  first, non-destructive adopter guide.
- M5 is complete through the release handoff. The active docs,
  repository/reviewer instructions, and Release Please inputs describe only
  the issue Task product; retired install templates are deleted.
- Live CLI help is the canonical command reference. The bundled skill contains
  only issue-writing, classification, authorization, and sequencing judgment;
  maintainer-only distribution guidance lives under `docs/`.
- The registered personal skill predates the CLI-first refinement and requires
  the documented refresh before reuse or distribution.
- YAML is no longer a dependency. Repository-wide tests and race detection,
  vet, pinned Staticcheck, tidy/link/fixture/release-input checks, CLI boundary
  checks, and the implementation/diet review pass all pass.
- Release Please published v0.7.0 through PR #26 before final M5 alignment
  landed through PR #27, so the immutable v0.7.0 tag retains retired skill,
  docs, and template surfaces. Corrective v0.7.1 PR #28 and its release were
  published and verified.

## Confirmed product decisions

- A **Task** is an open or closed GitHub issue explicitly enrolled in Baton.
- `baton:managed` is the complete enrollment fact. Labels do not require a
  hidden ownership comment, digest, issue-form fingerprint, or body schema.
- The released v0.5 policy did not assign `baton:managed` by default. Migration
  may use fixed eligibility labels as read-only discovery evidence, but every
  issue lacking the enrollment label requires explicit reviewed enrollment;
  old labels or body fingerprints never authorize automatic enrollment.
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
- Safe adopter decommissioning from the released v0.5.0, v0.5.1, and v0.6.0
  repository surfaces.
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
- Downstream-orchestrator adapters, compatibility projections, or
  downstream-orchestrator migration work.
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
  defines safe removal of v0.5 and v0.6 repository coupling.
- [Skill, documentation, and release](docs/plans/baton-v0.7/04-skill-docs-release.md)
  defines the human/agent workflow, documentation collapse, distribution,
  validation, and Release Please handoff.

## Milestones

- [x] **M1 — Freeze the issue-only contract.** Complete the domain, interface,
  output, fixed-label, and contract-test work in
  `01-product-contract.md`.
- [x] **M2 — Deliver the setup-free Task CLI.** Complete the Task module,
  adapters, lifecycle commands, and validation in
  `02-task-core-cli.md`.
- [x] **M3 — Delete orchestration and old contracts.** Remove the runtime
  branch/PR/delivery/body-policy paths and legacy projections after
  preserving the exact v0.6 evidence required by migration fixtures.
- [x] **M4 — Decommission v0.5 and v0.6 adopters safely.** Complete the
  version-aware audit, reviewed removal and explicit-enrollment plan, and
  non-destructive validation in `03-adopter-migration.md`.
- [x] **M5 — Align skill, docs, and release.** Complete repository alignment,
  refresh the registered personal skill through its owning distribution flow,
  review corrective v0.7.1 PR #28, and verify its published release.

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

No implementation or release handoff work remains for v0.7.1. Release Please
PR #28 was merged after review, and the v0.7.1 tag and GitHub release were
published and verified. Before the refined skill is reused or distributed,
refresh the registered copy through `docs/SKILL_DISTRIBUTION.md`. No next
implementation slice is authorized.

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
- **2026-07-16 — v0.6 evidence freeze:** Preserved eight exact rendered
  v0.6.0 managed files with verified SHA-256 fingerprints plus unmodified,
  modified, partial, and already-removed read-only adopter inventories. The
  fixtures contain no executable migration or compatibility path. Next: add
  the production GitHub issue adapter.
- **2026-07-16 — GitHub Task adapter:** Added the typed production issue store,
  server-side managed-label pagination, exact label lookup/creation, strict
  idempotent label deletion, close support, safe Task error codes, and request-
  boundary tests. `go vet ./...`, `go test ./...`, and focused race tests pass.
  Next: cut over setup-free repository resolution and the standalone CLI.
- **2026-07-16 — Standalone CLI cutover:** Replaced the orchestration-facing
  CLI with list/show/next and explicit Task lifecycle verbs, canonical JSON/
  text rendering, three exits, and pre-network validation. Repository targeting
  now proves explicit, ambient, and config-free local precedence. Repository-
  wide vet/tests, pinned staticcheck, binary help, and focused race tests pass.
  Next: delete the now-unreachable legacy runtime and fixtures.
- **2026-07-16 — M3 runtime deletion:** Deleted the legacy orchestration,
  policy, config, install, workflow, branch, PR, delivery, compatibility, and
  generic operation-report paths plus their old fixtures. The remaining Task
  contract has exhaustive fixed-label, next-ordering, and per-mutation
  execution-state tests. YAML is gone; repository-wide tests, race detection,
  vet, pinned Staticcheck, CLI boundary checks, v0.6 evidence hashes, and the
  implementation/diet review pass. M1–M3 are complete; next is M4.
- **2026-07-16 — Review hardening:** Made enrollment, unenrollment, blocker
  replacement, facet replacement, and close plans safe at every incomplete
  write prefix; unsafe conflicted-facet clears now require an explicit two-step
  normalization. Dry-run projections preserve project-label casing, mutation
  failures retain state-confirmation errors, no-op mutations avoid a redundant
  read, production requests have finite deadlines, and all text writers
  propagate failures. M4 remains next; M5 docs and skill work remain pending.
- **2026-07-16 — Migration scope correction:** Expanded M4 from v0.6-only
  decommissioning to direct migration from v0.5.0, v0.5.1, or v0.6.0. The
  released v0.5 line installed repository policy but did not assign
  `baton:managed` by default, so the plan now requires version-specific file
  evidence and explicit reviewed enrollment of v0.5-era issues lacking that
  label. Next: freeze the two v0.5 evidence profiles before writing the guide.
- **2026-07-16 — M4 and repository-local M5:** Added exact-tag-verified v0.5.0
  and v0.5.1 evidence, conservative cross-version inventories, and a
  required-check-first v0.7 adopter guide. Replaced the skill, docs,
  instructions, reviewer rules, and Release Please inputs with the Task-only
  surface; deleted retired manuals/templates; and added fixture, link, stale-
  surface, CLI, and release-target checks. Full tests/race/vet/Staticcheck/tidy
  and the implementation/diet review pass. External distribution and release
  actions remain intentionally unperformed.
- **2026-07-16 — M5 release handoff:** Reconciled the already-published v0.7.0
  with the later Task-only M5 merge, made the canonical skill installer-safe,
  refreshed the registered personal skill with byte-for-byte parity, and
  reviewed corrective v0.7.1 PR #28 plus its full prospective tag tree. The
  only remaining action is the separately authorized release merge/publication.
