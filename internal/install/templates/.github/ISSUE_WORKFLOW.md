# GitHub Issue Workflow

Managed by Baton. Edit in this repository when your issue workflow changes.

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

- `needs-info`: The issue is missing required policy fields or has another
  blocker.
- `needs:discussion`: A human decision is needed before implementation.
- `needs:review`: Agent work has been committed and needs human review.

When a work PR merges to `agent`, Baton's transition workflow idempotently
adds `needs:review` to its referenced open issues. Closed issues and issues
already carrying the label are unchanged.

## Policy Gate

The issue policy action runs on issue open and edit events. It reads the issue
form, updates controlled labels, and posts one updatable policy comment when a
ready issue is incomplete.

Ready issues require summary, context/evidence, and acceptance criteria. If a
ready issue is missing required fields, Baton adds `needs-info`. When the form
becomes complete, Baton removes `needs-info`.

## PR Protocol

Work PRs target `agent` and reference issues with `Refs #123`. Promotion PRs
target `main` from `agent` and close promoted issues with closing keywords such
as `Closes #123`.
