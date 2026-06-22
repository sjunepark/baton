# Baton

Baton is a Go CLI and companion Codex skill for reusable GitHub issue/PR agent
workflows. It extracts the Creo agent workflow into deterministic commands for
policy checks, queue inspection, safe worktree leasing, and target-repository
installation.

The CLI owns deterministic GitHub, git, policy, and lease state. Codex keeps
the judgment work: deciding implementation shape, handling ambiguous review
feedback, and reporting decisions back to the user.

## Status

Implemented:

- issue and PR policy parity with the Creo reference behavior;
- GitHub workflow, issue template, label, and config installation templates;
- GitHub issue/PR/check/review-thread queue inspection;
- `baton next --json` for one recommended automation action;
- native worktree leasing with release and prune safety gates;
- `doctor`, `complete`, `migrate-config`, `sync-labels`, and `ensure-branch`;
- a bundled Codex skill in `skills/baton`.

Remaining migration work depends on publishing or otherwise configuring a
trusted Baton install path for consuming GitHub Actions workflows. The default
template command is:

```sh
go install github.com/sjunepark/baton/cmd/baton@v0.1.2
```

That path must resolve from GitHub Actions before a consuming repository points
policy workflows at Baton.

## Quick Start

Build or run locally:

```sh
go test ./...
go run ./cmd/baton --help
go run ./cmd/baton doctor --json
```

Preview installation files for a target repository:

```sh
baton init --dry-run --json
```

Apply installation files after reviewing the plan:

```sh
baton init --apply --go-install github.com/sjunepark/baton/cmd/baton@v0.1.2
```

For a pinned release or alternate trusted source, pass a full command:

```sh
baton init --apply --install-command 'go install github.com/sjunepark/baton/cmd/baton@v0.1.0'
```

Inspect a repository queue:

```sh
baton queue --json --repo owner/name
baton prs --json --repo owner/name
baton next --json --repo owner/name
```

Acquire and release an isolated worktree:

```sh
baton lease --purpose issue-123 --base origin/agent --new-branch agent-work/issue-123 --repo owner/name --json
baton release --lease <id> --json
```

## Target Repository Workflows

`baton init` installs:

```text
.github/baton.yml
.github/labels.yml
.github/ISSUE_WORKFLOW.md
.github/ISSUE_TEMPLATE/agent-work.yml
.github/workflows/issue-policy.yml
.github/workflows/pr-policy.yml
```

The generated workflows install trusted Baton code, then run:

```sh
baton issue-policy --event "$GITHUB_EVENT_PATH" --apply
baton pr-policy --event "$GITHUB_EVENT_PATH"
```

`pull_request_target` policy runs check out the trusted base SHA before
installing Baton so PR-modified repository code is not executed.

## Project Map

- [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md): product and safety
  requirements.
- [ARCHITECTURE.md](ARCHITECTURE.md): system shape and invariants.
- [docs/CLI_SPEC.md](docs/CLI_SPEC.md): command and JSON contract.
- [docs/CONFIG_SPEC.md](docs/CONFIG_SPEC.md): reusable policy config.
- [docs/GITHUB_POLICY_EXTRACTION.md](docs/GITHUB_POLICY_EXTRACTION.md): Creo
  extraction plan.
- [docs/WORKTREE_LEASING.md](docs/WORKTREE_LEASING.md): automation isolation
  design.
- [docs/SKILL_SPEC.md](docs/SKILL_SPEC.md): companion skill requirements.
- [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md): current progress
  and remaining migration work.
