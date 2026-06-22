# Next Session

## Suggested Opening Prompt

```text
We are in /Users/sejunpark/IT/baton. This repo is planning-only so far.
Read AGENTS.md, README.md, ARCHITECTURE.md, docs/REQUIREMENTS.md, and
docs/IMPLEMENTATION_PLAN.md. Then implement Phase 0 and the first useful part
of Phase 1 for Baton, a Go CLI that extracts the GitHub issue/PR policy workflow
from /Users/sejunpark/IT/creo. Keep the first slice small, testable, and do not
migrate Creo yet.
```

## Recommended First Slice

1. Initialize the Go module.
2. Add a minimal `baton` CLI with `--help` and `version`.
3. Add internal package scaffolding for config and policy.
4. Port only pure issue-section parsing first.
5. Add tests from small inline fixtures.

Do not start with GitHub API calls or worktree leasing. The highest-value first
proof is pure policy parity because it can be tested without side effects.

## Context To Preserve

- The source behavior lives in `/Users/sejunpark/IT/creo/scripts/github/`.
- The target installed config should eventually become `.github/baton.yml`.
- Legacy Creo config `.github/agent-issue-policy.yml` should remain readable
  during migration.
- Baton should be Go-first.
- Baton should include a bundled Codex skill later under `skills/baton/`.
- Automation work must eventually happen only inside Baton-managed leases.

