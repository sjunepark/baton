---
name: baton
description: "Baton Task management. Use when the user mentions Baton, asks to create or enroll issues for Baton, or asks to inspect, select, classify, start, stop, close, or implement a Baton-enrolled GitHub issue."
---

# Baton

## CLI authority

Before constructing a Baton command, run `baton COMMAND --help` and derive its
behavior, syntax, flags, and accepted values from that output. When the request
does not identify a command, run `baton --help` first. The live CLI is the
single source of truth for deterministic Task facts and transitions.

Use agent judgment for issue content and classification and user intent for
authorization. Preserve project-owned issue content and labels. Follow the
target project's instructions and tools for implementation and delivery.

## Workflows

### Inspect and select

- For `$baton`, consult the live `list` help, run that command, and summarize
  the current Tasks.
- For a requested read, consult the matching command help and invoke that
  command. Request the complete issue body only when the task requires it.
- Treat a definitive empty CLI result as the answer.

Complete the branch when the requested Task facts or definitive empty result
have been reported.

### Create todos

For `$baton todo <request>` or `$baton todos <notes-or-file>`, read both
[`references/todo-creation.md`](references/todo-creation.md) and
[`references/classification.md`](references/classification.md). Create one
ordinary GitHub issue per independent outcome, classify it, then explicitly
enroll it. Consult the live `enroll` and, when needed, `update` help; preview
each mutation before applying it. Follow the classification reference's safe
sequence for a known blocker. The todo request authorizes creation and
enrollment; implementation requires an additional request.

Complete the branch when every created issue is reported with its number and
classification as either enrolled or explicitly pending a named decision or
blocker.

### Enroll or classify an existing issue

Read [`references/classification.md`](references/classification.md) before
choosing or suggesting classification.

1. Inspect the issue and its labels while preserving its body.
2. Choose or ask for the mode, priority, and blockers.
3. For an unblocked issue, consult the live `enroll` help, preview the
   transition, and apply it only when the request authorizes enrollment.
4. For a blocked issue, follow the reference's blocked-enrollment sequence.
5. Use the live `update` help for later classification changes. Treat
   unenrollment as a separate explicit intent.

Complete the branch when the CLI confirms the requested state or the issue is
reported as unenrolled with the exact pending decision or blocker.

### Implement one Task

For `$baton implement ISSUE`:

1. Consult the live `show` help, inspect the Task, and confirm it is open,
   enrolled, unblocked, and permits the requested work.
2. Consult the live `start` help, start the Task, and confirm the transition.
3. Follow the target project's instructions through its required validation.
4. With interaction available, ask for explicit permission before consulting
   the live `close` help and closing the Task. Without interaction, leave it
   open and report the CLI-derived close command.
5. For explicitly paused or abandoned work, consult the live `stop` help and
   clear advisory activity.

Complete the branch when project work and validation are reported together
with the final confirmed Task state or the explicit remaining close action.
