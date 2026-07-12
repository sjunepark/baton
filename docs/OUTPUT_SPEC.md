# Output Spec

Baton output is optimized for deterministic automation first, then compact
agent consumption, then human readability. This spec defines the additive AXI
output contract for future CLI changes.

## Compatibility

- Existing `--json` output remains the stable automation contract.
- Existing JSON result structs keep their current field names and
  `schemaVersion` values until a deliberate schema migration is documented.
- New metadata such as `count`, `counts`, `summary`, `help`, and truncation
  fields may be added to existing JSON objects as optional additive fields.
- Policy commands may continue returning a valid decision object with a
  policy-failure exit code when the command successfully evaluated an unsafe
  state.

### Versioning and migration rules

- Adding an optional field is compatible only when existing field meaning,
  types, and required collection shapes stay unchanged. Collections consumed
  by Coda remain JSON arrays, including when empty; they do not become `null`.
- Renaming or removing a field, changing its type or meaning, making an
  optional field required, changing a `kind`, or changing an exit code requires
  a schema-version bump and parallel fixtures for the old and new contracts.
- `nextCandidates` v2, `queueSnapshot` v1, and structured error v1 remain
  available while the maintained Coda consumer migrates. Their adopter note
  must define the supported overlap period. Removing them is a breaking public
  change and therefore requires a Baton major release after that period.
- `repositorySnapshot` v1 is the preferred observation contract. Its queue,
  branch, pull-request, and Recommendation fields come from one bounded
  acquisition; legacy queue and next results are projections of that model.
- Golden consumer fixtures live in `testdata/contracts/coda/`. Tests there use
  consumer-style required-field checks and deliberately allow additive fields.

### Baseline output and exit behavior

The workflow seam preserves the following maintained public behavior:

| Case | Text | JSON | TOON | Exit |
| --- | --- | --- | --- | --- |
| `queue` success | issue lines or an empty-list hint | `queueSnapshot` v1 | hand-rendered queue fields | 0 |
| `next` success | action, reason, count, candidates | `nextCandidates` v2 | hand-rendered recommendation fields | 0 |
| `snapshot` complete | repository, completeness, outcome, counts | `repositorySnapshot` v1 | compact acquisition and Recommendation fields | 0 |
| `snapshot` degraded | degraded observation summary | `repositorySnapshot` v1 with warnings and no Action | compact degraded fields | 0 |
| no eligible work | normal success output | action `none`, arrays present | action `none` | 0 |
| rendered usage/config/auth/GitHub/local-git error | message on stderr | error v1 on stdout | error v1 fields on stdout | matching 2–6 |
| evaluated unsafe policy | decision text/JSON | policy decision, not error v1 | command-specific | 1 |
| `pr-transition --dry-run` | planned operation count | `workItemTransitionPlan` v1 | not supported | 0 |
| `pr-transition --apply` | applied operation count | transition plan v1 plus `operationReport` v1 | not supported | 0 or matching error |

Flag-parser failures that occur before a renderer is selected, unknown
commands, and global help errors currently remain unstructured stderr usage
output. This is characterized legacy behavior, not the target design; changing
it requires an intentional fixture and migration decision.

## Formats

Commands that expose structured output should support:

```sh
--json
--format json
--format toon
--format text
```

`--json` is a compatibility alias for `--format json`.

Default format rules:

- Mutating or policy commands keep their current defaults until each command has
  tests covering structured errors and text output.
- Read-heavy commands may gain compact text defaults only after their JSON and
  TOON contracts are covered by golden tests.
- `--format json` emits compact JSON by default. Add `--pretty` later only if
  pretty-printed JSON proves useful enough for humans.

## TOON Contract

TOON output is intended for agents that need compact, stable, inspectable state.
It should preserve:

- `kind`
- `schemaVersion`
- `repo` when known
- stable item identity such as issue number, PR number, branch ref, or check name
- counts and summaries
- truncation metadata
- concrete `help[]` next commands

TOON output should omit fields that are expensive and rarely needed for first
pass triage, including repeated URLs, full markdown bodies, and duplicate path
forms. Detail commands and `--full` remain the escape hatches.

## Structured Errors

Structured modes must write error results to stdout and return the matching
numeric exit code. Stderr is reserved for debug traces and unexpected lower-level
diagnostics.

Error result shape:

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

Categories map to existing exit codes:

- `policy`: exit `1`
- `usage`: exit `2`
- `config`: exit `3`
- `auth`: exit `4`
- `github`: exit `5`
- `localGit`: exit `6`

Usage errors should include a short command-specific hint. Config, auth,
GitHub, and local-git errors should include whether retrying without a state
change is likely to help.

## Counts And Summaries

List and dashboard outputs should include precomputed counts so agents do not
need to post-process arrays.

Recommended fields:

- `count`: total returned items for simple lists.
- `counts`: named totals for mixed results.
- `summary`: grouped state for dashboards and rollups.

Initial targets:

- `queue`: issue totals, eligible/skipped totals, open PR totals, branch health
  state totals.
- `prs`: total PRs and counts by check state.
- `checks`: passed, failed, pending, skipped, cancelled, and unknown totals.
- `review-threads`: total, unresolved, human unresolved, bot unresolved,
  unknown-author unresolved, and outdated totals.
- `labels` and `sync-labels`: counts by planned action.
- `doctor`: counts by status plus an overall readiness state.

Empty structured outputs should still include `count: 0` or an equivalent
summary and a useful `help[]` field.

## Repository Snapshot v1

`baton snapshot --json` returns:

- `repository`: the validated GitHub `owner/name` identity;
- `acquisition.startedAt` and `acquisition.completedAt` in UTC;
- `completeness`: `complete` or `degraded`;
- `warnings[]`: scoped, safe acquisition evidence;
- `queue`: the queue facts also projected as `queueSnapshot` v1;
- `branches[]` and `pullRequests[]`: Baton-owned facts with stable repository
  and revision identities;
- `recommendation`: one Outcome, optional Action, machine-readable reasons,
  selected and deferred Candidates, and instructions.

Outcome values are `actionable`, `human_choice_required`, `waiting`, `blocked`,
`idle`, and `degraded`. Action values are `issue_implementation`,
`issue_investigation`, `pull_request_follow_up`, and `branch_health`. Waiting,
blocked, idle, and degraded Recommendations omit Action. A human-choice result
may name a common Action when choosing among tied work, or omit Action when a
single ready item needs human disposition. `selectionRequired` is true only
when the Candidate set contains multiple alternatives.

Every Candidate repeats the repository identity. Pull-request Candidates also
include base/head refs and SHAs; branch Candidates include ref and SHA. This
makes a Candidate an observed revision identity, not a claim or execution
record.

## Repository Reconciliation Plan v2

`baton init --dry-run --json` returns `repositoryReconciliationPlan` v2. A
Plan includes a stable `planId`, absolute repository root, and one operation
per managed file with action, ownership, conflict state, exact desired content,
a full before/after diff, and an observed absent-or-SHA-256 precondition.

Apply preflights every conflict and precondition before staging any target
replacement. Content is staged in the destination directory and renamed
atomically per file. Multi-file and remote effects cannot be transactional, so
apply results include an Operation Report.

## Operation Report v1

`pr-transition --apply --json` embeds `report` beside the plan fields. Each
planned issue-label operation ends as `applied`, `unchanged`, `refused`,
`failed`, or `not_attempted`; stale PR state is represented by a refused
preflight result. The dry-run plan is deterministic and has this core shape:

```json
{"schemaVersion":1,"kind":"workItemTransitionPlan","repository":"owner/repo","eventAction":"closed","pullRequestNumber":42,"flow":"work","operations":[{"id":"issue-7-awaiting-review","issueNumber":7,"action":"add_labels","label":"needs:review"}],"warnings":[]}
```

Multi-operation mutations use `operationReport` v1. Overall status is
`completed`, `refused`, `partial`, or `failed`. Each operation has a stable ID,
resource, action, and one status: `applied`, `unchanged`, `refused`, `failed`,
or `not_attempted`.

Label sync, issue-policy apply, work-item transition,
repository-file reconciliation, and branch reconciliation preserve the Report
when a later effect fails. A returned error therefore does not erase evidence
of effects already applied. Structured failures remain one `error` v1 object;
that object includes the optional `report` field when mutation was attempted.
Successful `issuePolicyDecision` v1 JSON likewise adds the optional `report`
field after `--apply` while preserving the decision's existing top-level shape.

## Truncation

Output truncation is a rendering concern. Internal policy and queue decisions
must still have access to full data when needed.

Fields that may contain long markdown, generated config, logs, or comments
should default to bounded output in agent-facing formats:

- `bodyPreview`
- `bodyChars`
- `bodyTruncated`
- `fullCommand`

Commands with truncation must support:

```sh
--full
--body-limit <chars>
```

Primary targets:

- `review-threads`
- `pr`
- `migrate-config --dry-run`

## Help Arrays

Structured outputs may include `help`, an ordered array of concrete next steps.
Entries should be command strings or short stop-condition instructions.

Examples:

```json
{
  "help": [
    "Run `baton next --format toon`.",
    "Prepare an isolated checkout before editing the selected branch."
  ]
}
```

Guidelines:

- Prefer commands that can be copied directly.
- Use placeholders only when the runtime value is unknown.
- Include stop conditions when continuing could require human judgment.
- Keep arrays short, usually one to three entries.

## Home View

Running `baton` with no arguments renders the same local dashboard as
`baton home`. `baton --help` keeps global help output.

The home view should not fail just because config, auth, or remote state is
missing. Missing pieces should be explicit fields in the result:

```text
bin: ~/go/bin/baton
description: Coordinate GitHub issue/PR agent workflows for this repository
repo: sjunepark/baton
config: missing (.github/baton.yml)
auth: ok (gh auth token)
next: unavailable (config missing)
help[3]:
  Run `baton init --dry-run --json`
  Run `baton doctor --format toon`
  Run `baton --help`
```

Paths in compact output should be home-relative when possible.
