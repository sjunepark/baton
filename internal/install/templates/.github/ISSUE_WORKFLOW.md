# GitHub Issue Workflow

Managed by Baton. Edit in this repository when your issue workflow changes.

This repository uses structured GitHub issues for a Baton-managed,
agent-readable workflow. GitHub Issues are the operational queue. A trusted
versioned issue comment records Baton ownership; labels are a query index and
define what agents may do, but do not establish ownership by themselves.

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

When a work PR merges to `agent`, the recorder commits its durable issue
references before Baton's transition idempotently adds `needs:review` to those
open issues. Later PR body edits cannot change the transition. Closed issues
and issues already carrying the label are unchanged.

## Policy Gate

The issue policy action runs on issue open/edit and trusted ownership-record
repair events. When the form first matches, it writes the ownership record
before controlled labels and the updatable policy comment. Later body or label
edits do not revoke ownership.

Ready issues require summary, context/evidence, and acceptance criteria. If a
ready issue is missing required fields, Baton adds `needs-info`. When the form
becomes complete, Baton removes `needs-info`.

## PR Protocol

Work PRs target `agent` and reference issues with `Refs #123`. Promotion PRs
target `main` from `agent`; Baton closes the exact sealed issue set after merge.
Closing references are optional presentation and must exactly match that set
when present. Ordinary and fork PRs are unmanaged. Using the reserved
`agent-work/` prefix in the same repository is explicit Baton intent and must
target `agent`. Once opened, a managed work PR is checked against durable issue
ownership and PR-local facts; later implementation or skip label edits affect
queue intake, not that PR's merge policy.
