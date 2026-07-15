# AGENTS.md

## Current State

- This repository contains the implemented Baton Go CLI, embedded install
  templates, tests, and bundled Codex skill.
- Keep documentation aligned with the implemented CLI and avoid speculative
  product scope.
- Treat the source behavior from Creo as historical reference material, not as
  the current source of truth when Baton code and tests already cover behavior.

## Implementation Defaults

- Implement the CLI in Go unless a specific dependency requires another
  language.
- Keep deterministic logic in the CLI and agent judgment in the bundled skill.
- Prefer GitHub GraphQL or REST APIs through a typed internal client over
  scraping `gh` output. Shell out to `gh` only when it materially reduces auth
  or platform complexity.
- Treat repository mutation as unsafe unless the caller has provided an
  isolated checkout for that work.
- Do not add worktree leasing or cleanup back into Baton; the invoking
  environment or user owns checkout lifecycle.
- Keep commands JSON-first for automation; human text output can wrap the same
  internal result objects.

## Safety Gates

- Never mutate a user's primary checkout for automation work.
- Never merge PRs unless the user explicitly asks and the target repo policy
  allows it.
- Never delete, reset, or prune caller-owned worktrees from Baton.
- GitHub Actions policy commands must run trusted Baton code, not PR-modified
  repository code.
- Preserve target repository config as the source of policy truth. Defaults are
  only bootstrap behavior.

## Release Management

- Release Please owns `CHANGELOG.md`, `.release-please-manifest.json`, release
  PRs, `vX.Y.Z` tags, GitHub releases, and marked pinned install-target bumps.
- Use Conventional Commits intentionally: `fix:` for bug fixes, `feat:` for
  release-worthy additions, `docs:` for docs-only non-release changes, and
  `feat!:` or `BREAKING CHANGE:` for breaking public behavior.
- Public SemVer surface includes CLI flags, JSON output, exit codes, config
  shape, install templates, generated workflows, and the module path.
- Review every Release Please PR before merge; do not merge solely because it
  is generated.
- Manual tags or GitHub releases are an emergency fallback only after explicit
  confirmation of the exact version.

## Source References

The initial implementation extracted behavior from `/Users/sejunpark/IT/creo`:

- `.github/ISSUE_WORKFLOW.md`
- `.github/agent-issue-policy.yml`
- `.github/labels.yml`
- `.github/ISSUE_TEMPLATE/agent-work.yml`
- `.github/workflows/issue-policy.yml`
- `.github/workflows/pr-policy.yml`
- `scripts/github/apply-issue-policy.mjs`
- `scripts/github/check-pr-policy.mjs`
- `scripts/github/sync-labels.mjs`
- `scripts/github/ensure-agent-branch.mjs`
- `tests/scripts/github-*.test.ts`

Use those files as behavior references only when checking migration parity or
investigating a behavior gap.

## Validation Expectations

- Add table-driven unit tests for policy parsing and decisions.
- Add tests for GitHub event fixtures.
- Add dry-run tests for branch plans.
- Add integration tests only behind explicit env gates for live GitHub calls.
- Every command that mutates GitHub or git state must have a dry-run path or a
  pure planner that can be tested without side effects.
