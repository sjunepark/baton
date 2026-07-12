# Defer candidate claims until an automatic dispatcher exists

## Status

Accepted for the current Baton/Coda design.

## Context

Coda's maintained Baton adapter invokes `baton next --json` and `baton queue
--json` only to record and display Baton Progress. Its LiveViews refresh that
display for a person. Coda Jobs and Runs are created through separate explicit
trigger paths; the Runner refreshes Baton after execution as enrichment rather
than using a Baton Candidate to create the Run.

Coda's database-enforced one-active-Run-per-Job rule prevents duplicate
execution of that Job. It does not claim a GitHub issue, PR, or branch Candidate
across Jobs, Coda instances, or standalone Baton automations.

## Decision

Baton will not expose candidate claim, renew, release, expiry, or completion
commands yet. Current dispatch is human-selected or caller-serialized. A
repository must have at most one unattended dispatcher unless the caller owns
stronger serialization; a Baton Recommendation is advice, not a reservation.

If a maintained caller later implements automatic Recommendation-to-Run
dispatch, claim design becomes a prerequisite. That work must first verify an
official GitHub primitive with the necessary conditional-mutation semantics.
Repository/candidate identity, snapshot revisions, holder correlation, expiry,
renewal, release, loss visibility, and idempotent completion would then be one
public coordination contract shared by standalone Baton and Coda. A
best-effort label must not be described as atomic compare-and-set.

## Consequences

- No speculative claim schema, command family, label, timer, or background
  reconciler is added.
- Coda Run ownership remains execution protection only.
- Multiple independent automatic dispatchers for one repository are currently
  unsupported; use a singleton dispatcher.
- The decision must be revisited before automatic dispatch ships.
