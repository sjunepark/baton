# Baton v0.7 CLI

```text
baton [--repo owner/name] [--json] COMMAND [ARGS]
baton --version
```

Repository resolution uses `--repo`, then `GITHUB_REPOSITORY`, then a local
GitHub remote. Credentials use `GITHUB_TOKEN`, `GH_TOKEN`, or `gh` auth.
Run `baton COMMAND --help` for exact syntax.

## Reads

- `list [--state open|closed|all]` lists enrolled Tasks; default state is open.
- `show ISSUE [--full]` shows one enrolled Task. The default body is bounded;
  `--full` opts into the full GitHub body.
- `next` returns the first ready Task ordered by priority (`p0` to `p3`), then
  issue number. Mode does not affect ordering.

## Mutations

- `enroll ISSUE [--mode ...] [--priority ...] [--dry-run]`
- `update ISSUE [--mode ...|none] [--priority ...|none]
  [--add-blocker ...]... [--remove-blocker ...]... [--dry-run]`
- `unenroll ISSUE [--dry-run]`
- `start ISSUE [--dry-run]`
- `stop ISSUE [--dry-run]`
- `close ISSUE [--dry-run]`

A mutation applies unless `--dry-run` is present. `enroll` may omit mode or
priority, yielding a blocked Task until later `update`. `unenroll` removes
only enrollment and advisory activity. `close` requires an enrolled Task,
closes it when open, and succeeds as an idempotent no-op when already closed.

## Output and errors

Text is concise and derived from the same result values as JSON. `list` prints
`No tasks.` and `next` prints `No ready task.` for definitive empty results.
JSON contracts are defined in [OUTPUT_SPEC.md](OUTPUT_SPEC.md).

Exit codes are 0 success, 1 operational failure, and 2 invalid use. Unknown
commands and flags are rejected before network calls. Removed v0.6 commands
are ordinary unknown commands; there is no compatibility mode.
