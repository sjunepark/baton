# v0.7 product and public contract

## Purpose

Freeze the issue-only product contract before restructuring implementation.
This plan converts the confirmed direction into observable semantics without
preserving v0.6 branch, PR, delivery, body-schema, or downstream-tool
compatibility.

## Current state

- The contract is represented in the new `internal/task` model and service.
- Lifecycle precedence, missing/contradictory classification, blockers with
  activity, closed Tasks, and unenrolled issues have direct external-package
  tests. CLI JSON/error goldens, empty states, and idempotent lifecycle outcomes
  are covered; the exhaustive label/next-ordering matrix remains.

## Domain model

- **Issue**: the GitHub resource. Baton reads only the number, title, URL,
  open/closed state, labels, and the body when detail output needs it.
- **Task**: an Issue carrying `baton:managed`.
- **Enrollment**: the explicit addition of `baton:managed`; never inferred from
  content, template shape, comments, branches, or PR references.
- **Mode**: what an agent may do next: trivial implementation, bounded
  implementation, or investigation only.
- **Priority**: an ordered queue preference within otherwise eligible work.
- **Blocker**: a label explaining why an open Task is not actionable, such as
  `needs-info` or `needs:discussion`.
- **Activity**: advisory evidence that work has started, represented by
  `baton:in-progress`; it is not an exclusive claim.
- **Done**: the Issue is closed. Baton does not separately record delivery.

## Derived lifecycle

Classify an enrolled issue deterministically in this order:

1. Closed issue -> `done`.
2. Open issue with any fixed blocker, missing mode, contradictory mode
   labels, or contradictory priority labels -> `blocked`, with explicit
   reasons.
3. Otherwise, issue with `baton:in-progress` -> `in_progress`.
4. Otherwise -> `ready`.

An issue may temporarily retain `baton:in-progress` while blocked; the blocker
wins in the derived state. `stop` removes activity when work is abandoned.
Removing the blocker can therefore resume the prior activity signal unless a
person or command explicitly stopped it.

## Label contract

### Baton-owned facts

- `baton:managed`: complete enrollment authority.
- `baton:in-progress`: advisory activity.

### Retained agent eligibility

- `agent:ready-trivial`
- `agent:ready-bounded`
- `agent:investigate-only`

Exactly one eligibility label is required for an actionable Task. The labels
describe permitted work, not branch or delivery behavior.

### Blockers and priority

- Default blockers include `needs-info` and `needs:discussion`.
- Default priority remains `priority:p0` through `priority:p3`, with P0 first.
  Zero priority labels means P2; one selects that priority; more than one is a
  blocking classification conflict.
- Work-kind labels such as `bug`, `documentation`, `enhancement`, and
  `question` remain project-owned annotations. Baton does not require, rewrite,
  or remove them.
- `needs:review` and delivery-specific labels are not part of v0.7.

## Required capabilities

Use concise top-level verbs; every retained command already concerns Tasks:

```text
baton [--repo owner/name] [--json] list [--state open|closed|all]
baton [--repo owner/name] [--json] show ISSUE [--full]
baton [--repo owner/name] [--json] next
baton [--repo owner/name] [--json] enroll ISSUE [--mode trivial|bounded|investigate] [--priority p0|p1|p2|p3] [--dry-run]
baton [--repo owner/name] [--json] update ISSUE [--mode trivial|bounded|investigate|none] [--priority p0|p1|p2|p3|none] [--add-blocker needs-info|needs:discussion]... [--remove-blocker needs-info|needs:discussion]... [--dry-run]
baton [--repo owner/name] [--json] unenroll ISSUE [--dry-run]
baton [--repo owner/name] [--json] start ISSUE [--dry-run]
baton [--repo owner/name] [--json] stop ISSUE [--dry-run]
baton [--repo owner/name] [--json] close ISSUE [--dry-run]
baton --version
```

- `ISSUE` is a positive GitHub issue number in the resolved repository; v0.7
  does not add cross-repository issue-reference parsing.
- No arguments prints concise help and performs no auth, git, or network work.
- `list` is the only collection view; it defaults to open Tasks. Text output is
  compact and JSON returns canonical Task summaries. There is no `--fields`
  selector.
- `show` returns one enrolled Task with a bounded body preview; `--full` is the
  deliberate escape hatch for the complete body.
- `enroll` adds `baton:managed` and may set mode/priority. Omitting mode is
  allowed and produces an explicitly blocked Task rather than hidden defaults.
- `update` changes only Baton's fixed classification facets: mode, priority,
  and the fixed blocker labels. `none` clears the selected facet; blocker flags
  are repeatable. At least one change flag is required, and adding and removing
  the same blocker is invalid usage. It is not an arbitrary GitHub label editor.
- Setting mode or priority replaces every fixed label in that facet; it never
  touches project labels. `enroll` uses the same normalization when either
  optional facet is supplied.
- `unenroll` removes Baton's managed/activity labels and leaves project,
  eligibility, priority, blocker, body, and comment data intact.
- `start` and `stop` add/remove advisory activity. `close` removes activity and
  closes the issue while preserving enrollment and classification labels.
- `update`, `start`, `stop`, and `close` reject unenrolled issues with a hint to
  run `enroll`. `start` rejects closed Tasks; `close`, `stop`, and every other
  already-satisfied transition are idempotent.
- Mutation verbs apply immediately and share `--dry-run`; there is no generic
  `--apply` confirmation.
- Baton does not create GitHub issues in v0.7. Users or the skill create them
  through normal project tooling, then explicitly enroll them.
- `list`, `show`, and `next` expose only enrolled Tasks. Showing an unenrolled
  issue returns a `not_managed` operational error with an enrollment hint.
- Human text and `--json` are the only output modes. Remove `--format`, TOON,
  format aliases, and per-command render contracts.

### Singular next selection

`next` considers only open Tasks whose derived state is `ready`. It orders
them by P0, P1, P2 (including unspecified priority), then P3, with issue number
ascending as the stable tie-breaker. Mode never creates an action tier or
dispatch precedence. The command returns the first Task or a definitive
`null`; list output exposes the rest.

Do not return Candidate arrays, deferred work, Action, Outcome,
`selectionRequired`, Recommendations, or execution instructions. The selected
Task's mode describes what kind of work it permits.

## Canonical Task result

Use one canonical Task payload across list, show, next, and mutation results.
The Task payload uses these fields and only durable facts from Baton's Task
domain:

- `number`, `title`, and `url`;
- `issueState` (`open` or `closed`) and derived `state` (`ready`,
  `in_progress`, `blocked`, or `done`);
- nullable `mode`, nullable normalized `priority`, and boolean `inProgress`;
- sorted `blockers`, `projectLabels`, and machine-readable `reasons` arrays;
- optional `body` and `bodyTruncated` fields in detail output.

Missing priority normalizes to P2; contradictory priority labels produce a
null priority and blocking reasons. Missing or contradictory mode labels
produce a null mode and blocking reasons. Fixed Baton labels are represented
by the normalized fields and do not reappear in `projectLabels`.

Do not include branch refs, PR identities, checks, review threads, commits,
delivery cursors, acquisition windows, promotion facts, or compatibility
projections. Also exclude eligibility booleans, priority ranks, counts, help
arrays, execution instructions, transport-only node identities, and duplicated
label/value fields.

The command already identifies the result and SemVer identifies its contract,
so do not add `kind` or `schemaVersion` to every payload. JSON shapes are:

- list: `{repository, tasks}`;
- show: `{repository, task}`;
- next: `{repository, task}`, where `task` is nullable;
- mutation: `{repository, changed, dryRun, changes, task}`, where `task` is
  nullable after unenroll and `changes` is a small ordered list of Task-label
  or close operations.

Pure plans remain internal. Do not expose stable operation IDs, generic
resource names, aggregate partial/refused state lattices, or a separate
operation-report protocol. If a mutation partially fails, report applied
changes and the final reread when available without inventing multi-resource
orchestration semantics.

For JSON errors, write a small `{error:{code,message,hint}}` object to stderr.
A partially applied mutation may add Task-specific `changes` and `task` fields
inside `error` so callers can see confirmed effects and the final reread; do
not expose a generic operation state machine. Use exit `0` for success, `1`
for an operational failure, and `2` for invalid usage. Keep richer GitHub
metadata internal unless it directly improves the safe message or hint.

## Repository targeting and fixed semantics

- `--repo owner/name` is authoritative and must work outside a checkout.
- `GITHUB_REPOSITORY` is an explicit environment fallback.
- A local git remote may provide a convenience fallback but must never defeat
  an explicit repository or make task use require a valid checkout.
- Once a higher-precedence source resolves, do not inspect or reconcile lower
  sources. An unrelated or broken local remote cannot invalidate `--repo`.
- v0.7 uses the fixed labels in this contract. It has no active config loader,
  config schema, `--config` flag, label manifest, or legacy decoder.
- Remove the YAML runtime dependency after config/manifest/install parsing is
  deleted. Optional static guidance may be embedded as bytes without adding a
  YAML object model.

## Optional project guidance

There is no installed intake profile in v0.7:

- Lifecycle operations lazily create only missing labels from the fixed Baton
  vocabulary and never update existing label colors/descriptions.
- The skill/docs may provide a copyable issue-template example encouraging a
  summary, evidence, acceptance criteria, constraints, and validation hints.
- Baton does not install, pin, inspect, update, or remove that template; its
  presence and shape never affect Task semantics.
- Baton does not install an issue workflow, add policy blockers in the
  background, or create/repair a policy comment.
- The supported path for manual or external issues is explicit
  `enroll`/`update`, without editing the body.

## Compatibility and versioning

- Treat CLI commands/flags, Task JSON, exit behavior, and skill semantics as
  one breaking v0.7 public change.
- Delete active config decoding rather than decoding v0.6 config into v0.7.
- Do not keep `queueSnapshot`, `nextCandidates`, or `repositorySnapshot`
  compatibility projections. Delete them without providing a replacement
  adapter or client-specific fixture.
- Baton does not identify, inventory, adapt, test, or document downstream
  orchestrators as an active product obligation. Its responsibility ends at a
  coherent public Task contract.
- Removed commands use the ordinary unknown-command error. Do not retain
  tombstones, aliases, no-op implementations, or targeted compatibility exits.

## Contract tests

- [ ] Table-test every label combination and derived lifecycle outcome.
- [x] Cover contradictory modes, missing mode, multiple priorities, blockers
  with activity, closed tasks, and unenrolled issues.
- [x] Golden-test the small JSON list/show/next/mutation and error shapes
  without downstream-tool-specific fixtures.
- [x] Test definitive empty lists and no-next-task results.
- [ ] Test singular next ordering across priority, unspecified priority, issue
  number, and every mode; mode must not affect ordering.
- [x] Test idempotent enroll, unenroll, start, stop, and close outcomes.
- [x] Test explicit repository precedence, including a broken lower-precedence
  local remote, and prove that no config discovery occurs.
- [x] Test that list/next never read comments or request PR, branch, check,
  review-thread, commit, settings, or delivery facts.

## Completion criteria

- The core Task contract can be understood without any branch, PR, workflow,
  delivery, issue-form, Candidate, Recommendation, Run, dispatcher, or
  downstream-orchestrator concept.
- Every later implementation and migration task can cite this contract instead
  of recovering behavior from v0.6 code.
- No unresolved product choice prevents implementing the Task module and CLI.

## Progress log

- **2026-07-16 — Domain contract:** Added the canonical Task fields, fixed
  label semantics, machine-readable reasons, derived lifecycle, bounded detail,
  and singular ready-Task selection. Focused, race, vet, and repository-wide
  tests pass. Remaining M1 contract work is primarily CLI/output goldens and
  the exhaustive ordering/idempotence coverage completed with that surface.
- **2026-07-16 — Production fact boundary:** Added request-recorder coverage
  proving list/next use only the server-filtered GitHub issues endpoint and do
  not acquire comments or orchestration facts. Transport failures become small
  Task codes without leaking response bodies or credentials.
- **2026-07-16 — Public contract cutover:** Replaced the v0.6 CLI projections
  with canonical list/show/next/mutation/error goldens, definitive empty states,
  exact fixed flags, and the three-exit contract. CLI and resolver tests prove
  explicit, ambient, and local repository precedence without config discovery.
