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
go install github.com/sjunepark/baton/cmd/baton@v0.6.0
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
go run ./cmd/baton doctor --repo owner/name --format toon
```

Preview installation files for a target repository:

```sh
baton init --dry-run --json
```

Apply installation files after reviewing the plan:

<!-- x-release-please-start-version -->
```sh
baton init --apply --go-install github.com/sjunepark/baton/cmd/baton@v0.6.0
```
<!-- x-release-please-end -->

For a pinned release or alternate trusted source, pass a full command:

<!-- x-release-please-start-version -->
```sh
baton init --apply --install-command 'go install github.com/sjunepark/baton/cmd/baton@v0.6.0'
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
execution state or merge authority. `baton next` remains the v3 compatibility
projection for existing callers. Use
`baton queue` for the full eligible issue list, or
`baton next --action issue-investigation --format toon --repo owner/name` when
a human intentionally wants to inspect investigation-only candidates.

Use `--json` instead of `--format toon` when a script needs the stable
automation contract.

Report execution summaries and validation to the caller. The invoking
automation owns execution completion; GitHub issue/PR state owns semantic work-item
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
.github/workflows/delivery-recorder.yml
```

`.github/baton.yml` includes `setup.baseline_baton_version`, which records the
Baton release the repository setup files were last reviewed or applied against.
The workflow install command remains the runtime pin, and `version` remains the
config schema version.

`--install-command` customizes the issue/PR policy workflow install step. The
write-capable transition and delivery-recorder workflows intentionally ignore
arbitrary shell install commands and install the exact `--go-install` target.
Use an exact `module/path@vX.Y.Z` with `--go-install` when adopting an alternate
Baton pin for that workflow. Pass the same reviewed option to `baton doctor`
so its exact trusted-file reconciliation uses the intended installer.

The generated workflows install trusted Baton code, then run:

```sh
baton issue-policy --event "$GITHUB_EVENT_PATH" --apply
baton pr-policy --event "$GITHUB_EVENT_PATH"
baton pr-transition --event "$GITHUB_EVENT_PATH" --apply --json
baton delivery-record --event "$GITHUB_EVENT_PATH" --apply --json

# Preview the same transition without GitHub writes.
baton pr-transition --event "$GITHUB_EVENT_PATH" --dry-run --json
```

`pull_request_target` policy runs check out the trusted base SHA before
installing Baton so PR-modified repository code is not executed. The generated
PR policy and transition workflows listen on all target branches and leave the
Go classifier authoritative. PR Policy conservatively admits every managed
candidate shape; Work Item Transition admits promotions, while Delivery
Recorder owns work and synchronization transitions. Ordinary and fork PRs are
unmanaged no-ops; a same-repository `agent-work/` branch is explicit Baton
intent and must target staging.

PR Policy shares the repository delivery concurrency group with the Delivery
Recorder. Promotion checks therefore cannot start inside a ledger batch; the
recorder re-lists open promotions and re-acquires their check rollups after the
final checkpoint commit. Generated workflows opt into GitHub's `queue: max`
mode so up to 100 pending runs wait instead of replacing one another.

Issue policy writes a trusted, versioned ownership record before its managed
index label and other controlled labels. Queue, PR-reference, and transition
paths require that ownership decision; labels alone do not enroll an issue.
Existing form fingerprints remain a temporary read/backfill path.
Implementation and skip labels continue to decide issue intake and
recommendation, but changing them after a managed work PR opens does not alter
that PR's merge policy. Work PR policy uses durable ownership, PR references,
forbidden closing keywords, and commit facts.

Delivery recording is disabled until `.github/baton.yml` pins a reviewed
ledger locator. Bootstrap compares the bounded ancestry projection with the
planned ledger projection and blocks on every mismatch. Commit the locator
with `delivery.authority: shadow`, complete bootstrap, then review the explicit
change to `sealed`. Promotion policy thereafter seals the exact ledger plan and
uses it as authority. After merge, Work Item Transition closes the seal's
still-open issues, removes the awaiting-review index, records base integration,
and commits the cursor last; PR closing keywords are not delivery authority.
Recommendation commands also require the bounded ledger and do not fall back
to closed-PR history. Bootstrap runs through the same generated workflow and
concurrency group; protect its `baton-delivery-bootstrap`
environment with required reviewers. See
[Delivery bootstrap](docs/DELIVERY_BOOTSTRAP.md).

Ordinary PRs may still merge directly to base. Baton then recommends a normal
human-reviewed base-to-staging sync PR before further managed work. Use a merge
commit so both histories remain ancestors of staging; Baton records the result
but never pushes, merges, squashes, rebases, or rewrites staging. `doctor`
reads the live repository and blocks workflow, Actions, ownership/delivery,
required-check, merge-setting, ruleset, or merge-queue incompatibilities. Do
not finish adoption while its `readyState` is `blocked`.

## Security and Support Boundaries

- Baton does not currently support using selected-action
  `patterns_allowed` as the only authorization for generated actions in a
  private enterprise repository. For private repositories, enable GitHub-owned
  actions explicitly; otherwise `doctor` blocks adoption. Enterprise-aware
  pattern evaluation is deferred until Baton can acquire enterprise membership
  authoritatively without making organization-level access a baseline setup
  requirement.
- Baton's setup-free ownership provenance trusts `github-actions[bot]` rather
  than a dedicated Baton App. Every repository workflow granted `issues: write`
  is therefore inside the trust boundary: another such workflow could mint a
  syntactically valid ownership record. Grant that permission only to reviewed
  default-branch workflows, protect workflow-file changes, and never execute
  pull-request-controlled code in a write-enabled `pull_request_target` job.
  The record digest detects malformed or inconsistent data; it is not a
  signature against another trusted workflow. See
  [Explicit resource ownership](docs/adr/0004-explicit-resource-ownership.md).

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
