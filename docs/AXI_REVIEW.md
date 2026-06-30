# AXI Review

This review tracks Baton's agent-facing CLI shape.

## Current Direction

- Baton returns deterministic facts and policy decisions.
- Read-heavy commands support compact `--format toon` output for agents and
  JSON for automation contracts.
- Mutating setup commands require explicit apply or confirmation flags.
- Worktree leasing and cleanup are outside Baton. Agents must work inside a
  caller-provided isolated checkout before editing files.

## Maintained Checks

- `baton` with no arguments renders the home view; `baton --help` remains
  global help.
- `<command> --help` and `baton help <command>` should exit `0` on stdout for
  supported commands.
- Structured-mode errors should use the shared error object with category,
  exit code, message, hint, and retryability.
- High-volume outputs should include counts, summaries, truncation metadata
  when needed, and short `help[]` next steps.

## Validation

Run before changing public output contracts:

```sh
go test ./...
go run ./cmd/baton
go run ./cmd/baton init --dry-run --json
go run ./cmd/baton doctor --json
go run ./cmd/baton queue --help
go run ./cmd/baton next --help
```
