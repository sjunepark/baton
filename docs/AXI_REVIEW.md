# AXI Review

Reviewed Baton at `fb24bdc` against the AXI principles described at
<https://axi.md/>.

## Scope

This review covers the agent-facing CLI surface, JSON contracts, GitHub read
models, worktree lease output, bundled Baton skill, and command documentation.
It does not propose changing Baton's core safety model: deterministic CLI
decisions stay in Go, mutating operations stay explicit, and Codex keeps
judgment work.

Validation run during review:

```sh
go test ./...
go run ./cmd/baton
go run ./cmd/baton init --dry-run --json
go run ./cmd/baton doctor --json
go run ./cmd/baton labels --file internal/install/templates/.github/labels.yml --json
go run ./cmd/baton queue --help
go run ./cmd/baton next --help
go run ./cmd/baton lease --help
```

## Summary

Baton is already strong on the AXI robustness axis: it has stable JSON result
objects, explicit exit codes, dry-run or apply gates for mutations, pure
decision objects, and no interactive prompts. It also ships a concise Codex
skill, which is a good foundation for ambient agent context.

The main AXI gaps are at the interface layer:

- no TOON or other compact agent-native output mode;
- no content-first home view when `baton` is run without arguments;
- per-command help is Go flag help, exits as usage, and lacks examples;
- errors are plain stderr strings instead of structured result objects;
- high-volume outputs need counts, summaries, truncation metadata, and
  contextual next commands;
- the bundled skill still tells agents to request JSON explicitly instead of
  steering them toward an AXI-style home or compact output.

The safest path is additive: keep the current JSON contracts stable, then add an
AXI output layer and richer summaries around the existing internal result
objects.

## Current State

- `docs/OUTPUT_SPEC.md` now defines the additive output contract for JSON
  compatibility, TOON/compact output, structured errors, counts, truncation, and
  `help[]` fields.
- The home/no-args decision is recorded there: ship `baton home` first, then
  change no-args to that home view after tests and docs are in place.
- `internal/cli` now routes JSON success output and post-parse JSON-mode errors
  through a shared renderer. Structured errors use the AXI shape with category,
  exit code, message, hint, and retryability while preserving successful JSON
  result objects.
- Queue, PR list, check rollup, review-thread, doctor, label, lease, and prune
  outputs now expose additive count/summary/help metadata, and text list views
  print explicit empty states.
- `baton help <command>` and `<command> --help` now provide first-class
  command help on stdout with exit 0.

## AXI Scorecard

| AXI principle | Current fit | Possible fix or enhancement |
| --- | --- | --- |
| Token-efficient output | Partial. JSON is stable but pretty-printed by default in `writeJSON`, and there is no TOON mode. | Add `--format toon,json,text` or `--format toon` alongside existing `--json`; keep `--json` for compatibility. Add compact JSON or `--pretty` as an opt-in. |
| Minimal default schemas | Partial. Queue list bodies are omitted, but list items still include nested wrappers, URLs, labels, SHAs, and repeated metadata. | Define compact default views per command and add `--fields` or `--view full` for expansion. |
| Content truncation | Weak. Review-thread comments return full bodies, and migration dry-run JSON can return complete generated config content. | Add default body limits with `bodyChars`, `bodyTruncated`, and `--full` escape hatches. |
| Pre-computed aggregates | Partial. `next` precomputes one action, and checks have a single rollup state, but most lists lack counts and grouped summaries. | Add `counts` and summaries to queue, PRs, checks, review threads, label sync, leases, prune, and doctor. |
| Definitive empty states | Partial. JSON arrays are explicit, but text output loops can print nothing for empty lists. | Add `count: 0` or clear "0 results" text for empty queues, PR lists, leases, checks, threads, and sync plans. |
| Structured errors and exit codes | Partial. Exit codes exist and mutations are gated, but errors are unstructured stderr strings even when `--json` is requested. | Add a shared `error` result envelope on stdout for structured modes, with category, exit code, message, hint, and retryability. |
| Ambient context | Partial. `skills/baton` exists, but there is no command that installs or prints a compact session dashboard. | Add `baton home` or no-args home output, and consider `baton setup-agent-context` for Codex/session integrations. |
| Content first | Weak. No-args `baton` prints global help rather than live repo state. | Make no-args output a live home view with binary path, repo/config/auth status, active leases, and next suggested command. |
| Contextual disclosure | Partial. `nextAction.instructions` is useful, but other command outputs do not include `help[]` next steps. | Append concrete `help[]` commands to structured outputs, especially `doctor`, `queue`, `prs`, `pr`, `checks`, `review-threads`, and `lease`. |
| Consistent help | Weak. Subcommand `--help` is default Go flag help and currently exits as usage through `go run`. | Add first-class subcommand help on stdout with exit 0, concise examples, and `baton help <command>`. |

## Prioritized Enhancements

### AXI-001: Add an AXI Output Layer

Evidence:

- Current automation guidance and docs center on `--json`.
- `writeJSON` always pretty-prints JSON, which is readable but token-heavy.
- Most internal command handlers already produce typed result objects, so a
  renderer layer can be additive.

Recommended shape:

- Keep `--json` as the stable automation contract.
- Add `--format <json|toon|text>` with `toon` available for read-heavy agent
  commands.
- Add a shared renderer boundary in `internal/cli` so commands return result
  objects plus metadata instead of writing per-command output directly.
- Make compact output preserve result identity, counts, truncation hints, and
  next-step help.
- Add golden tests for `next`, `queue`, `prs`, `checks`, `review-threads`,
  `doctor`, `init`, and `leases` in compact mode.

Compatibility note: do not replace existing JSON initially. Existing workflows,
tests, and the bundled skill depend on it.

### AXI-002: Replace No-Args Help With a Content-First Home View

Evidence:

- `Run` treats empty args, `--help`, `-h`, and `help` identically and calls
  `printHelp`.
- The current no-args output is a full usage list, not live repo state.

Recommended home output:

```text
bin: ~/go/bin/baton
description: Coordinate GitHub issue/PR agent workflows for this repository
repo: sjunepark/baton
config: missing (.github/baton.yml)
auth: ok (gh auth token)
leases: 0 active
next: unavailable (config missing)
help[3]:
  Run `baton init --dry-run --format toon`
  Run `baton doctor --format toon`
  Run `baton --help`
```

Implementation notes:

- Avoid making the home view fail just because config or auth is missing.
  Missing pieces should appear as explicit fields.
- Render paths with the home directory as `~`.
- Keep `baton --help` as help; change only no-args and optionally add
  `baton home`.

### AXI-003: Make Subcommand Help First-Class

Evidence:

- `baton queue --help`, `baton next --help`, and `baton lease --help` currently
  show Go flag output and return usage from the program.
- The output lacks command purpose, common examples, and related next commands.

Recommended shape:

- Add `baton help <command>` and make `<command> --help` exit 0.
- Print to stdout, reserve stderr for debug and unexpected failures.
- Keep each help page short: purpose, usage, flags, examples, output formats,
  and related commands.
- Add tests for global help and representative subcommand help.

### AXI-004: Add Structured Error Results

Evidence:

- CLI handlers mostly return errors with `fmt.Fprintln(stderr, err)`.
- `--json` success output is structured, but failure output is not.
- Exit code categories already exist: policy, usage, config, auth, GitHub, and
  local git.

Recommended result:

```json
{
  "schemaVersion": 1,
  "kind": "error",
  "category": "config",
  "exitCode": 3,
  "message": "baton config not found",
  "hint": "Run `baton init --dry-run` or pass `--config <path>`.",
  "retryable": false
}
```

Implementation notes:

- In structured modes, write the error object to stdout and exit with the same
  numeric code.
- Keep stderr for debug traces and lower-level command diagnostics.
- Centralize error mapping instead of repeating strings in each handler.
- Preserve the current policy failure behavior where a valid decision object can
  be emitted with a policy-failure exit code.

### AXI-005: Add Aggregates To High-Volume Results

Evidence:

- `queue.Snapshot` has `issues` and `pullRequests`, but no totals.
- `CheckRollup` has one `state`, but no per-state counts.
- `ReviewThreadResult` has threads, but no unresolved counts or author-kind
  summary.
- Label sync, leases, prune, and doctor all require an agent to count arrays.

Recommended fields:

- `queue`: `counts.totalIssues`, `counts.eligibleIssues`,
  `counts.skippedIssues`, `counts.openPullRequests`,
  `counts.branchHealthState`.
- `prs`: `count`, plus counts by `checkState`.
- `checks`: `summary.passed`, `summary.failed`, `summary.pending`,
  `summary.skipped`, `summary.cancelled`, `summary.unknown`.
- `review-threads`: `summary.total`, `summary.unresolved`,
  `summary.humanUnresolved`, `summary.botUnresolved`,
  `summary.outdated`.
- `labels` and `sync-labels`: counts by action.
- `leases` and `prune`: counts by status/action.
- `doctor`: counts by status and an overall readiness state.

These fields remove common follow-up shell filtering and make `next` decisions
easier to audit.

### AXI-006: Add Truncation And Escape Hatches

Evidence:

- `ReviewComment.Body` is returned in full.
- GraphQL review thread fetching paginates all comments and keeps complete
  bodies.
- `migrate-config --dry-run --json` includes full generated config content.

Recommended shape:

- Default text body limit: 2-4 KB per comment or document-like field.
- Include metadata:
  - `bodyChars`
  - `bodyTruncated`
  - `bodyPreview`
  - `fullCommand`, for example `baton review-threads 12 --full --json`
- Add `--full` and possibly `--body-limit <n>` for commands that expose long
  content.
- Keep full internal data available for policy decisions; truncation is an
  output concern.

Primary targets:

- `review-threads`
- `pr`
- `migrate-config --dry-run`
- `complete --summary/--validation` output
- future issue-body or PR-body views

### AXI-007: Add Compact Views And Field Selection

Evidence:

- List command defaults include nested structures and sometimes fields agents do
  not need for first-pass triage.
- Baton already has separate list and view commands, which makes compact
  defaults straightforward.

Recommended shape:

```sh
baton queue --format toon
baton queue --fields number,title,action,reasons
baton prs --fields number,title,headRef,checkState
baton checks 12 --fields name,state,url
```

Keep defaults around 3-4 fields per list item where possible. Use detail
commands such as `baton pr <number>` and `baton review-threads <number>` for
expanded data.

### AXI-008: Make Empty States Explicit In Text And Structured Output

Evidence:

- Text renderers for `queue`, `prs`, `review-threads`, `leases`, and similar
  list commands iterate over arrays and can print no result lines.
- Some JSON results have empty arrays, but no `count: 0` field or next-step
  hint.

Recommended text examples:

```text
pullRequests[0]:
help[2]:
  Run `baton queue --format toon`
  Run `baton next --format toon`
```

```text
leases[0]:
help[1]:
  Run `baton lease --purpose <purpose> --base <ref> --new-branch <ref>`
```

### AXI-009: Add Contextual `help[]` To Results

Evidence:

- `NextAction.Instructions` already works as operational guidance.
- Other outputs force the agent to know the next command or read global help.

Recommended examples:

- `doctor` with missing config:
  - `Run baton init --dry-run`
  - `Run baton doctor --config <path>`
- `queue`:
  - `Run baton next`
  - `Run baton lease --purpose issue-<number> ...`
- `prs`:
  - `Run baton pr <number>`
  - `Run baton checks <number>`
  - `Run baton review-threads <number>`
- `checks` with failures:
  - `Run baton pr <number>`
  - `Open <detail-url>` only when useful and not too noisy.
- `review-threads` with unresolved human comments:
  - `Stop and ask the user if the comment requires product judgment.`

In structured output, keep these as command templates with placeholders rather
than guessed runtime values.

### AXI-010: Add An Agent Context Setup Command

Evidence:

- `skills/baton` gives concise process guidance, but users or automations still
  need to know that the skill exists and to run `baton next --json`.
- AXI recommends ambient context before the agent takes action.

Recommended shape:

- Add `baton context` or `baton home --format toon` as the compact dashboard.
- Add `baton setup-agent-context --dry-run|--apply` only if Baton should install
  repo-local agent hints or hooks.
- Update `skills/baton/SKILL.md` to prefer the AXI home/compact output once it
  exists.
- Generate the skill command reference from the same command metadata used by
  CLI help, so docs do not drift.

### AXI-011: Deepen PR And Review Summaries

Evidence:

- `baton pr <number> --json` currently returns PR basics plus check state.
- The CLI spec expects richer PR state, including linked issues, labels, review
  decision, unresolved threads, and latest bot summaries.

Recommended enhancement:

- Keep `prs` compact.
- Make `pr <number>` a precomputed PR dashboard:
  - linked issue numbers and label readiness;
  - check summary counts;
  - review-thread summary counts;
  - human/bot unresolved split;
  - likely next command.
- Include a `--full` path for full review-thread bodies and bot summaries.

This aligns with AXI's aggregate principle and reduces the current sequence of
`pr`, `checks`, and `review-threads` calls.

### AXI-012: Normalize Lease Output For Agent Consumption

Evidence:

- Lease records expose both `path` and `worktreePath`.
- Full absolute paths are useful, but repeated long paths are costly in list
  views.

Recommended enhancement:

- Keep `path` as the canonical edit directory for agents.
- Mark `worktreePath` as compatibility or remove it in a future schema version.
- In compact output, render home-relative paths as `~`.
- Add lease list counts and active/released/pruned grouping.
- Add `help[]` lines for release, prune, and `cd <path>`.

## Suggested Implementation Plan

1. [x] Define the interface contract:
   - Add a short `docs/OUTPUT_SPEC.md` covering JSON compatibility, TOON or
     compact format, structured errors, counts, truncation, and help arrays.
   - Decide whether no-args `baton` changes immediately or whether `baton home`
     ships first.

2. [x] Centralize CLI rendering:
   - Introduce a result envelope or renderer helper in `internal/cli`.
   - Preserve existing JSON structs and tests.
   - Add structured error mapping.

3. [x] Add low-risk AXI metadata:
   - [x] `count` and `counts` fields.
   - [x] `help[]` fields.
   - [x] explicit empty-state text.
   - [x] subcommand help with exit 0.

4. [ ] Add truncation:
   - Start with `review-threads`.
   - Add `--full` tests.
   - Then extend to PR body/config-like dry-run content.

5. [ ] Add compact/TOON output:
   - Start with read-only commands: `home`, `doctor`, `next`, `queue`, `prs`,
     `checks`, and `review-threads`.
   - Add golden tests for stable formatting.

6. [ ] Update agent-facing docs:
   - Revise `skills/baton/SKILL.md` and references.
   - Update README examples to show compact agent output where appropriate,
     while keeping `--json` examples for automation compatibility.

## Progress Log

- 2026-06-23: Completed the interface-contract slice by adding
  `docs/OUTPUT_SPEC.md` and recording the `baton home` before no-args-home
  decision. Validation: `go test ./...` passes.
- 2026-06-23: Completed the centralized-renderer slice by adding an
  `internal/cli` renderer, AXI-shaped structured errors for JSON-mode command
  failures after flag parsing, and CLI tests for config and usage errors.
  Validation: `go test ./...` passes; a built `cmd/baton` binary emits
  structured error JSON on stdout with empty stderr for representative config
  and usage failures.
- 2026-06-23: Completed the counts/help/empty-state metadata slice for queue,
  PR lists, check rollups, review threads, doctor, labels, leases, and prune.
  Validation: `go test ./...` passes; a built `cmd/baton` binary shows metadata
  on `labels --json`, `leases --json`, and `doctor --json`, and `leases` text
  output prints an explicit empty state.
- 2026-06-23: Completed first-class command help with `baton help <command>`
  and `<command> --help` rendering concise stdout help and exit 0. Validation:
  `go test ./...` passes; a built `cmd/baton` binary verifies `queue --help`
  and `help lease` exit 0 with empty stderr.

## Keep As-Is

- Keep JSON contracts with `schemaVersion`; they are useful for tests,
  workflows, and non-agent automation.
- Keep explicit `--apply`, `--yes`, lease, and release gates.
- Keep deterministic policy and queue decisions in Go.
- Keep the bundled skill concise and avoid embedding large schemas in prose.
- Keep GitHub writes idempotent where practical and never introduce interactive
  prompts for automation flows.
