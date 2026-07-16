# Derive work-item state from GitHub and persist only awaiting review

> Historical: superseded by the v0.7 issue-only Task contract. Project pull
> requests and delivery events no longer transition Baton Tasks.

Status: Superseded by v0.7.

Baton will keep form-derived work kind and Agent Mode separate from Work Item
State. For an open issue, live linked work PRs represent `active_work_pr`; the
configured awaiting-review label represents `awaiting_review`; other policy
gates represent `blocked`; an otherwise capable issue is `ready`. A closed
issue is `promoted_or_closed`. Coda Run success is never an input to this
classification.

When a valid work PR is merged into the staging branch, an event-driven pure
planner adds the configured awaiting-review label to every referenced open
issue from the exact committed staged-work record. The write is explicitly
gated, idempotent, preflighted against current GitHub state and durable issue
identity, and reported per issue. Agent-mode labels remain unchanged so
capability and lifecycle do not collapse into one label dimension.

Closing a work PR without merge, reopening it, or opening a replacement PR
does not write issue labels: live open PR state determines whether the issue is
active. A merged managed promotion consumes its exact sealed delivery plan,
closes each still-open delivered issue, removes the awaiting-review index, then
appends base-integration evidence and commits the promotion cursor last. PR
closing keywords are optional presentation and never the completion authority.
Once the cursor is committed, a duplicate event is a total no-op and therefore
does not undo a later human reopen. New implementation after staging review
uses a follow-up issue; reopening the original remains a terminal human
override rather than a request for Baton to deliver it again.

Recommendation acquisition therefore reads bounded authoritative staged-work
records. If the sealed ledger cannot be acquired completely, `snapshot`
returns `completeness: degraded` with a degraded Recommendation and no Action.
Both that snapshot behavior and the legacy `next` projection block a
recommendation instead of assuming the issue is ready.

Generated transition automation executes a released pinned Baton binary from
trusted base-branch code under `pull_request_target`; it never runs code from
the pull request checkout. Work-transition labeling runs record-before-label in
Delivery Recorder; promotion completion runs in Work Item Transition. Both
share the delivery concurrency group. A general event bus and a separate local lifecycle
ledger were rejected because there is one real transition today and the
repository delivery ledger plus GitHub own the durable facts.
