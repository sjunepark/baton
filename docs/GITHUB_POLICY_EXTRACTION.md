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

Historical Creo behavior at extraction time:

- Parse issue form sections from Markdown headings.
- Detect whether an issue matches the configured form fingerprint.
- Map form values to work-kind and agent-mode labels.
- Add `needs-info` when ready implementation modes are missing required
  sections.
- Remove stale labels only from controlled label groups.
- Post or update one policy comment when required fields are missing.

Reusable Baton behavior:

- Same policy engine, config-driven.
- Marker string configurable.
- Controlled label groups required.
- No repo-specific label names hardcoded except defaults.

### PR Policy

Historical Creo behavior at extraction time:

- Work PRs target `agent`.
- Work PR heads start with `agent-work/`.
- Work PRs reference issues with `Refs #123`.
- Work PRs must not use closing keywords.
- Referenced issues had to have implementation labels.
- Referenced issues could not have skip labels.
- Multi-issue all-trivial PRs were rejected.
- Promotion PRs target `main` from `agent`.
- Promotion closing keywords are optional presentation and, when present, must
  exactly match the sealed delivery plan. Explicit post-merge transition owns
  issue closure.
- Direct PRs to `main` from ordinary branches were outside Creo's automation
  policy; `agent-work/*` branches had to target `agent` first.
- Noisy commit subjects are rejected.
- GitHub commit listing cap fails closed.

Current Baton direction:

- Branch names and prefixes configurable.
- Durable managed-issue ownership, rather than current implementation, skip,
  or trivial labels, gates referenced resources in work-PR policy.
- Label sets remain configurable for issue intake and recommendation.
- Keyword rules remain configurable where useful.
- Commit cap behavior remains fail-closed by default.
- Promotion selection comes from the sealed delivery plan, not mutable merged
  PR prose or ancestry. Bootstrap shadow comparisons gate cutover, and active
  recommendation paths never scan all closed staging PRs.

### Branch Setup

Historical Creo behavior at extraction time:

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

Historical Creo behavior at extraction time:

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
.github/workflows/work-item-transition.yml
.github/workflows/delivery-recorder.yml
```

The workflow files should call Baton, not copied scripts.

## Historical Creo Migration Strategy

This section records the original parity migration. Current adopters should use
[the v0.6.0 adopter update](adopter-updates/v0.6.0.md) and the live doctor gate;
the Creo scripts are not current Baton policy authority.

Completed compatibility path:

- Baton can read `.github/agent-issue-policy.yml` directly.
- Creo can run `baton issue-policy` and `baton pr-policy` without renaming
  config.
- Run `baton migrate-config` to produce
  `.github/baton.yml`.
- Keep the old file until automation has run successfully at least once, then
  remove repo-local policy scripts and tests after Go parity is proven.

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
7. Update Codex automation prompts to use Baton next-action output and
   caller-provided isolated checkouts.

## Template Design Requirements

- Templates must be readable after installation.
- Generated comments should state they are managed by Baton but editable.
- `ISSUE_WORKFLOW.md` should document policy in target-repo terms, not Baton
  internals.
- `baton init` should not overwrite user-edited files without showing a diff or
  requiring explicit confirmation.
