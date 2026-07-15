# Baton User Flows

Baton has two surfaces:

- Skill commands such as `$baton run` express the workflow and leave only true
  arguments for the user.
- CLI commands provide deterministic repository, policy, queue, branch, and
  work-item facts for automation and scripts.

The Go CLI does not provide `baton todo` or `baton todos`; those are skill
workflows that prepare issue bodies, preflight them with `baton issue-policy`,
and create GitHub issues with `gh issue create`.

## Create One Todo

Use when a human has one work item that should enter the Baton-managed GitHub
issue queue.

- Required input: the todo text and target repository if it cannot be inferred.
- Skill command: `$baton todo <todo>`.
- CLI equivalent:

  ```sh
  baton issue-policy --body-file issue.md --json
  gh issue create --repo owner/name --title "..." --body-file issue.md
  ```

- Expected output: one issue number, title, selected Agent mode, and why that
  mode was chosen.
- Safety boundaries: creates an issue only; no branch, commit, PR, or
  merge.
- Common stop conditions: missing repository, missing GitHub auth, or a todo so
  vague that even an investigation/discussion issue would be misleading.

## Create Many Todos From Notes

Use when notes contain multiple unrelated outcomes that should become separate
Baton-ready GitHub issues.

- Required input: pasted notes or a file path containing notes.
- Skill command: `$baton todos <notes-or-file>`.
- CLI equivalent: repeat the single-todo issue body, preflight, and
  `gh issue create` flow once per split issue.
- Expected output: every created issue, selected Agent mode, and split/merge
  decisions.
- Safety boundaries: creates issues only; does not implement.
- Common stop conditions: notes cannot be read, repository is ambiguous, or
  issue creation would misrepresent the requested work.

## Check Readiness And Status

Use before adopting Baton in a repository or when automation readiness is
uncertain.

- Required input: optional target repository.
- Skill command: `$baton status [repo]`.
- CLI equivalent:

  ```sh
  baton doctor --repo owner/name --format toon
  baton sync-labels --dry-run --repo owner/name --json
  baton ensure-branch --json
  ```

- Expected output: a live `ready`, `degraded`, or `blocked` compatibility result
  with per-check remediation. Token discovery alone is not success: doctor
  reads the selected repository and its workflows, rules, labels, Actions
  policy, ownership records, and delivery ledger.
- Organization repositories may be degraded when GitHub does not expose the
  standard-hosted-runner disablement setting through its supported REST API;
  confirm that setting explicitly instead of treating unknown as enabled.
- Safety boundaries: read-only or dry-run; no setup is applied.
- Common stop conditions: missing auth, missing local config, or any blocked
  live compatibility check. Local tracking-ref drift is diagnostic only; live
  GitHub facts decide adoption compatibility.

## Inspect Queue And Next Action

Use when deciding what Baton would do without letting it act.

- Required input: optional target repository.
- Skill commands: `$baton queue [repo]` and `$baton next [repo]`.
- CLI equivalent:

  ```sh
  baton queue --format toon --repo owner/name
  baton next --format toon --repo owner/name
  ```

- Expected output: eligible and skipped issues/PRs, next candidate set, reason,
  and whether candidates are read-only, investigation, implementation, or PR
  follow-up.
- Safety boundaries: read-only; no edits, comments, pushes, or merges.
- Common stop conditions: no eligible work, blocked queue, or missing GitHub
  access.

## Investigate An Issue

Use when an issue is explicitly scoped for research, diagnosis, or a written
recommendation.

- Required input: issue number or URL.
- Skill command: `$baton investigate <issue>`.
- CLI equivalent:

  ```sh
  baton queue --format toon --repo owner/name
  baton next --format toon --repo owner/name
  gh issue view <number> --repo owner/name
  gh issue comment <number> --repo owner/name --body-file findings.md
  ```

- Expected output: a GitHub issue comment with findings, evidence, and a
  recommended next label or decision.
- Safety boundaries: no file edits unless the user explicitly changes scope.
- Common stop conditions: issue is not investigation-scoped, findings require a
  product/security/schema decision, or auth prevents commenting.

## Implement An Issue

Use when one ready issue has clear acceptance criteria and no skip label.

- Required input: issue number or a repository where `snapshot` reports an
  `actionable` eligible implementation item.
- Skill command: `$baton implement <issue>` or `$baton run [repo]`.
- CLI equivalent:

  ```sh
  baton snapshot --format toon --repo owner/name
  # continue only when outcome is actionable
  # in a caller-provided isolated checkout:
  # substitute repository.work_branch_prefix, repository.default_remote,
  # and repository.staging_branch from .github/baton.yml:
  git switch -c <work_branch_prefix>123-short-slug <default_remote>/<staging_branch>
  # read AGENTS.md, implement, validate, push, open/update PR, and report
  # the summary plus validation evidence to the caller
  ```

- Expected output: PR URL or update summary and validation result returned to
  the caller.
- Safety boundaries: edits only inside the caller-provided isolated checkout;
  PR targets the staging branch and references the issue with `Refs #<issue>`.
- Common stop conditions: missing isolated checkout, ambiguous acceptance
  criteria, unrelated dirty state, or a required human decision.

## Follow Up On A PR

Use when an existing Baton work PR has failing checks or unresolved review
feedback.

- Required input: PR number or URL.
- Skill command: `$baton follow-up <pr>`.
- CLI equivalent:

  ```sh
  baton pr <number> --json --repo owner/name
  baton checks <number> --format toon --repo owner/name
  baton review-threads <number> --format toon --repo owner/name
  # in a caller-provided isolated checkout:
  git switch <head-ref>
  # fix only the PR follow-up, validate, push to the same branch, and report
  # the summary plus validation evidence to the caller
  ```

- Expected output: pushed fixes or a no-action report plus validation evidence.
- Safety boundaries: no replacement PR and no merge; push only to the existing
  PR branch from the isolated checkout.
- Common stop conditions: review feedback needs human judgment, checks are red
  for unrelated reasons, or the PR branch cannot be checked out safely.

## Adopt Baton In A Target Repo

Use when preparing a repository for Baton-managed issue and PR automation.

- Required input: target repository.
- Skill command: `$baton adopt [repo]`.
- CLI equivalent:

  ```sh
  baton doctor --repo owner/name --format toon
  baton init --dry-run --json
  baton migrate-config --dry-run
  baton sync-labels --dry-run --repo owner/name --json
  baton ensure-branch --json
  ```

- Expected output: setup plan, migration preview when applicable, label diff,
  branch readiness, live compatibility blockers, and exact apply commands for
  a human to approve. After approved changes, rerun doctor; adoption is not
  complete while `readyState` is `blocked`, and any `degraded` capability must
  be named in the handoff.
- Safety boundaries: dry-run/read-only by default; do not apply setup or resolve
  branch divergence without explicit approval.
- Common stop conditions: unreviewed install diff, legacy config ambiguity,
  label policy mismatch, unsafe local staging-branch state, or any blocked
  doctor check.

## Set Up Scheduled Automation

Use after at least one manual Baton run has selected the right repository and
respected checkout isolation boundaries.

- Required input: repository, cadence, automation scope, and whether the first
  automation should be read-only or implementation-capable.
- Skill command: `$baton automate [repo]`.
- CLI equivalent: run read-only prerequisite checks first, then schedule a
  one-unit worker prompt:

  ```sh
  baton home --format toon
  baton doctor --repo owner/name --format toon
  baton ensure-branch --json
  baton sync-labels --dry-run --repo owner/name --json
  baton snapshot --format toon --repo owner/name
  # schedule implementation only when outcome is actionable
  ```

- Expected output: automation prompt, prerequisites status, recommended cadence,
  and first-run review checklist.
- Safety boundaries: start with read-only automation when uncertain; scheduled
  implementation still must handle one unit, run in an isolated checkout before
  edits, and never merge.
- Common stop conditions: manual run has not succeeded, auth is missing,
  isolated execution is unavailable, or the target repo is not Baton-ready.
