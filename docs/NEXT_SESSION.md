# Next Session

## Suggested Opening Prompt

```text
We are in /Users/sejunpark/IT/baton. Read AGENTS.md, README.md,
ARCHITECTURE.md, docs/REQUIREMENTS.md, and docs/IMPLEMENTATION_PLAN.md. Continue
from the implemented Baton CLI and keep docs, tests, templates, and the bundled
skill aligned with the current command surface. Run focused validation before
finishing, usually `go test ./...`.
```

## Recommended Next Slice

1. Inspect the requested area and current command behavior before editing docs.
2. Keep generated templates, `docs/CONFIG_SPEC.md`, and `config.DefaultConfig`
   in sync.
3. Update `skills/baton` references when command usage or JSON contracts change.
4. Run `go test ./...` after code, template, or contract changes.

Avoid relying on the old Creo scripts unless checking parity for behavior Baton
does not already cover with Go tests.

## Context To Preserve

- Baton is Go-first and ships a bundled skill under `skills/baton/`.
- Target repositories use `.github/baton.yml`; legacy
  `.github/agent-issue-policy.yml` remains readable for migration.
- Automation work must happen only inside caller-provided isolated checkouts.
- Baton must not manage worktree lease, release, or cleanup lifecycle.
- Mutating GitHub or git commands need dry-run/planner coverage or explicit
  apply/confirmation gates.
