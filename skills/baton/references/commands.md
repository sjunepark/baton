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
  implement one ready issue and open/update a staging PR.
- `$baton follow-up <pr>`: in a caller-provided isolated checkout, fix PR
  follow-up on the existing branch.
- `$baton run [repo]`: choose and handle exactly one safe candidate.
- `$baton adopt [repo]`: dry-run target-repo setup checks.
- `$baton update [repo]`: check and update an existing Baton adoption through a
  normal reviewed PR. Do not merge.
- `$baton automate [repo]`: prepare scheduled one-unit automation.

## CLI Commands

- `baton home --format toon`: show local Baton context without failing on
  missing config or auth.
- `baton doctor --format toon`: check local readiness.
- `baton next --format toon`: return the highest-priority candidate set.
- `baton next --action issue-investigation --format toon`: inspect
  investigation candidates when a human intentionally wants that lower-priority
  action.
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
- `baton migrate-config --dry-run|--apply`: convert legacy
  `.github/agent-issue-policy.yml` into `.github/baton.yml`.
- `baton complete --summary <text> --comment --repo owner/name --issue N|--pr N`:
  record completion metadata and optionally post an explicit GitHub timeline
  comment.

Mutating commands require explicit `--apply`, `--yes`, or user-provided
execution context plus explicit user/workflow intent. Do not infer permission
to merge from any Baton output.
