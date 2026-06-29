# Architecture

## Purpose

Baton is a local-first automation coordinator for solo-developer GitHub
projects. It converts GitHub Issues and PRs into deterministic agent work
instructions, enforces repository policy, and isolates local execution with
managed worktree leases.

The system is intentionally split:

```text
GitHub state + repo config
        |
        v
    Baton CLI
  deterministic facts,
  policy decisions,
  worktree leases
        |
        v
   Codex + Baton skill
  judgment, code edits,
  summaries, escalation
```

## Component Map

- CLI entrypoint
  - Parses command flags.
  - Loads target repository config.
  - Calls internal planners and renderers.
  - Emits JSON for automation and concise text for humans.

- Config loader
  - Reads Baton policy config from the target repo.
  - Supports the legacy agent issue-policy config as a migration input.
  - Validates labels, branch names, issue form section IDs, and mode mappings.

- GitHub client
  - Fetches issues, PRs, labels, checks, reviews, review threads, commits, and
    linked issue references.
  - Applies issue labels and comments when requested.
  - Uses typed API responses and keeps GraphQL queries near the features that
    need them.

- Policy engine
  - Computes issue policy decisions from issue form sections and current labels.
  - Computes PR policy decisions from base/head branches, linked issues, commit
    subjects, and closing/reference keywords.
  - Produces pure decision objects for testing and GitHub Actions output.

- Queue classifier
  - Combines issues, open agent PRs, CI checks, review threads, and branch health.
  - Returns the highest-priority next candidate set.
  - Does not implement the work.

- Worktree lease manager
  - Acquires an isolated working directory for one automation unit.
  - Refuses dirty, in-use, or unsafe candidates.
  - Records lease metadata and release state.
  - Supports a native backend first, with optional Treehouse backend later.

- Installer and templates
  - Writes GitHub workflows, issue templates, label manifests, and policy config
    into target repos.
  - Uses dry-run/diff output by default.

- Skill package
  - Lives under `skills/baton/`.
  - Teaches Codex how to interpret Baton JSON and when to stop.
  - References the CLI rather than duplicating GitHub API logic.

## Runtime Flows

### Issue Policy In GitHub Actions

1. GitHub emits an `issues` event.
2. Workflow invokes `baton issue-policy --event "$GITHUB_EVENT_PATH" --apply`.
3. Baton loads repo policy config from the base checkout.
4. Baton parses issue form sections.
5. Baton computes labels to add/remove and optional blocked-policy comment.
6. Baton applies only controlled labels and the updatable policy comment.

### PR Policy In GitHub Actions

1. GitHub emits a `pull_request_target` event for configured branches.
2. Workflow checks out trusted base SHA.
3. Workflow invokes `baton pr-policy --event "$GITHUB_EVENT_PATH"`.
4. Baton loads target repo policy.
5. Baton validates branch direction, references, linked issue labels, closing
   keywords, commit subject quality, and GitHub commit listing caps.
6. Baton exits non-zero with actionable errors when policy fails.

### Automation Work Selection

1. Codex automation starts in a project directory.
2. The Baton skill tells Codex to call `baton next --json`.
3. Baton fetches queue state and returns the winning candidate set:
   - Branch health fix when the shared agent branch is red.
   - PR follow-up candidates from the highest check-state tier.
   - Issue intake when no PR follow-up blocks new work.
   - No-op/report when no mutation is appropriate.
4. Codex chooses exactly one candidate and acquires the appropriate lease.
5. Codex works only in the leased path.
6. Codex validates, pushes/comments, and calls `baton complete` or
   `baton release`.

## Code Map

```text
cmd/baton/
  main.go: CLI entrypoint

internal/config/
  load and validate baton policy config

internal/gh/
  typed GitHub REST/GraphQL client and fixtures

internal/policy/
  pure issue and PR policy decisions

internal/queue/
  queue snapshots and next-action classifier

internal/git/
  branch planning, refs, worktree operations

internal/lease/
  lease records, acquisition, release, stale detection

internal/install/
  embedded target-repo templates and init planning

internal/doctor/, internal/complete/, internal/labels/
  readiness checks, completion metadata, and label manifest sync

skills/baton/
  Codex skill package
```

## Invariants

- Automation work never mutates the user's primary checkout.
- One automation run claims at most one unit of work.
- GitHub Issues are the operational queue.
- Target repo config is the policy source of truth.
- The CLI returns facts and policy decisions; Codex performs code changes.
- All mutating commands have a dry-run mode, pure planner, or explicit
  `--apply`/`--yes` gate.
- Human review comments outrank bot comments.
- Unresolved blocking review comments and failing required checks block merge.
- Baton never relies on PR-modified policy code in `pull_request_target`.

## External Dependencies

- GitHub REST and GraphQL APIs.
- Local `git`.
- Optional `gh` fallback for auth or operations that are too costly to
  duplicate initially.
- Optional Treehouse backend for pooled worktrees after native leasing works.
