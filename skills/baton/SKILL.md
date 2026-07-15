---
name: baton
description: Use the Baton CLI to run reusable GitHub issue/PR agent workflows, including creating Baton-ready GitHub issue todos, queue triage, PR follow-up, review-thread inspection, CI check handling, policy-gated issue intake, branch/ref guidance, scheduled Baton-managed Codex automation, and adopter updates. Use when asked to create, convert, or triage todos for Baton-managed agents, run or inspect Baton-managed agent work, automate GitHub issue intake, follow up agent PRs, set up recurring Codex automation with Baton, update repositories that adopted Baton, or migrate Baton policy.
---

# Baton

Baton provides deterministic GitHub, git, policy, queue, and branch/ref facts.
Use Baton compact output for agent context, Baton JSON for automation
contracts, and your judgment for code changes.

## Core Rules

- Treat this `sjunepark/baton` skill package as the canonical source for Baton
  skill behavior. Downstream repositories with local copies should refresh from
  the skills CLI or the owning distribution flow instead of hand-editing copied
  skill files.
- Run `baton home --format toon` or `baton doctor --format toon` to establish
  local Baton context.
- Run `baton snapshot --format toon` before selecting unattended work.
- Act only when `recommendation.outcome` is `actionable`, then choose exactly
  one snapshot candidate per automation run. Human-choice, waiting, blocked,
  idle, and degraded outcomes are stop/report states.
- Verify you are in a caller-provided isolated checkout before editing files.
- Work only inside that isolated checkout.
- Never mutate the user's primary checkout for automation work.
- Never merge unless the user explicitly asks.
- Prefer Baton compact or JSON output over manual GitHub browsing for issues,
  PRs, checks, and review threads.
- Stop and report on auth failures, missing isolation, ambiguous scope, human
  product/security/schema decisions, unrelated dirty state, or unsafe branch
  checkout state.

## Commands

Use `$baton <command> <arguments>` as the user-facing skill command surface.
The command name supplies the workflow; everything after it is the true
argument.

| Skill command | Behavior |
| --- | --- |
| `$baton` | Show readiness, queue summary, and 2-3 recommended next commands. Read-only. |
| `$baton status [repo]` | Run readiness and setup checks. Read-only. |
| `$baton next [repo]` | Show the next Baton candidate set. Read-only. |
| `$baton queue [repo]` | Show eligible and skipped issues/PRs. Read-only. |
| `$baton todo <todo>` | Create one Baton-ready GitHub issue. No branch, commit, or PR. |
| `$baton todos <notes-or-file>` | Split notes into Baton-ready GitHub issues. No implementation. |
| `$baton investigate <issue>` | Investigate/comment on one issue. No file edits unless the user explicitly changes scope. |
| `$baton implement <issue>` | In a caller-provided isolated checkout, implement one ready issue, validate, and open/update a PR to the staging branch. |
| `$baton follow-up <pr>` | In a caller-provided isolated checkout, fix checks or review follow-up, validate, and push to that branch. |
| `$baton run [repo]` | Let Baton return candidates, choose exactly one safe unit, then stop. |
| `$baton adopt [repo]` | Check target-repo setup with dry-run/read-only commands; never report adoption complete while doctor is blocked. |
| `$baton update [repo]` | Check and update an existing Baton adoption through a normal reviewed PR. Do not merge. |
| `$baton automate [repo]` | Explain or prepare scheduled one-unit automation. Do not schedule implementation automation before a manual run succeeds. |

## Routing

- No argument means read-only dashboard/menu. Never auto-run mutating work.
- If the first word matches a command, load only the reference needed for that
  command and execute that workflow.
- If intent clearly maps to a command, proceed as if that command was invoked;
  for example, "create a Baton todo for X" maps to `$baton todo X`.
- If two mutating commands could fit, ask one short clarification before
  acting.
- Preserve command-level consent boundaries:
  - `$baton`, `status`, `next`, and `queue` are read-only.
  - `todo` and `todos` may create GitHub issues but must not create branches,
    commits, PRs, or merges.
  - `investigate` may comment on an issue but must not edit files unless the
    user explicitly changes scope.
  - `update` may edit Baton setup files and open/update a normal PR only after
    reading `references/updating-adopters.md` and running dry-run checks.
  - `implement`, `follow-up`, and `run` may edit only inside a caller-provided
    isolated checkout.
  - No command may merge unless the user explicitly asks and target repo policy
    allows it.

## Todo Creation

- Use GitHub issues as the Baton todo queue.
- Create todos with the repository's Agent-readable work item issue template.
- If creating issues through an API instead of the GitHub form UI, write the
  body with the same `###` headings from the template.
- Split unrelated work into separate issues.
- Prefer durable problem and outcome descriptions over exact line numbers,
  private helper names, or speculative implementation steps.
- Choose the least-permissive Agent mode that fits:
  - Ready trivial: tiny obvious fix.
  - Ready bounded: scoped implementation work with clear acceptance criteria.
  - Investigate only: research/report only.
  - Needs discussion: human decision required before implementation.
- Set Priority to P2 for ordinary todos unless the user explicitly indicates
  urgent, blocking, or lower-priority work.
- Do not mark vague work as ready for implementation.
- Do not create branches or PRs when only asked to create todos.
- For detailed todo-creation prompts, read `references/todo-creation.md`.

## Command Workflows

- `$baton`: run `baton home --format toon` or `baton doctor --format toon`,
  then `baton queue --format toon` and `baton next --format toon` when a repo
  is known. Report state and exact next skill commands only.
- `status`: from the target repository checkout, run
  `baton doctor --repo <repo> --format toon`, plus `baton ensure-branch --json`
  and `baton sync-labels --dry-run --repo <repo> --json` when setup is in
  scope. Do not apply setup.
- `next`: run `baton next --format toon --repo <repo>` and report the
  highest-priority candidate set, selection reason, and any deferred eligible
  items without taking it. Use `baton queue --format toon --repo <repo>` for
  the full eligible issue list.
- `queue`: run `baton queue --format toon --repo <repo>` and summarize eligible
  and skipped work.
- `todo` and `todos`: read `references/todo-creation.md`, create issue bodies
  with the required `###` headings, preflight with `baton issue-policy
  --body-file <tmp-file> --json`, then create issues with `gh issue create`.
- `investigate`: inspect the issue through Baton/GitHub, confirm investigation
  scope, run focused diagnostics, and comment findings. Do not edit files.
- `implement`: run `baton snapshot --format toon --repo <repo>` and continue
  only when `recommendation.outcome` is `actionable`,
  `recommendation.action` is `issue_implementation`, and
  `recommendation.candidates` contains the requested issue identity. That
  selection proves durable ownership and current intake eligibility. Then
  verify the isolated checkout, create a work branch from the staging branch,
  validate, and open/update a PR to the staging branch with `Refs #<issue>`.
- `follow-up`: run `baton pr <number> --json`, `baton checks <number> --format
  toon`, and `baton review-threads <number> --format toon`; verify the
  isolated checkout, check out the existing PR branch, and push fixes there.
- `run`: run `baton snapshot --format toon --repo <repo>`. Continue only when
  `recommendation.outcome` is `actionable`; choose exactly one returned
  candidate, handle it according to `recommendation.action`, validate, report
  the chosen candidate, and stop. All other outcomes are report-and-stop states.
- `adopt`: from the target repository checkout, run read-only/dry-run setup
  checks: `baton home --format toon`,
  `baton doctor --repo <repo> --format toon`, `baton init --dry-run --json`,
  `baton migrate-config --dry-run` when a legacy policy exists,
  `baton sync-labels --dry-run --repo <repo> --json`, and
  `baton ensure-branch --json`. Treat every failed doctor check as an adoption
  blocker with its reported remediation. After approved setup changes, rerun
  doctor against the live repository; do not report adoption complete or
  enable automation while `readyState` is `blocked`. If it is `degraded`,
  report the reduced capability explicitly. If init used a reviewed custom
  `--go-install` or `--install-command`, pass the same option to doctor.
- `update`: read `references/updating-adopters.md`, inspect
  `.github/baton.yml` and `.github/workflows/*`, read the relevant adopter
  update notes, run the dry-run checks listed in the reference, then open or
  update a normal reviewed PR for needed Baton runtime, baseline, config,
  template, or label changes. Do not merge.
- `automate`: read `references/automation-setup.md`, verify prerequisites with
  read-only commands, and inspect the owning automation platforms' scheduler
  listings to prove the proposed schedule will leave exactly one unattended
  dispatcher for the repository. If read-only evidence cannot establish that
  condition, stop and require explicit operator confirmation before preparing
  or scheduling automation. Use `$baton run --repo owner/name` when repo
  selection must be explicit.

## PR Follow-Up

- Inspect the PR with `baton pr <number> --json`.
- Fix failing required checks before review nits.
- Inspect checks with `baton checks <number> --format toon`.
- Inspect review threads with `baton review-threads <number> --format toon`.
- Human unresolved review threads outrank bot comments and may require stopping.
- Push fixes to the existing PR branch. Do not open a new PR.

## Issue Intake

- Confirm `snapshot` selected the requested issue with
  `recommendation.outcome` equal to `actionable` and `recommendation.action`
  equal to `issue_implementation`; an implementation label alone is not
  durable managed ownership.
- Create work from the configured staging branch in the isolated checkout.
- Open the work PR to the staging branch with `Refs #<issue>`, not closing
  keywords.
- Do not close issues from work PRs. Baton derives issue-backed work included
  in each promotion, then explicitly closes the sealed issue set after the
  promotion merges. Promotion closing references are optional presentation and
  must exactly match that set when used.

## References

- For creating Baton-ready GitHub issue todos, read
  `references/todo-creation.md`.
- For commands and common flags, read `references/commands.md`.
- For reviewed delivery-ledger initialization, migration, and cutover, read
  `references/delivery-bootstrap.md`.
- For JSON fields to inspect before acting, read `references/json-contracts.md`.
- For target-repo setup and scheduled Codex app automations that run Baton, read
  `references/automation-setup.md`.
- For updating repositories that already adopted Baton, read
  `references/updating-adopters.md`.
