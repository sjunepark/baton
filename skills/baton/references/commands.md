# Baton Commands

Use `--format toon` for compact agent-facing reads. Use `--json` for stable
automation contracts and mutating command results.

## Skill Commands

- `$baton`: read-only dashboard/menu.
- `$baton status [repo]`: readiness and setup checks.
- `$baton next [repo]`: show the next candidate set without acting.
- `$baton queue [repo]`: show eligible and skipped queue items.
- `$baton todo <todo>`: create one Baton-ready GitHub issue.
- `$baton todos <notes-or-file>`: split notes into Baton-ready GitHub issues.
- `$baton investigate <issue>`: investigate/comment without edits.
- `$baton implement <issue>`: in a caller-provided isolated checkout,
  implement only when `baton snapshot` selects that exact issue as an
  actionable `issue_implementation` candidate, then open/update a staging PR.
- `$baton follow-up <pr>`: in a caller-provided isolated checkout, fix PR
  follow-up on the existing branch.
- `$baton run [repo]`: choose and handle exactly one safe candidate.
- `$baton adopt [repo]`: dry-run target-repo setup checks; blocked doctor
  results prevent adoption completion.
- `$baton update [repo]`: check and update an existing Baton adoption through a
  normal reviewed PR. Do not merge.
- `$baton automate [repo]`: prepare scheduled one-unit automation.

## CLI Commands

- `baton home --format toon`: show local Baton context without failing on
  missing config or auth.
- `baton doctor --repo owner/name --format toon`: check local and live adoption
  readiness, including workflow trust, delivery/ownership evidence, Actions
  policy, required checks, labels, merge methods, and merge queues.
  Repeat the reviewed init `--go-install` or `--install-command` option when an
  adopter intentionally customized its workflow installer.
- `baton next --format toon`: return the highest-priority candidate set.
- `baton next --action issue-investigation --format toon`: inspect
  investigation candidates when a human intentionally wants that lower-priority
  action.
- `baton snapshot --format toon`: preferred unattended-work observation. Act
  only when `recommendation.outcome` is `actionable`; otherwise report and stop.
- `baton queue --format toon`: inspect eligible and skipped issues.
- `baton prs --format toon`: list open staging PRs.
- `baton pr <number> --json`: inspect one PR.
- `baton checks <number> --format toon`: inspect check rollup.
- `baton review-threads <number> --format toon`: inspect resolved/outdated
  review threads, author kinds, and truncation metadata. Add `--full` when the
  bounded body preview is insufficient.
- `baton issue-policy --event "$GITHUB_EVENT_PATH" --apply`: apply issue labels
  and policy comments in GitHub Actions.
- `baton pr-policy --event "$GITHUB_EVENT_PATH"`: check PR policy in GitHub
  Actions.
- `baton pr-transition --event "$GITHUB_EVENT_PATH" --dry-run|--apply --json`:
  plan or apply idempotent merged-work awaiting-review and merged-promotion
  completion transitions. Promotion apply closes sealed-plan issues, removes
  the awaiting-review index, records base integration, and commits the cursor
  last. The generated privileged workflow owns routine apply, checks out
  trusted base-repository code, and never executes PR-head code.
- `baton delivery-record --event "$GITHUB_EVENT_PATH" --dry-run|--apply --json`:
  preview or apply a staged-work or exact base-to-staging synchronization
  record. Sync PRs remain unmanaged by PR policy; this trusted post-merge path
  verifies ancestry and records evidence. Omit `--event` only for manual
  missed-event reconciliation.
- `baton delivery-bootstrap --dry-run|--apply --plan-id <id> --json`: review
  and apply delivery migration. Invoke it through the generated Delivery
  Recorder bootstrap modes so writes are trusted and serialized. Initialization
  and historical migration must follow `delivery-bootstrap.md`; when a locator
  is already pinned, initialization is a drained-ledger rollover into a new
  locked issue. Unresolved ambiguity is a stop.
- `baton migrate-config --dry-run|--apply`: convert legacy
  `.github/agent-issue-policy.yml` into `.github/baton.yml`.
Mutating commands require explicit `--apply`, `--yes`, or user-provided
execution context plus explicit user/workflow intent. Do not infer permission
to merge from any Baton output.
