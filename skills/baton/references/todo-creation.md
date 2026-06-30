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
- Prefer stable problem, outcome, and evidence descriptions over volatile code
  coordinates. Avoid making exact line numbers, private helper names, or
  speculative implementation steps part of the required work because the code
  can drift before the todo is picked up.
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

When source locations help, cite durable anchors such as file paths, symbols,
tests, commands, issue links, or permalinks tied to a commit. Put volatile
details in Context / evidence as clues, not in Summary or Acceptance criteria
as requirements the future agent must follow.

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

## Skill Commands

- `$baton todo <todo>`: create one Baton-ready GitHub issue from the todo.
- `$baton todos <notes-or-file>`: split notes into Baton-ready GitHub issues,
  merging duplicate notes that describe the same outcome.

For both commands, create issue bodies with the API body template above,
preflight each body with `baton issue-policy --body-file <tmp-file> --json`,
then create issues with `gh issue create --repo <repo> --title ... --body-file
<tmp-file>`.

If a note lacks enough information for implementation, create it as
Investigate only or Needs discussion instead of Ready bounded. Ask a concise
clarification only when creating even an investigation/discussion issue would be
misleading.

## Reporting Format

When finished, report:

- issue number and title;
- Agent mode;
- why that mode was chosen;
- any issues intentionally marked Investigate only or Needs discussion because
  implementation was not ready.
