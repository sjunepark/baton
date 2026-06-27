# Baton Commands

Use `--format toon` for compact agent-facing reads. Use `--json` for stable
automation contracts and mutating command results.

## Skill Commands

- `$baton`: read-only dashboard/menu.
- `$baton status [repo]`: readiness and setup checks.
- `$baton next [repo]`: show one selected next action without acting.
- `$baton queue [repo]`: show eligible and skipped queue items.
- `$baton todo <todo>`: create one Baton-ready GitHub issue.
- `$baton todos <notes-or-file>`: split notes into Baton-ready GitHub issues.
- `$baton investigate <issue>`: investigate/comment without edits.
- `$baton implement <issue>`: lease, implement one ready issue, and open/update
  a staging PR.
- `$baton follow-up <pr>`: lease an existing PR branch and fix PR follow-up.
- `$baton run [repo]`: select and handle exactly one safe unit.
- `$baton adopt [repo]`: dry-run target-repo setup checks.
- `$baton automate [repo]`: prepare scheduled one-unit automation.

## CLI Commands

- `baton home --format toon`: show local Baton context without failing on
  missing config or auth.
- `baton doctor --format toon`: check local readiness.
- `baton next --format toon`: select one recommended unit.
- `baton queue --format toon`: inspect eligible and skipped issues.
- `baton prs --format toon`: list open staging PRs.
- `baton pr <number> --json`: inspect one PR.
- `baton checks <number> --format toon`: inspect check rollup.
- `baton review-threads <number> --format toon`: inspect resolved/outdated
  review threads, author kinds, and truncation metadata. Add `--full` when the
  bounded body preview is insufficient.
- `baton lease --purpose <purpose> --branch <ref> --repo owner/name --json`:
  lease an existing PR branch.
- `baton lease --purpose <purpose> --base <ref> --new-branch <ref> --repo
  owner/name --json`: create and lease a new work branch.
- `baton release --lease <id> --json`: release a clean lease.
- `baton release --lease <id> --keep-dirty --json`: retain/release dirty state
  only when reporting it is acceptable.
- `baton issue-policy --event "$GITHUB_EVENT_PATH" --apply`: apply issue labels
  and policy comments in GitHub Actions.
- `baton pr-policy --event "$GITHUB_EVENT_PATH"`: check PR policy in GitHub
  Actions.
- `baton migrate-config --dry-run|--apply`: convert legacy
  `.github/agent-issue-policy.yml` into `.github/baton.yml`.
- `baton complete --summary <text> --comment --repo owner/name --issue N|--pr N`:
  record completion metadata and optionally post an explicit GitHub timeline
  comment.

Mutating commands require explicit `--apply`, `--yes`, or a lease/release
operation. Do not infer permission to merge from any Baton output.
