# Baton User Flows

Baton has two surfaces:

- Skill commands such as `$baton run` express the workflow and leave only true
  arguments for the user.
- CLI commands provide deterministic repository, policy, queue, and lease facts
  for automation and scripts.

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
- Safety boundaries: creates an issue only; no branch, lease, commit, PR, or
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
  baton doctor --format toon
  baton sync-labels --dry-run --repo owner/name --json
  baton ensure-branch --json
  ```

- Expected output: setup gaps, auth/config status, and exact safe commands to
  fix missing pieces.
- Safety boundaries: read-only or dry-run; no setup is applied.
- Common stop conditions: missing auth, missing local config, or branch
  divergence that needs a human decision.

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
- Safety boundaries: read-only; no lease, edits, comments, pushes, or merges.
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

- Required input: issue number or a repository where `next` can select one
  eligible implementation item.
- Skill command: `$baton implement <issue>` or `$baton run [repo]`.
- CLI equivalent:

  ```sh
  baton lease --purpose issue-123 --base origin/agent --new-branch agent-work/123-short-slug --repo owner/name --json
  cd <returned-path>
  # read AGENTS.md, implement, validate, push, and open/update PR
  baton complete --summary "..." --lease <id> --validation "..." --json
  baton release --lease <id> --json
  ```

- Expected output: PR URL or update summary, validation result, completion
  record, and lease release status.
- Safety boundaries: edits only inside the returned lease path; PR targets the
  staging branch and references the issue with `Refs #<issue>`.
- Common stop conditions: lease conflict, ambiguous acceptance criteria, dirty
  lease release conflict, or a required human decision.

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
  baton lease --purpose pr-<number> --branch <head-ref> --repo owner/name --json
  cd <returned-path>
  # fix only the PR follow-up, validate, and push to the same branch
  baton complete --summary "..." --lease <id> --validation "..." --json
  baton release --lease <id> --json
  ```

- Expected output: pushed fixes or a no-action report, validation result,
  completion record, and lease release status.
- Safety boundaries: no replacement PR and no merge; push only to the existing
  PR branch after leasing it.
- Common stop conditions: review feedback needs human judgment, checks are red
  for unrelated reasons, or the PR branch cannot be leased.

## Adopt Baton In A Target Repo

Use when preparing a repository for Baton-managed issue and PR automation.

- Required input: target repository.
- Skill command: `$baton adopt [repo]`.
- CLI equivalent:

  ```sh
  baton init --dry-run --json
  baton migrate-config --dry-run
  baton sync-labels --dry-run --repo owner/name --json
  baton ensure-branch --json
  ```

- Expected output: setup plan, migration preview when applicable, label diff,
  branch readiness, and exact apply commands for a human to approve.
- Safety boundaries: dry-run/read-only by default; do not apply setup or resolve
  branch divergence without explicit approval.
- Common stop conditions: unreviewed install diff, legacy config ambiguity,
  label policy mismatch, or branch state that needs a human decision.

## Set Up Scheduled Automation

Use after at least one manual Baton run has selected the right repository and
respected lease boundaries.

- Required input: repository, cadence, automation scope, and whether the first
  automation should be read-only or implementation-capable.
- Skill command: `$baton automate [repo]`.
- CLI equivalent: run read-only prerequisite checks first, then schedule a
  one-unit worker prompt:

  ```sh
  baton home --format toon
  baton doctor --format toon
  baton ensure-branch --json
  baton sync-labels --dry-run --repo owner/name --json
  baton next --format toon --repo owner/name
  ```

- Expected output: automation prompt, prerequisites status, recommended cadence,
  and first-run review checklist.
- Safety boundaries: start with read-only automation when uncertain; scheduled
  implementation still must handle one unit, acquire a lease before edits, and
  never merge.
- Common stop conditions: manual run has not succeeded, auth is missing,
  overlapping runs could lease-conflict, or the target repo is not Baton-ready.
