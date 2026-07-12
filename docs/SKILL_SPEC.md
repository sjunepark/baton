# Baton Skill Spec

## Purpose

Baton should ship a Codex skill under:

```text
skills/baton/
```

The skill is the agent-facing process layer for using the Baton CLI. It should
not implement GitHub API logic or policy parsing in prose. It should tell Codex
which Baton commands to run, how to interpret the results, and when to stop.

## Trigger Description

Draft description:

```yaml
name: baton
description: Use the Baton CLI to run reusable GitHub issue/PR agent workflows, including creating Baton-ready GitHub issue todos, queue triage, PR follow-up, review-thread inspection, CI check handling, policy-gated issue intake, branch/ref guidance, scheduled Baton-managed Codex automation, and adopter updates. Use when asked to create, convert, or triage todos for Baton-managed agents, run or inspect Baton-managed agent work, automate GitHub issue intake, follow up agent PRs, set up recurring Codex automation with Baton, update repositories that adopted Baton, or migrate Baton policy.
```

## Required Skill Behavior

The skill must instruct Codex to:

- Treat `$baton <command> <arguments>` as the user-facing command router for
  common Baton workflows.
- Treat `$baton` with no arguments as a read-only dashboard/menu, never as
  permission to start mutating work.
- Treat everything after a recognized skill command as the command argument so
  users do not need to paste boilerplate workflow prompts.
- Run `baton home --format toon` or `baton doctor --format toon` when
  repository readiness is uncertain.
- Run `baton snapshot --format toon` before selecting unattended work. Proceed
  only when `outcome` is `actionable`; every other outcome is report-and-stop.
- Prefer Baton compact output or JSON over manual GitHub browsing for queue
  state.
- Create todos as structured GitHub issues with the Agent-readable work item
  template when asked to prepare Baton-managed work.
- Use issue-form-compatible `###` headings when creating issues through an API
  instead of the GitHub form UI.
- Prefer durable problem and outcome descriptions over volatile code
  coordinates such as exact line numbers, private helper names, or speculative
  implementation steps.
- Choose the least-permissive Agent mode that fits and avoid marking vague work
  ready for implementation.
- Verify a caller-provided isolated checkout before any file edits.
- Work only inside that isolated checkout.
- Handle exactly one unit of work per automation run.
- Push to the existing PR branch for PR follow-up.
- Open a new PR only after choosing an issue implementation candidate or when
  `$baton update` finds adopter-update changes.
- Never merge unless explicitly requested.
- Stop on ambiguous scope, human decision needs, risky data/schema/security
  changes, missing isolation, unrelated dirty state, or auth failures.

## Skill Command Router

The bundled skill must expose these concise commands:

| Skill command | Behavior |
| --- | --- |
| `$baton` | Show readiness, queue summary, and recommended next commands. Read-only. |
| `$baton status [repo]` | Run readiness and setup checks. Read-only. |
| `$baton next [repo]` | Show the highest-priority Baton candidate set and deferred eligible items. Read-only. |
| `$baton queue [repo]` | Show eligible and skipped issues/PRs. Read-only. |
| `$baton todo <todo>` | Create one Baton-ready GitHub issue. No branch or PR. |
| `$baton todos <notes-or-file>` | Split notes into Baton-ready GitHub issues. No implementation. |
| `$baton investigate <issue>` | Investigate/comment on one issue. No edits unless explicitly respecified. |
| `$baton implement <issue>` | In a caller-provided isolated checkout, implement one ready issue, validate, and open/update a staging PR. |
| `$baton follow-up <pr>` | In a caller-provided isolated checkout, fix checks or review follow-up on the existing PR branch. |
| `$baton run [repo]` | Let Baton return candidates, choose exactly one safe unit, then stop. |
| `$baton adopt [repo]` | Check target-repo Baton setup with dry-run/read-only commands. |
| `$baton update [repo]` | Check and update an existing Baton adoption through a normal reviewed PR. Do not merge. |
| `$baton automate [repo]` | Explain or prepare scheduled one-unit automation. |

Routing rules:

- If the first word matches a command, execute that workflow.
- If intent clearly maps to a command, route to it without asking for a long
  prompt; for example, "create a Baton todo for X" maps to `$baton todo X`.
- If two mutating commands could fit, ask one short clarification.
- `todo` and `todos` may create GitHub issues only.
- `update` may edit Baton setup files and open/update a normal PR only after
  reading `references/updating-adopters.md` and running dry-run checks.
- `implement`, `follow-up`, and `run` may edit only inside a caller-provided
  isolated checkout.

## Skill Workflow

### General Automation

1. Run `baton snapshot --format toon`.
2. Unless `outcome` is `actionable`, report the outcome/reasons and stop.
3. Choose exactly one returned candidate and follow the typed `action`.
4. Verify the current working directory is a caller-provided isolated checkout.
5. Check out the PR `headRef` for follow-up or create an issue-work branch from
   the configured staging branch.
6. Read target repo `AGENTS.md`.
7. Implement or investigate exactly the chosen candidate.
8. Validate with focused checks first.
9. Push/comment according to `selectedAction` and the chosen candidate.
10. Report the summary and validation evidence to the caller, then stop. Coda
    or the invoking automation owns execution completion; Baton does not keep
    a parallel completion ledger.

### PR Follow-Up

1. Run `baton pr <number> --json` or use data returned by `baton next`.
2. Fix failing checks before bot nits.
3. Read unresolved review threads with
   `baton review-threads <number> --format toon`.
4. Human unresolved threads block auto-merge and outrank bot comments.
5. Apply safe fixes only.
6. Push to the existing PR branch.
7. Comment with summary, validation, and remaining risk.

### Issue Intake

1. Confirm issue has implementation label and no skip labels.
2. Branch from configured staging branch.
3. Implement the smallest change satisfying acceptance criteria.
4. Validate.
5. Open PR to staging branch with `Refs #<issue>`, not closing keywords.
6. Add configured automation labels when available.

### Todo Creation

1. Use GitHub issues as the Baton todo queue.
2. Use the repository's Agent-readable work item issue template.
3. Use issue-form-compatible `###` headings when creating issues through an API.
4. Split unrelated work into separate issues.
5. Prefer durable problem and outcome descriptions over volatile code
   coordinates, because code can drift before the todo is picked up.
6. Fill Summary, Context / evidence, and Acceptance criteria with actionable
   detail.
7. Use optional Non-goals / constraints and Validation hints when they reduce
   ambiguity.
8. Set Priority to P2 for ordinary work unless the user explicitly indicates
   urgent, blocking, or lower-priority work.
9. Choose the least-permissive Agent mode that fits.
10. Do not create branches or PRs when only asked to create todos.

### Investigation-Only

1. Do not edit files unless the user explicitly changes scope.
2. Inspect repository and run diagnostics.
3. Comment findings, evidence, and recommended next label.

## Stop Conditions

Stop and report instead of editing when:

- A caller-provided isolated checkout is not available.
- The selected work conflicts with target repo policy.
- Requirements are ambiguous.
- A human review comment requires product judgment.
- Fix requires migrations, contracts, auth, billing, security, dependency locks,
  release workflows, or broad frontend behavior outside stated scope.
- Required GitHub auth or permissions are missing.
- The target branch is red from unrelated failures and Baton classified the item
  as blocked.

## Skill Resources

The skill may include:

- `references/commands.md`: compact command reference.
- `references/json-contracts.md`: key fields Codex should inspect.
- `references/todo-creation.md`: guidance and prompts for creating
  Baton-ready GitHub issue todos.
- `references/updating-adopters.md`: guidance for reviewing and updating
  repositories that already adopted Baton.
- No scripts initially. The CLI owns scripts.

Keep `SKILL.md` short and point to Baton CLI help for detailed command syntax.

## Automation Prompt Pattern

Issue intake:

```text
$baton run
```

PR follow-up:

```text
$baton follow-up <pr>
```

Todo creation:

```text
$baton todos <notes-or-file>
```

Use [USER_FLOWS.md](USER_FLOWS.md) for human-facing flow examples that map
skill commands to equivalent CLI commands.
