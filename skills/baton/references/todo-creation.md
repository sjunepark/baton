# Baton Todo Creation

Use this reference when asked to create todos, convert notes into GitHub issues,
or prepare work for future Baton-managed agents.

## Rules

- Use GitHub issues as the Baton todo queue.
- Use the repository's Agent-readable work item issue template.
- If creating issues through an API instead of the GitHub form UI, write the
  issue body with the same `###` headings from the template so Baton can parse
  it.
- Split unrelated work into separate issues.
- Merge duplicate notes into one issue when they describe the same outcome.
- Choose the least-permissive Agent mode that fits.
- Use `Ready trivial` only for tiny, obvious fixes.
- Use `Ready bounded` only when scope, context, and acceptance criteria are
  clear enough for implementation.
- Use `Investigate only` when the next useful step is research, diagnosis, or a
  written recommendation.
- Use `Needs discussion` when a human product, security, schema, release, or
  compatibility decision is needed.
- Do not create branches, commits, PRs, or implementation plans when only asked
  to create todos.

## Required Issue Fields

Fill these fields with concrete, agent-usable detail:

- Work kind: Bug, Documentation, Enhancement, or Question.
- Agent mode: Ready trivial, Ready bounded, Investigate only, or Needs
  discussion.
- Summary: the requested outcome in one or two short paragraphs.
- Context / evidence: links, logs, screenshots, files, commands, observed
  behavior, or exact examples.
- Acceptance criteria: concrete bullets or checkboxes.

Use optional fields when they materially reduce ambiguity:

- Non-goals / constraints: boundaries, compatibility limits, risky areas, or
  things that must not change.
- Validation hints: relevant tests, commands, manual checks, or expected
  observable results.
- Notes: extra context that does not belong in the required fields.

Let Baton issue-policy derive controlled labels from these fields when the
workflow is installed. Do not rely on free-form issue labels as the source of
truth for Agent mode.

## API Body Template

Use this Markdown body shape when a GitHub API or issue-creation tool cannot
submit the issue through the interactive issue form:

```md
### Work kind

Bug

### Agent mode

Ready bounded

### Summary

<requested outcome>

### Context / evidence

<links, logs, files, observed behavior, screenshots, commands, or examples>

### Acceptance criteria

- [ ] <observable result>
- [ ] <observable result>

### Non-goals / constraints

<optional boundaries, compatibility limits, risky areas, or "N/A">

### Validation hints

<optional tests, commands, or manual checks>

### Notes

<optional extra context>
```

## Default Prompt

```text
Create Baton-ready GitHub issues for the work below.

For each todo:
1. Create one GitHub issue using the repository's Agent-readable work item issue template.
2. If creating through an API, use the issue-form-compatible Markdown body with `###` headings.
3. Choose the least-permissive Agent mode that fits.
4. Fill Work kind, Agent mode, Summary, Context / evidence, and Acceptance criteria.
5. Add Non-goals / constraints and Validation hints when useful.
6. Split unrelated work into separate issues.
7. Merge duplicate notes that describe the same outcome.
8. Use Investigate only or Needs discussion if implementation scope is unclear.
9. Do not implement, branch, open a PR, or merge.

After creating issues, report the issue numbers, chosen Agent mode, and why each
mode was chosen.

Todos:
<paste todos here>
```

## Single-Todo Prompt

```text
Create one Baton-ready GitHub issue for this todo:

<todo>

Use the repository's Agent-readable work item issue template. Make the issue
actionable for a future agent by including clear context/evidence, acceptance
criteria, non-goals, and validation hints. Choose the least-permissive Agent
mode that still fits. Do not implement the work.
```

## Notes-To-Issues Prompt

```text
Turn these notes into Baton-ready GitHub issues.

Split unrelated work into separate issues. Merge duplicate notes. If a note
lacks enough information for implementation, create it as Investigate only or
Needs discussion instead of Ready bounded. Each issue should have concrete
acceptance criteria and validation hints where possible.

Notes:
<paste notes here>
```

## Reporting Format

When finished, report:

- issue number and title;
- Agent mode;
- why that mode was chosen;
- any issues intentionally marked Investigate only or Needs discussion because
  implementation was not ready.
