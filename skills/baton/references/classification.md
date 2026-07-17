# Baton classification judgment

Read this reference when creating, enrolling, or reclassifying a Task. Confirm
the accepted values in the relevant live CLI help before constructing a
command.

## Mode

Choose the least-permissive mode that authorizes the requested next action:

- `trivial` for a small, obvious implementation;
- `bounded` for clearly scoped implementation;
- `investigate` for research and reporting only.

## Priority

Use `p2` as the normal priority. Choose another priority only when the user or
project context supplies a clear urgency or ordering reason.

## Blockers

Use `needs-info` when required facts or input are missing. Use
`needs:discussion` when a project decision or agreement must precede action.

Keep a known-blocked issue blocked throughout enrollment:

1. Without an existing fixed mode label, enroll it without a mode so it remains
   blocked, then use one previewed `update` to set classification and add the
   blocker.
2. With an existing fixed mode label, add the blocker through the project's
   approved workflow before enrollment or leave the issue unenrolled.

Report the specific blocker with the result.

Complete classification when one mode, one effective priority, and every
known blocker are accounted for.
