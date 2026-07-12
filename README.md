# Baton

Baton is a Go CLI and companion Codex skill for reusable GitHub issue/PR agent
workflows. It turns repository-local agent workflow policy into deterministic
commands for policy checks, queue inspection, branch planning, work-item
transitions, and target-repository installation.

The CLI owns deterministic GitHub, git, policy, queue, and work-item facts.
The caller owns checkout isolation. Codex keeps the judgment work: deciding
implementation shape, handling ambiguous review feedback, and reporting
decisions back to the user.

## Status

Implemented:

- issue and PR policy behavior extracted from the original reference workflow;
- GitHub workflow, issue template, label, and config installation templates;
- GitHub issue/PR/check/review-thread queue inspection;
- `baton home --format toon` and `baton snapshot --format toon` for compact agent
  context and the next candidate set;
- `doctor`, `migrate-config`, `sync-labels`, `ensure-branch`, and
  `pr-transition`;
- a bundled Codex skill in `skills/baton`.

The default generated GitHub Actions install command uses the published module
path:

<!-- x-release-please-start-version -->
```sh
go install github.com/sjunepark/baton/cmd/baton@v0.4.4
```
<!-- x-release-please-end -->

Use `baton init --go-install` or `baton init --install-command` to pin a
different trusted source for a consuming repository.

## Quick Start

Build or run locally:

```sh
go test ./...
go run ./cmd/baton --help
go run ./cmd/baton home --format toon
go run ./cmd/baton doctor --format toon
```

Preview installation files for a target repository:

```sh
baton init --dry-run --json
```

Apply installation files after reviewing the plan:

<!-- x-release-please-start-version -->
```sh
baton init --apply --go-install github.com/sjunepark/baton/cmd/baton@v0.4.4
```
<!-- x-release-please-end -->

For a pinned release or alternate trusted source, pass a full command:

<!-- x-release-please-start-version -->
```sh
baton init --apply --install-command 'go install github.com/sjunepark/baton/cmd/baton@v0.4.4'
```
<!-- x-release-please-end -->

Inspect a repository queue:

```sh
baton queue --format toon --repo owner/name
baton prs --format toon --repo owner/name
baton snapshot --format toon --repo owner/name
baton next --format toon --repo owner/name
```

`baton snapshot` returns one bounded repository observation with explicit
completeness and a typed recommendation outcome. Only an `actionable` outcome
with one candidate means agent work is currently useful; it is still not
execution state or merge authority. `baton next` remains the v2 compatibility
projection for existing callers. Use
`baton queue` for the full eligible issue list, or
`baton next --action issue-investigation --format toon --repo owner/name` when
a human intentionally wants to inspect investigation-only candidates.

Use `--json` instead of `--format toon` when a script needs the stable
automation contract.

Report execution summaries and validation to the caller. Coda or the invoking
automation owns Run completion; GitHub issue/PR state owns semantic work-item
completion. Baton does not maintain a local completion ledger.

## Target Repository Workflows

`baton init` installs:

```text
.github/baton.yml
.github/labels.yml
.github/ISSUE_WORKFLOW.md
.github/ISSUE_TEMPLATE/agent-work.yml
.github/workflows/issue-policy.yml
.github/workflows/pr-policy.yml
.github/workflows/work-item-transition.yml
```

`.github/baton.yml` includes `setup.baseline_baton_version`, which records the
Baton release the repository setup files were last reviewed or applied against.
The workflow install command remains the runtime pin, and `version` remains the
config schema version.

`--install-command` customizes the issue/PR policy workflow install step. The
write-capable work-item transition workflow intentionally ignores arbitrary
shell install commands and installs the exact `--go-install` target instead.
Use an exact `module/path@vX.Y.Z` with `--go-install` when adopting an alternate
Baton pin for that workflow.

The generated workflows install trusted Baton code, then run:

```sh
baton issue-policy --event "$GITHUB_EVENT_PATH" --apply
baton pr-policy --event "$GITHUB_EVENT_PATH"
baton pr-transition --event "$GITHUB_EVENT_PATH" --apply --json

# Preview the same transition without GitHub writes.
baton pr-transition --event "$GITHUB_EVENT_PATH" --dry-run --json
```

`pull_request_target` policy runs check out the trusted base SHA before
installing Baton so PR-modified repository code is not executed.

## Project Map

- [docs/index.html](docs/index.html): interactive documentation — concepts, a
  hands-on tutorial, and the complete command/config/JSON reference in one
  searchable page. Open in a browser; start here. (The old `overview.html`,
  `tutorial.html`, and `reference.html` now redirect here.)
- [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md): product and safety
  requirements.
- [ARCHITECTURE.md](ARCHITECTURE.md): system shape and invariants.
- [docs/CLI_SPEC.md](docs/CLI_SPEC.md): command and JSON contract.
- [docs/USER_FLOWS.md](docs/USER_FLOWS.md): human-facing Baton skill and CLI
  workflows.
- [docs/CONFIG_SPEC.md](docs/CONFIG_SPEC.md): reusable policy config.
- [docs/RELEASE.md](docs/RELEASE.md): Release Please ownership, commit
  message rules, and SemVer policy.
- [docs/adopter-updates/](docs/adopter-updates/): per-release notes for
  repositories that have adopted Baton.
- [docs/EXECUTION_CONTEXT.md](docs/EXECUTION_CONTEXT.md): checkout isolation
  boundary and why Baton does not manage worktrees.
- [docs/GITHUB_POLICY_EXTRACTION.md](docs/GITHUB_POLICY_EXTRACTION.md):
  extraction plan for the original reference workflow.
- [docs/SKILL_SPEC.md](docs/SKILL_SPEC.md): companion skill requirements.
- [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md): current progress
  and remaining migration work.
