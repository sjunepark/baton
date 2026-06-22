# AGENTS.md

## Current State

- This repository is in planning/spec mode.
- Do not add a Go module, executable code, GitHub workflows, or generated
  templates until explicitly asked to begin implementation.
- Keep documentation implementation-ready and avoid speculative product scope.

## Implementation Defaults

- Implement the CLI in Go unless a specific dependency requires another
  language.
- Keep deterministic logic in the CLI and agent judgment in the bundled skill.
- Prefer GitHub GraphQL or REST APIs through a typed internal client over
  scraping `gh` output. Shell out to `gh` only when it materially reduces auth
  or platform complexity.
- Treat repository mutation as unsafe unless it happens inside a Baton worktree
  lease.
- Keep commands JSON-first for automation; human text output can wrap the same
  internal result objects.

## Safety Gates

- Never mutate a user's primary checkout for automation work.
- Never merge PRs unless the user explicitly asks and the target repo policy
  allows it.
- Never delete, reset, or prune worktrees unless Baton can prove the candidate
  is managed, idle, clean, and safe.
- GitHub Actions policy commands must run trusted Baton code, not PR-modified
  repository code.
- Preserve target repository config as the source of policy truth. Defaults are
  only bootstrap behavior.

## Source References

The first implementation should extract behavior from
`/Users/sejunpark/IT/creo`:

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

Use those files as behavior references, not as long-term source layout.

## Validation Expectations

Once implementation begins:

- Add table-driven unit tests for policy parsing and decisions.
- Add tests for GitHub event fixtures.
- Add dry-run tests for branch/worktree plans.
- Add integration tests only behind explicit env gates for live GitHub calls.
- Every command that mutates GitHub or git state must have a dry-run path or a
  pure planner that can be tested without side effects.

