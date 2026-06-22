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
- stable item identity such as issue number, PR number, lease ID, or check name
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
- `review-threads`: total, unresolved, human unresolved, bot unresolved, and
  outdated totals.
- `labels` and `sync-labels`: counts by planned action.
- `leases` and `prune`: counts by lease status or cleanup action.
- `doctor`: counts by status plus an overall readiness state.

Empty structured outputs should still include `count: 0` or an equivalent
summary and a useful `help[]` field.

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
- `complete --summary`
- `complete --validation`

## Help Arrays

Structured outputs may include `help`, an ordered array of concrete next steps.
Entries should be command strings or short stop-condition instructions.

Examples:

```json
{
  "help": [
    "Run `baton next --format toon`.",
    "Run `baton lease --purpose issue-123 --base origin/agent --new-branch agent-work/issue-123 --json`."
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
leases: 0 active
next: unavailable (config missing)
help[3]:
  Run `baton init --dry-run --format toon`
  Run `baton doctor --format toon`
  Run `baton --help`
```

Paths in compact output should be home-relative when possible.
