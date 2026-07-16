# Baton v0.7 user flows

## Enroll an existing issue

Inspect the issue, choose the least-permissive mode and suitable priority,
preview `enroll --dry-run`, then apply the same command. The issue body and
project labels remain unchanged.

If the new Task needs a blocker, first enroll without a mode so it remains
blocked, then use one prefix-safe `update` to set mode/priority and add the
blocker. Never expose a known-blocked issue as briefly ready.

## Create a Task

Create an ordinary project issue with a clear outcome and acceptance criteria.
Then explicitly enroll its issue number. An issue form is optional and does
not enroll by itself.

## Select work

Use `list` for the queue and `next` for exactly one deterministic ready Task.
A blocked or unenrolled issue is never returned as a fallback.

## Work and finish

Use `start` as an advisory signal, follow the project's own development
workflow, and use `stop` when activity is abandoned or paused. Close only on
explicit intent. Project events never close a Task implicitly.

## Correct classification

Use `update` to replace mode or priority and add/remove blockers. Use
`unenroll` to remove only Baton's enrollment/activity labels while preserving
project content and labels.
