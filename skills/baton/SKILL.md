---
name: baton
description: Use the Baton CLI to manage explicitly enrolled GitHub issue Tasks: create and enroll todos, inspect or select Tasks, classify mode/priority/blockers, mark advisory activity, and explicitly close work. Use when asked for Baton todos, task triage, the next Task, or implementation of an enrolled issue.
---

# Baton

Baton is an issue Task manager. `baton:managed` is the complete enrollment
fact. Fixed labels determine mode, priority, blockers, and advisory activity;
issue bodies and comments are never authoritative Baton state.

## Boundaries

- Use the CLI for deterministic Task facts and transitions; use agent judgment
  to suggest classification and follow the target project's instructions for
  implementation.
- Never edit an existing issue body to make it Baton-compatible.
- Comments are optional human explanations and do not enroll or classify.
- Mutations apply on explicit verbs. Preview with `--dry-run` when review is
  useful; do not invent an extra confirmation flag.
- Baton does not prescribe checkouts, branches, commits, pull requests, CI,
  review, merge, release, or delivery.

## Skill workflows

### Inspect and select

- `$baton`: run `baton list` and summarize current Tasks.
- `$baton list [repo]`: list enrolled Tasks; use `--state` when requested.
- `$baton show ISSUE [repo]`: show one Task. Use `--full` only when the full
  issue body is necessary.
- `$baton next [repo]`: run `baton next`. A definitive empty result means
  there is no ready Task; do not substitute a blocked or unenrolled issue.

### Create todos

For `$baton todo <request>` or `$baton todos <notes-or-file>`, read
`references/todo-creation.md`. Create ordinary GitHub issues with clear
project-owned bodies, then explicitly enroll each created issue with the
least-permissive suitable mode and chosen priority. Split unrelated outcomes.
Do not start implementation unless the user also asked for it.

### Enroll or classify an existing issue

1. Inspect the issue and its labels without changing its body.
2. Choose or ask for one mode: `trivial`, `bounded`, or `investigate`.
3. Choose a priority when it should differ from the default `p2`, and identify
   `needs-info` or `needs:discussion` blockers.
4. If there is no blocker, preview `baton enroll ISSUE ... --dry-run`, then
   enroll when the request authorizes it. If a blocker is needed and the issue
   has no fixed mode label, enroll first without a mode so the Task remains
   blocked, then preview and apply one `update` that sets mode/priority and adds
   the blocker. If a fixed mode label already exists, require the project's
   approved workflow to add the blocker before classified enrollment or leave
   the issue unenrolled. Use `baton update` for later classification changes.
5. Preserve all project labels. Use `baton unenroll` only on explicit intent;
   it reversibly removes Baton enrollment/activity, not project data.

### Implement one Task

`$baton implement ISSUE` is only a convenience around explicit Task state:

1. Run `baton show ISSUE` and confirm the Task is open, enrolled, unblocked,
   and permits the requested work.
2. Run `baton start ISSUE`.
3. Hand all implementation and validation decisions to the target project's
   own instructions and tools.
4. When interaction is available, ask whether to run `baton close ISSUE`.
   If it is unavailable, leave the Task open and report that exact command.
5. If work is abandoned or paused, run `baton stop ISSUE` when requested.

Do not infer completion from a commit, pull request, check, or merge. Only an
explicit `close` closes the GitHub issue.

## References

- Todo creation: `references/todo-creation.md`
- Current CLI syntax: `references/commands.md` or `baton COMMAND --help`
- Bundled-skill distribution: `DISTRIBUTION.md`
