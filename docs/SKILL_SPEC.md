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
description: Use the Baton CLI to run reusable GitHub issue/PR agent workflows, including creating Baton-ready GitHub issue todos, queue triage, PR follow-up, review-thread inspection, CI check handling, safe worktree leasing, policy-gated issue intake, and scheduled Baton-managed Codex automation. Use when asked to create, convert, or triage todos for Baton-managed agents, run or inspect Baton-managed agent work, automate GitHub issue intake, follow up agent PRs, set up recurring Codex automation with Baton, migrate Baton policy, or operate inside a Baton lease.
```

## Required Skill Behavior

The skill must instruct Codex to:

- Run `baton home --format toon` or `baton doctor --format toon` when
  repository readiness is uncertain.
- Run `baton next --format toon` before selecting work.
- Prefer Baton compact output or JSON over manual GitHub browsing for queue
  state.
- Create todos as structured GitHub issues with the Agent-readable work item
  template when asked to prepare Baton-managed work.
- Use issue-form-compatible `###` headings when creating issues through an API
  instead of the GitHub form UI.
- Choose the least-permissive Agent mode that fits and avoid marking vague work
  ready for implementation.
- Acquire a lease before any file edits.
- Work only inside the leased path.
- Handle exactly one unit of work per automation run.
- Push to the existing PR branch for PR follow-up.
- Open a new PR only for issue intake when Baton selected an issue
  implementation action.
- Never merge unless explicitly requested.
- Stop on ambiguous scope, human decision needs, risky data/schema/security
  changes, dirty lease conflicts, or auth failures.

## Skill Workflow

### General Automation

1. Run `baton next --format toon`.
2. If action is `none` or `digest`, report the summary and stop.
3. Run `baton lease` with the selected action.
4. Change to the lease path.
5. Read target repo `AGENTS.md`.
6. Implement or investigate exactly the selected unit.
7. Validate with focused checks first.
8. Push/comment according to Baton's selected action.
9. Release or retain the lease based on cleanliness and result.

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
5. Fill Summary, Context / evidence, and Acceptance criteria with actionable
   detail.
6. Use optional Non-goals / constraints and Validation hints when they reduce
   ambiguity.
7. Choose the least-permissive Agent mode that fits.
8. Do not create branches or PRs when only asked to create todos.

### Investigation-Only

1. Do not edit files unless the user explicitly changes scope.
2. Inspect repository and run diagnostics.
3. Comment findings, evidence, and recommended next label.

## Stop Conditions

Stop and report instead of editing when:

- Baton cannot acquire a lease.
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
- No scripts initially. The CLI owns scripts.

Keep `SKILL.md` short and point to Baton CLI help for detailed command syntax.

## Automation Prompt Pattern

Issue intake:

```text
Use the Baton skill in this repository. Run Baton to select one safe next issue
or investigation item, acquire a lease, complete that one unit, validate, push
or comment as appropriate, release the lease when clean, and stop. Do not merge.
```

PR follow-up:

```text
Use the Baton skill in this repository. Run Baton to select one open agent PR
that needs follow-up, acquire a lease for its branch, fix failing checks or
blocking review feedback, validate, push to the existing branch, comment with
results, release the lease when clean, and stop. Do not merge.
```

Todo creation:

```text
Use the Baton skill in this repository. Create Baton-ready GitHub issues from
the notes below using the Agent-readable work item issue template. Split
unrelated work, use issue-form-compatible Markdown headings if creating issues
through an API, choose the least-permissive Agent mode that fits, include clear
context/evidence and acceptance criteria, and do not implement, branch, open a
PR, or merge.
```
