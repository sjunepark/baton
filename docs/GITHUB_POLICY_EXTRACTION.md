# GitHub Policy Extraction

## Source Repository

Initial behavior comes from:

```text
/Users/sejunpark/IT/creo
```

Source files:

```text
.github/ISSUE_WORKFLOW.md
.github/agent-issue-policy.yml
.github/labels.yml
.github/ISSUE_TEMPLATE/agent-work.yml
.github/workflows/issue-policy.yml
.github/workflows/pr-policy.yml
scripts/github/apply-issue-policy.mjs
scripts/github/check-pr-policy.mjs
scripts/github/sync-labels.mjs
scripts/github/ensure-agent-branch.mjs
tests/scripts/github-agent-branch.test.ts
tests/scripts/github-issue-policy.test.ts
tests/scripts/github-pr-policy.test.ts
```

## Extracted Concepts

### Issue Policy

Current behavior:

- Parse issue form sections from Markdown headings.
- Detect whether an issue matches the configured form fingerprint.
- Map form values to work-kind and agent-mode labels.
- Add `agent:blocked` when ready implementation modes are missing required
  sections.
- Remove stale labels only from controlled label groups.
- Post or update one policy comment when required fields are missing.

Reusable Baton behavior:

- Same policy engine, config-driven.
- Marker string configurable.
- Controlled label groups required.
- No repo-specific label names hardcoded except defaults.

### PR Policy

Current behavior:

- Work PRs target `agent`.
- Work PR heads start with `agent-work/`.
- Work PRs reference issues with `Refs #123`.
- Work PRs must not use closing keywords.
- Referenced issues must have implementation labels.
- Referenced issues must not have skip labels.
- Multi-issue all-trivial PRs are rejected.
- Promotion PRs target `main` from `agent`.
- Promotion PRs use closing keywords.
- Noisy commit subjects are rejected.
- GitHub commit listing cap fails closed.

Reusable Baton behavior:

- Branch names and prefixes configurable.
- Label sets configurable.
- Keyword rules configurable where useful, but defaults should preserve the
  Creo model.
- Commit cap behavior remains fail-closed by default.

### Branch Setup

Current behavior:

- Inspect `origin/main`, `origin/agent`, and local `agent`.
- Create/publish `agent` only when it exactly matches `origin/main`.
- Create a tracking local branch from existing `origin/agent`.
- Set upstream when local and remote match.
- Refuse force reset or force push.

Reusable Baton behavior:

- Generalize `main` and `agent` names through config.
- Keep non-destructive default.
- Keep pure planner for tests.

### Label Sync

Current behavior:

- Read `.github/labels.yml`.
- Create missing labels.
- Update color/description drift.

Reusable Baton behavior:

- Support dry-run first.
- Keep manifest format stable.
- Report labels not managed by Baton without deleting them.

## Target Installed Files

For a consuming repository, `baton init --apply` should be able to install:

```text
.github/baton.yml
.github/labels.yml
.github/ISSUE_WORKFLOW.md
.github/ISSUE_TEMPLATE/agent-work.yml
.github/workflows/issue-policy.yml
.github/workflows/pr-policy.yml
```

The workflow files should call Baton, not copied scripts.

## Compatibility Strategy

Phase 1:

- Baton can read `.github/agent-issue-policy.yml` directly.
- Creo can switch workflows to `baton issue-policy` and `baton pr-policy`
  without renaming config.

Phase 2:

- Run `baton migrate-config` or `baton init --migrate` to produce
  `.github/baton.yml`.
- Keep the old file until automation has run successfully at least once.

Phase 3:

- Remove repo-local JS scripts and tests after Go parity exists.

## Creo Migration Checklist

1. Implement Baton policy parity tests from Creo fixtures.
2. Install Baton binary locally.
3. In Creo, run:
   - `baton doctor`
   - `baton issue-policy --body-file <fixture> --json`
   - `baton pr-policy --event <fixture> --json`
4. Update Creo GitHub workflows to call Baton.
5. Keep old scripts for one trial period.
6. Remove old scripts after policy checks pass in CI.
7. Update Codex automation prompts to use Baton next-action and leases.

## Template Design Requirements

- Templates must be readable after installation.
- Generated comments should state they are managed by Baton but editable.
- `ISSUE_WORKFLOW.md` should document policy in target-repo terms, not Baton
  internals.
- `baton init` should not overwrite user-edited files without showing a diff or
  requiring explicit confirmation.

