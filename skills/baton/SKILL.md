---
name: baton
description: Use the Baton CLI to run reusable GitHub issue/PR agent workflows, including queue triage, PR follow-up, review-thread inspection, CI check handling, safe worktree leasing, policy-gated issue intake, and Baton-managed Codex automation. Use when asked to run or inspect Baton-managed agent work, automate GitHub issue intake, follow up agent PRs, migrate Baton policy, or operate inside a Baton lease.
---

# Baton

Baton provides deterministic GitHub, git, policy, queue, and worktree facts.
Use Baton compact output for agent context, Baton JSON for automation
contracts, and your judgment for code changes.

## Core Rules

- Run `baton home --format toon` or `baton doctor --format toon` to establish
  local Baton context.
- Run `baton next --format toon` before selecting unattended work.
- Handle exactly one Baton-selected unit per automation run.
- Acquire a lease before editing files: `baton lease ... --json`.
- Work only inside the returned lease `path`.
- Never mutate the user's primary checkout for automation work.
- Never merge unless the user explicitly asks.
- Prefer Baton compact or JSON output over manual GitHub browsing for issues,
  PRs, checks, and review threads.
- Stop and report on auth failures, lease conflicts, ambiguous scope, human
  product/security/schema decisions, or dirty lease release conflicts.

## Workflow

1. If readiness is uncertain, run `baton home --format toon` or
   `baton doctor --format toon`; otherwise run the narrow command needed for
   the current task.
2. Run `baton next --format toon`.
3. If the action is `none` or `digest`, report the summary and stop.
4. Acquire a lease for the selected action.
5. `cd` to the lease path and read that repo's `AGENTS.md`.
6. Implement or investigate only the selected unit.
7. Validate with focused checks.
8. Push/comment according to the selected action.
9. Release the lease when clean. If release refuses a dirty worktree, report the
   lease path and changed files.

## PR Follow-Up

- Inspect the PR with `baton pr <number> --json`.
- Fix failing required checks before review nits.
- Inspect checks with `baton checks <number> --format toon`.
- Inspect review threads with `baton review-threads <number> --format toon`.
- Human unresolved review threads outrank bot comments and may require stopping.
- Push fixes to the existing PR branch. Do not open a new PR.

## Issue Intake

- Confirm the issue is implementation-ready and has no skip labels.
- Create work from the configured staging branch in a Baton lease.
- Open the work PR to the staging branch with `Refs #<issue>`, not closing
  keywords.
- Do not close issues from work PRs; closure belongs to promotion PRs.

## References

- For commands and common flags, read `references/commands.md`.
- For JSON fields to inspect before acting, read `references/json-contracts.md`.
