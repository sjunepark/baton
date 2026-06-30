# Implementation Plan

## Current State

- Baton is a Go CLI with policy, queue, GitHub inspection, install, label sync,
  branch setup, doctor, migration, and completion commands.
- Baton ships a bundled Codex skill under `skills/baton/`.
- Baton no longer owns worktree leasing, release, pruning, or checkout cleanup.
  Execution isolation is caller-provided; see
  [EXECUTION_CONTEXT.md](EXECUTION_CONTEXT.md).
- Target repositories use `.github/baton.yml`; legacy
  `.github/agent-issue-policy.yml` remains readable for migration.
- Automation-facing commands are JSON-first, with TOON output for compact agent
  context on read-heavy commands.

## Maintained Scope

- Keep deterministic policy and queue decisions in Go.
- Keep agent judgment, implementation choices, and escalation decisions in the
  skill/Codex layer.
- Keep mutating repository or GitHub operations behind explicit `--apply`,
  `--yes`, or command-specific user intent.
- Keep docs, tests, templates, and the bundled skill aligned with the current
  command surface.

## Next Work

- Add targeted tests when changing public JSON, TOON, exit-code, or config
  behavior.
- Prefer pure planners for branch and install mutations.
- Keep generated templates concise and repository-editable.
- Run focused validation after each code or contract slice, usually
  `go test ./...`.
