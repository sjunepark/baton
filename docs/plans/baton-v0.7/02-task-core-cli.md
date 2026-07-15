# v0.7 Task core and CLI

## Purpose

Replace the layered repository-orchestration implementation with one deep Task
module backed by GitHub Issues. The resulting CLI must provide useful task
behavior from a small interface and delete complexity rather than hiding it
behind compatibility wrappers.

## Current state

- `internal/task` now owns the canonical Task, fixed labels, classification,
  deterministic selection, one mutation planner, the issue-store port, and a
  tested in-memory adapter.
- Dry-run and apply share the same Task-change plan. Apply creates only labels
  needed by additions, writes changes minimally, and rereads final state;
  confirmed prerequisite creation is surfaced on partial failure.
- The production GitHub adapter, setup-free repository resolution, CLI cutover,
  bounded v0.6 migration evidence, and old-runtime deletion remain.

## Architecture audit findings

The v0.6 implementation contains more than a few named downstream fixtures;
its central abstractions were shaped for repository dispatch. Not every item
was created exclusively for one consumer, but none earns its cost in the
standalone Task product:

- `internal/snapshot` and `internal/queue` model bounded acquisition,
  completeness, Candidates, Actions, Outcomes, Recommendations, revisions,
  deferred choices, and execution instructions for an external dispatcher.
- `internal/workflow` composes a repository-wide observation graph of issues,
  comments, PRs, branches, checks, reviews, commits, rules, and delivery state.
  Task commands need a direct issue query, not a reduced observation graph.
- `internal/workitem` derives PR-linked lifecycle states that disappear when
  completion is simply the GitHub issue's closed state.
- `internal/operation` exposes a generic multi-resource result protocol for
  repository reconciliation. A Task mutation needs only its own planned and
  confirmed label/close changes.
- `internal/config`, `internal/install`, `internal/doctor`, and manifest-driven
  labels exist to configure, install, and diagnose repository policy. Fixed
  Task labels and lazy creation remove that system and its YAML dependency.
- `internal/cli` carries legacy projections, multiple output formats, field
  selection, rich exit taxonomies, and dedicated downstream golden contracts.
- `internal/repository/context.go` reconciles lower-precedence checkout facts
  even after an explicit repository is known, coupling otherwise remote-only
  Task commands to local git state.

Remove these models and paths rather than renaming them into Task vocabulary.
Retain only the small issue, auth, transport, and repository-resolution
behavior listed below.

## Target module seam

Create one `internal/task` module that owns:

- the canonical Task and classification result;
- label normalization, state derivation, eligibility, blocker reasons, and
  priority ordering;
- fixed label definitions and one pure label-change planner used by enroll,
  update, unenroll, start, stop, and close;
- singular next-task selection from enrolled issues;
- idempotent already-satisfied outcomes and compact mutation results.

Put the true external GitHub seam inside that module:

- a small issue-store port exposes only the issue reads and mutations required
  by the Task interface;
- the production adapter uses Baton's typed GitHub client;
- an in-memory adapter exercises the same module interface in tests;
- transport pagination, authentication, and error translation stay behind the
  adapter rather than leaking into the Task model.

CLI parsing and JSON/text rendering remain outside the module. The CLI calls
the Task module directly; do not recreate `internal/workflow` as a Task facade.
Do not create ports for pure in-process policy or add interfaces that have only
one real adapter and no testing value.

## Implementation sequence

### 1. Establish the new contract in code

- [x] Add the canonical Task, state, mode, blocker, priority, list, singular
  next, label-change plan, and compact mutation-result types.
- [x] Implement pure label classification and priority ordering with
  table-driven tests from the product-contract plan, including zero, one, and
  conflicting priority labels.
- [x] Implement one pure label-change planner without stable operation IDs,
  resource-generic actions, or compare-and-set claims GitHub cannot enforce.
- [x] Make explicit mutation verbs apply immediately and expose the same pure
  plans through a uniform `--dry-run`; do not add a redundant `--apply` gate.
- [x] For apply, read current issue state, compute minimal idempotent writes,
  apply them, and reread the final Task. A dry-run is a current projection, not
  a lease or reusable atomic plan.

### 2. Add the issue-store adapters

- [x] Define the minimum issue-store port from real command needs.
- [x] Add the in-memory adapter first and test list, detail, next, and every
  transition through the Task interface.
- [ ] Trim the GitHub adapter to server-side queries for open/closed
  `baton:managed` issues, one-issue detail, label definitions and mutations,
  and close operations.
- [ ] Keep comments out of the Task store. Core list/show/next/mutations never
  fetch, create, update, parse, or depend on issue comments.
- [ ] Handle pagination completely and translate GitHub failures into Baton's
  small typed errors without leaking raw responses or credentials.
- [ ] Do not request PRs, branches, checks, reviews, GraphQL threads, commits,
  repository settings, or delivery facts from any Task command.

### 3. Make repository context setup-free

- [ ] Resolve explicit `--repo` first, then `GITHUB_REPOSITORY`, then optional
  local inference.
- [ ] Stop resolving after the first authoritative source. Do not inspect,
  compare, or reject a broken checkout remote when `--repo` or the environment
  already supplied the repository.
- [ ] Delete active config loading, `--config`, `.github/baton.yml` discovery,
  legacy decoding, label manifests, and all config precedence/validation.
- [x] Put the fixed mode, priority, blocker, managed, and activity labels in
  the Task module. Do not add a configurable label-role abstraction.
- [ ] Remove `gopkg.in/yaml.v3` and run `go mod tidy` after its config,
  manifest, and installer consumers are gone.
- [ ] Preserve existing token discovery where useful. Calling `gh auth token`
  as a credential provider remains allowed; do not scrape human-oriented `gh`
  command output for Task facts.

### 4. Cut over the CLI

- [ ] Implement exactly the top-level list, show, next, enroll, update,
  unenroll, start, stop, close, and `--version` surface from the public
  contract. Do not expose a `task` namespace or a second version spelling.
- [ ] Implement the exact fixed mode, priority, and repeatable blocker flags
  from the public contract. Reject an empty `update`, conflicting blocker
  flags, and invalid enum values as usage errors before auth or network work.
- [ ] Make no-argument Baton print concise help without resolving auth,
  repository, config, git, or network state. Delete `home` and `doctor`.
- [ ] Make `next` return one ready Task using priority then issue number across
  all modes, or a definitive null. Delete action tiers, candidate/deferred
  collections, human-choice states, and `--action`.
- [ ] Use a fixed compact text list and a bounded detail body. Delete
  `--fields` and field registries.
- [ ] Return definitive empty states and idempotent success/no-op results.
- [ ] Reject unknown input before auth or network calls and include the valid
  correction in structured output.
- [ ] Render human text or `--json` from the same Task values. Delete
  `--format`, TOON renderers/fixtures, per-result schema/kind fields, result
  help/instructions, and compatibility projections.
- [ ] Reduce public exits to success `0`, operational failure `1`, and usage
  error `2`. Keep typed internal errors, but emit the small JSON error envelope
  from the public contract, including Task-specific partial-mutation fields
  only when an operation actually failed after applying a change.

### 5. Keep project guidance outside the runtime

- [x] Let explicit lifecycle planners lazily create only the missing default
  label needed by that operation so first use does not require setup; do not
  update existing label metadata or create arbitrary project labels.
- [ ] Do not add setup/readiness commands, a template installer, issue-policy
  workflow, workflow pin, drift detector, validator command, or policy-comment
  maintainer.
- [ ] Keep an optional copyable issue-template example in user guidance only.
  It is not installed, parsed, fingerprinted, or tested as Task authority.

### 6. Delete the old runtime path

- [ ] Before deleting installer/doctor code, preserve exact v0.6 managed-file
  fingerprints and representative settings facts required by the bounded
  decommission fixtures.
- [ ] Remove PR policy, PR transition, PR/check/review inspection, branch
  planning, staging health, delivery recording/bootstrap, promotion,
  synchronization, and adoption-compatibility commands.
- [ ] Remove `snapshot` and fold the small amount of retained issue selection
  behavior into the Task module. Delete Candidate, Recommendation, Action,
  Outcome, acquisition/completeness, revision identity, deferred-selection,
  and in-band instruction types rather than renaming them.
- [ ] Remove body parsing, required sections, controlled form mappings, hidden
  ownership comments/digests, ownership repair, and issue-event authority.
- [ ] Remove config/install fields and all v0.6 decoding from the active
  runtime; any exact evidence needed by M4 lives only under a migration-v0.6
  fixture namespace.
- [ ] Delete generic `internal/operation` reports. Task mutations return only
  changed/dry-run facts, an ordered Task-specific change list, and the final or
  projected Task.
- [ ] Delete unused GraphQL/REST queries, fixtures, templates, packages, tests,
  docs hooks, and error paths. Do not leave no-op commands or compatibility
  adapters.
- [ ] Delete downstream-tool-specific adapters, fixtures, golden contracts,
  and documentation rather than translating them to the new Task contract.
- [ ] Delete `internal/cli/coda_contract_test.go` and
  `testdata/contracts/coda/`, then remove their testdata index entries.
- [ ] Delete `docs/GOAL-CODA-INTEGRATION-REDESIGN.md`,
  `docs/adopter-updates/coda-contract-baseline.md`, and
  `docs/adopter-updates/coda-repository-snapshot-v2.md`, then remove their
  index links. They are compatibility artifacts, not v0.7 history that Baton
  must maintain.
- [ ] Remove Coda-specific requirements from active product docs, repository
  instructions, and bundled skill references. Core Task docs should not define
  checkout, process, Run, dispatcher, or execution ownership.
- [ ] Preserve immutable release notes and clearly superseded ADRs only as
  history; they must not be linked or tested as active contracts.

## Code areas to inspect first

- `internal/workitem`, `internal/queue`, and `internal/snapshot`: retained
  issue classification and priority ordering to replace at one seam; do not
  retain their dispatcher/result models.
- `internal/workflow/repository_facts.go`: evidence of acquisition that should
  be replaced by one server-filtered issue request rather than trimmed into an
  issue-only workflow.
- `internal/policy/issue.go` and `issue_ownership.go`: body/comment authority to
  delete.
- `internal/repository/context.go`: explicit repository precedence to repair.
- `internal/cli/cli.go`: current public surface and rendering to replace.
- `internal/operation/result.go`: multi-resource partial-state protocol to
  delete rather than reuse for one-issue mutations.
- `internal/config/config.go` and `internal/labels`: active YAML/config/manifest
  surface to delete in favor of fixed Task labels.

## Deletion targets

Expect complete deletion of `internal/delivery`, `internal/doctor`,
`internal/install`, `internal/operation`, `internal/queue`, `internal/snapshot`,
`internal/workflow`, `internal/workitem`, `internal/policy`, `internal/labels`,
and active `internal/config`. Move only the minimum read-only remote parsing
and repository inference into `internal/repository`, then delete `internal/git`
instead of retaining a mixed branch/command utility package. Delete unused
GitHub methods. Replace the small earned issue behavior in `internal/task`; do
not layer a facade over old packages.

Keep and simplify modules that continue to earn their interface:

- auth discovery;
- small typed usage/GitHub errors with the three public exits;
- repository-name normalization and optional read-only remote inference;
- the generic HTTP transport used by the Task module's GitHub adapter;
- direct JSON encoding and small human-text renderers.

## Validation

- [ ] Unit-test the Task module exclusively through its external interface;
  delete shallow tests after replacement coverage exists.
- [ ] Test the production adapter with `httptest` fixtures for pagination,
  errors, redaction, server-side managed-label filtering, label creation,
  label mutation, and close behavior.
- [ ] Assert list/next make no comment, PR, branch, check, review-thread,
  settings, commit, or delivery request.
- [ ] Test all commands without config using explicit repo, ambient repo, and
  optional local inference; include an explicit repo beside a broken checkout.
- [ ] Test every mutation in dry-run/no-op/apply/partial-failure/final-reread
  states without asserting atomic claims or stale-plan identities.
- [ ] Delete all old contract fixtures, including doctor, queue TOON, PR policy,
  pull request, transition, delivery, event, format/field, rich-error, and
  generic operation-report fixtures and tests. Retain only explicitly named
  v0.6 decommission evidence under `testdata/migration/v0.6`.
- [ ] Audit named downstream-tool references. Every remaining occurrence must
  be this bounded removal checklist or immutable, clearly superseded history;
  no active code, test, fixture, spec, skill, or instruction may depend on it.
- [ ] Run `gofmt`, `go vet ./...`, pinned staticcheck, and `go test ./...`.
- [ ] Run `go mod tidy` and confirm the YAML dependency and removed-package
  imports are absent.
- [ ] Inspect the final diff with the repository's implementation-review and
  diet lenses; remove compatibility or abstraction surface that does not earn
  leverage.

## Completion criteria

- A new user with the binary and GitHub credentials can inspect and manage
  explicitly enrolled tasks without committing any Baton file.
- An adopter can remove every optional Baton repository file and core task
  behavior remains unchanged.
- No active CLI command, JSON field, or Go path requires a
  branch, PR, check, review, delivery, body schema, or hidden comment.
- No active JSON or Go type models Candidate, Recommendation, Action, Outcome,
  acquisition, completeness, a generic operation report, or execution state.
- No active code, test fixture, or documentation gives Baton responsibility
  for adapting or validating a downstream orchestrator.
- The full Go validation suite passes after old runtime tests and fixtures are
  deleted or replaced.

## Progress log

- **2026-07-16 — Task core and memory adapter:** Implemented and reviewed the
  deep Task seam with safe replacement ordering, input-pure planning,
  dry-run/apply Task-change parity, idempotent transitions, final rereads, and
  Task-specific partial failures. `go vet ./...`, `go test ./...`, and
  `go test -race ./internal/task` pass. Next: preserve v0.6 decommission
  evidence and implement the typed GitHub issue-store adapter.
