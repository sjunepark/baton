# Baton Commands

Use `--json` for automation-facing reads.

- `baton next --json`: select one recommended unit.
- `baton queue --json`: inspect eligible and skipped issues.
- `baton prs --json`: list open staging PRs.
- `baton pr <number> --json`: inspect one PR.
- `baton checks <number> --json`: inspect check rollup.
- `baton review-threads <number> --json`: inspect resolved/outdated review
  threads and author kinds.
- `baton lease --purpose <purpose> --branch <ref> --json`: lease an existing
  PR branch.
- `baton lease --purpose <purpose> --base <ref> --new-branch <ref> --json`:
  create and lease a new work branch.
- `baton release --lease <id> --json`: release a clean lease.
- `baton release --lease <id> --keep-dirty --json`: retain/release dirty state
  only when reporting it is acceptable.
- `baton issue-policy --event "$GITHUB_EVENT_PATH" --apply`: apply issue labels
  and policy comments in GitHub Actions.
- `baton pr-policy --event "$GITHUB_EVENT_PATH"`: check PR policy in GitHub
  Actions.

Mutating commands require explicit `--apply`, `--yes`, or a lease/release
operation. Do not infer permission to merge from any Baton output.
