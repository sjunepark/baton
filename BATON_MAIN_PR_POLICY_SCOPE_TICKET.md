# Ticket: Scope main-branch PR policy to Baton-managed promotion flows

## Summary

Baton `pr-policy` currently treats every PR targeting the configured base branch
as a promotion PR. In repositories that use `agent` as the staging branch and
`main` as the base branch, that means normal direct PRs into `main` fail unless
they come from `agent` and close issues.

That is too broad. The `agent -> main` requirement should apply to
Baton-managed promotion flows, not every human-directed PR into the base branch.

## Evidence

- Creo PR `workflow-document-rewrite -> main` failed the Baton PR policy check
  with:
  - `Promotion PRs into main must come from agent.`
  - `Promotion PRs into main must close promoted issues with Closes #123.`
- The PR was a human-directed direct-to-main workflow rewrite PR, not an
  automated promotion from the shared `agent` branch.
- Baton v0.1.4 still classifies `baseRef == main` as a promotion path before it
  can distinguish direct human PRs from automation promotion PRs.

## Desired Behavior

Baton should distinguish these cases explicitly:

- PRs targeting the staging branch, normally `agent`, are work PRs and must
  follow work-branch and `Refs #123` policy.
- PRs targeting the base branch from the staging branch, normally
  `agent -> main`, are promotion PRs and must close promoted issues.
- PRs targeting the base branch from Baton-managed work branches, normally
  `agent-work/* -> main`, should fail with guidance to target the staging
  branch instead.
- PRs targeting the base branch from ordinary human branches should be allowed
  or skipped by Baton policy, leaving normal CI, review, and branch protection
  to govern them.

## Acceptance Criteria

- `pr-policy` has a first-class way to classify base-branch PRs as promotion,
  direct human PR, or invalid direct agent-work PR.
- The behavior is configurable enough for target repositories to keep strict
  promotion enforcement for automation while allowing direct human PRs.
- Fixture/table tests cover:
  - `agent-work/123-x -> agent` passes when issue policy is satisfied.
  - `agent -> main` requires closing keywords.
  - `feature-x -> main` does not fail promotion-only checks.
  - `agent-work/123-x -> main` fails with staging-branch guidance.
- Config docs and install templates explain the distinction between
  Baton-managed automation flows and ordinary direct PRs.

## Non-Goals

- Do not replace GitHub branch protection or normal repository review rules.
- Do not make direct human PRs eligible for autonomous merge.
- Do not weaken work PR policy for branches that target the staging branch.

## Validation Hints

- Add policy unit tests in `internal/policy/pr_test.go`.
- Add event fixture coverage for `pull_request_target` payloads that target
  both `agent` and `main`.
- Run `go test ./...`.
