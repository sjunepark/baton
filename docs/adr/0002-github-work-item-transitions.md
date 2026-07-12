# Derive work-item state from GitHub and persist only awaiting review

Baton will keep form-derived work kind and Agent Mode separate from Work Item
State. For an open issue, live linked work PRs represent `active_work_pr`; the
configured awaiting-review label represents `awaiting_review`; other policy
gates represent `blocked`; an otherwise capable issue is `ready`. A closed
issue is `promoted_or_closed`. Coda Run success is never an input to this
classification.

When a valid work PR is merged into the staging branch, an event-driven pure
planner adds the configured awaiting-review label to every referenced open
issue. The write is explicitly gated, idempotent, preflighted against current
GitHub state, and reported per issue. Agent-mode labels remain unchanged so
capability and lifecycle do not collapse into one label dimension.

Closing a work PR without merge, reopening it, or opening a replacement PR
does not write issue labels: live open PR state determines whether the issue is
active. Promotion PR closing keywords and manual issue closure remain GitHub's
authority for the terminal state. Reopening a terminal issue retains the
awaiting-review gate. Merged work-PR history is a fail-safe when the event
label write is delayed or fails, so removing the label alone intentionally
does not re-admit that issue. New implementation after staging review uses a
follow-up issue; closing the original remains the terminal human override.

Recommendation acquisition therefore reads merged staging-PR history. If that
history cannot be acquired within the bounded snapshot window, `snapshot`
returns `completeness: degraded` with a degraded Recommendation and no Action.
Both that snapshot behavior and the legacy `next` projection block a
recommendation instead of assuming the issue is ready.

Generated transition automation executes a released pinned Baton binary from
trusted base-branch code under `pull_request_target`; it never runs code from
the pull request checkout. A general event bus and a local lifecycle ledger
were rejected because there is one real transition today and GitHub already
owns the durable facts.
