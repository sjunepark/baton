# Baton

Baton is a Go CLI and companion Codex skill for reusable GitHub issue/PR agent
workflows. It turns repository-local agent workflow policy into deterministic
commands for policy checks, queue inspection, safe worktree leasing, and
target-repository installation.

The CLI owns deterministic GitHub, git, policy, and lease state. Codex keeps
the judgment work: deciding implementation shape, handling ambiguous review
feedback, and reporting decisions back to the user.

## Status

Implemented:

- issue and PR policy behavior extracted from the original reference workflow;
- GitHub workflow, issue template, label, and config installation templates;
- GitHub issue/PR/check/review-thread queue inspection;
- `baton next --json` for one recommended automation action;
- native worktree leasing with release and prune safety gates;
- `doctor`, `complete`, `migrate-config`, `sync-labels`, and `ensure-branch`;
- a bundled Codex skill in `skills/baton`.

The default generated GitHub Actions install command uses the published module
path:

```sh
go install github.com/sjunepark/baton/cmd/baton@v0.1.3
```

Use `baton init --go-install` or `baton init --install-command` to pin a
different trusted source for a consuming repository.

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
baton init --apply --go-install github.com/sjunepark/baton/cmd/baton@v0.1.3
```

For a pinned release or alternate trusted source, pass a full command:

```sh
baton init --apply --install-command 'go install github.com/sjunepark/baton/cmd/baton@v0.1.3'
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
- [docs/GITHUB_POLICY_EXTRACTION.md](docs/GITHUB_POLICY_EXTRACTION.md):
  extraction plan for the original reference workflow.
- [docs/WORKTREE_LEASING.md](docs/WORKTREE_LEASING.md): automation isolation
  design.
- [docs/SKILL_SPEC.md](docs/SKILL_SPEC.md): companion skill requirements.
- [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md): current progress
  and remaining migration work.
