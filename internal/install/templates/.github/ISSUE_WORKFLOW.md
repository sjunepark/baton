# GitHub Issue Workflow

This repository uses structured GitHub issues for a Baton-managed,
agent-readable workflow. GitHub Issues are the operational queue; labels are
derived from form fields where possible and define what agents may do.

## Labels

Implementation labels:

- `agent:ready-trivial`: Agents may make narrowly scoped, obvious fixes.
- `agent:ready-bounded`: Agents may implement bounded work from the stated
  summary, evidence, and acceptance criteria.

Comment-only label:

- `agent:investigate-only`: Agents may inspect and comment with findings. They
  must not commit behavior changes for this issue.

Skip labels:

- `agent:blocked`: The issue is missing required policy fields or has another
  blocker.
- `needs:discussion`: A human decision is needed before implementation.
- `needs:review`: Agent work has been committed and needs human review.

## Policy Gate

The issue policy action runs on issue open and edit events. It reads the issue
form, updates controlled labels, and posts one updatable policy comment when a
ready issue is incomplete.

Ready issues require summary, context/evidence, and acceptance criteria. If a
ready issue is missing required fields, Baton adds `agent:blocked`. When the
form becomes complete, Baton removes `agent:blocked`.

## PR Protocol

Work PRs target `agent` and reference issues with `Refs #123`. Promotion PRs
target `main` from `agent` and close promoted issues with closing keywords such
as `Closes #123`.
