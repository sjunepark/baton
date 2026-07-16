# Baton language

**Task**: A GitHub issue explicitly enrolled with `baton:managed`.

**Enrollment**: The reversible presence of `baton:managed`. No body shape,
comment, template, or other label substitutes for it.

**Mode**: The permitted kind of work: `trivial`, `bounded`, or `investigate`.
Exactly one mode label makes an open Task classifiable.

**Priority**: `p0` through `p3`. An omitted priority is treated as `p2`;
conflicting priority labels block selection.

**Blocker**: `needs-info` or `needs:discussion`, or an invalid/conflicting
classification that prevents a Task from being ready.

**Activity**: The advisory `baton:in-progress` label. It does not claim a Task,
create a session, or guarantee exclusivity.

**Done**: The GitHub issue is closed. Baton closes only on explicit `close`
intent and never infers Done from project implementation or delivery events.

**Project label**: Any non-Baton label. Baton preserves it and reports it
separately from fixed Task classification.
