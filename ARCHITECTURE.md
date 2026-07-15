# Architecture

## Purpose

Baton is a local-first automation coordinator for solo-developer GitHub
projects. It converts GitHub Issues and PRs into deterministic agent work
instructions and enforces repository policy. Local execution isolation belongs
to the caller: an isolated automation checkout or user-managed worktree
provides the working directory before code edits happen.

The system is intentionally split:

```text
GitHub state + repo config
        |
        v
    Baton CLI
  deterministic facts,
  policy decisions,
  branch guidance
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
  - Fetches issues, PRs, labels, checks, reviews, review threads, commits,
    workflows, repository rules, Actions policy, and linked issue references.
  - Applies issue labels and comments when requested.
  - Uses typed API responses and keeps GraphQL queries near the features that
    need them.

- Policy engine
  - Computes issue policy decisions and one durable managed-issue ownership
    decision. The issue-form fingerprint is temporary migration evidence only.
  - Classifies PR ownership from branch shape plus base/head repository identity,
    then computes policy for managed work and promotions.
  - Produces pure decision objects for testing and GitHub Actions output.

- Queue classifier
  - Combines already-classified managed issues and PRs with CI checks, review
    threads, and branch health.
  - Returns the highest-priority next candidate set.
  - Does not implement the work.

- Installer and templates
  - Writes four GitHub workflows, issue templates, label manifests, and policy
    config into target repos.
  - Uses dry-run/diff output by default.

- Doctor
  - Acquires authenticated live repository facts and exact managed files from
    the default-branch revision.
  - Returns actionable ready, degraded, or blocked adoption compatibility
    without mutating local or GitHub state.

- Skill package
  - Lives under `skills/baton/`.
  - Teaches Codex how to interpret Baton JSON and when to stop.
  - References the CLI rather than duplicating GitHub API logic.

## Runtime Flows

### Issue Policy In GitHub Actions

1. GitHub emits an `issues` event, or a trusted ownership-record comment is
   edited or deleted.
2. Workflow invokes `baton issue-policy --event "$GITHUB_EVENT_PATH" --apply`.
3. Baton loads repo policy config from the base checkout.
4. Baton parses issue form sections.
5. Baton writes the trusted, versioned ownership record before controlled
   labels and the optional blocked-policy comment. Later body/label edits do
   not revoke ownership.
6. A trusted ownership-record edit/delete event repairs the record
   idempotently; untrusted marker comments have no authority.

### PR Policy In GitHub Actions

1. GitHub emits `pull_request_target` for every target branch.
2. A conservative job prefilter skips obvious non-candidates before setup but
   admits every prefixed-work, promotion, misrouted, or indeterminate shape.
3. The workflow checks out trusted base SHA and invokes `baton pr-policy`.
4. Baton authoritatively classifies repository identity plus branch shape as
   `work`, `promotion`, `misroutedWork`, `indeterminate`, or `unmanaged`.
5. Unmanaged PRs are no-ops. Managed flows validate durable issue ownership,
   references, closing keywords, commit subjects, and bounded GitHub facts.
   Mutable implementation, skip, and trivial labels remain queue-intake facts
   and never enter work-PR merge policy.
6. After commit acquisition, Baton rechecks the current PR head SHA against the
   event so commit policy cannot cross revisions during a concurrent push.

### Work-Item Transition In GitHub Actions

1. GitHub emits a closed `pull_request_target` event.
2. Workflow checks out trusted base SHA and installs an exact released Baton.
3. `baton pr-transition` classifies the PR from repository policy.
4. The recorder first commits the exact staged-work record. A merged work PR
   then plans one awaiting-review label operation per immutable issue reference
   in that record; PR title/body edits cannot change the transition.
5. Apply verifies the exact PR revision, staged-record digest, issue node ID,
   and durable ownership digest before the first label write, then returns an
   operation report.
6. A merged promotion resolves the exact active seal, preflights every issue,
   closes open delivered issues, removes their awaiting-review index, appends
   base-integration evidence, and commits the promotion cursor last. A cursor
   already naming that promotion makes the event a total no-op.
7. Repository recommendations derive their awaiting-review backstop from the
   bounded active ledger window. Missing or incomplete ledger state degrades
   recommendations; Baton never falls back to scanning all closed staging PRs.

### Delivery Ledger In GitHub Actions

1. A separate trusted `pull_request_target` workflow observes merged work PRs;
   manual dispatch reconciles missed events from checkpoint coverage.
2. It installs an exact released Baton under one repository-scoped delivery
   concurrency group shared with PR Policy, so a promotion check either
   finishes before the ledger mutation or starts after it. Both workflows use
   `queue: max`; recorder reconciliation repairs a delivery event lost only if
   GitHub's 100-run group queue itself is exceeded.
3. Baton verifies the pinned locked issue/checkpoint and fetches only exact
   checkpoint references; ambiguous retry recovery is capped at 100 comments.
4. Legacy ownership is backfilled before the immutable staged-work record, and
   the checkpoint update is the final commit point.
5. Promotion rechecks are deduplicated and deferred until the complete batch;
   the open promotion set and check rollups are acquired again after commit.
6. A complete reconciliation with no missing managed record advances the
   coverage watermark to the exact staging head observed by that scan.
7. Bootstrap plan/apply jobs use the same workflow run and concurrency group;
   an environment approval gates apply without changing writer provenance.
8. Successful PR-policy checks on affected promotion heads are re-requested so
   a record/repair race cannot leave a stale green check.
9. Bootstrap compares bounded ancestry containment with the planned ledger
   projection for every open promotion and blocks on mismatches. Committing the
   reviewed locator under `delivery.authority: shadow` establishes the migration
   checkpoint; PR Policy does not gain delivery authority at that point. Only a
   separate reviewed change from `shadow` to `sealed` activates authority, after
   which PR Policy appends and activates exact promotion seals bound to PR
   node/base/head, cursor, coverage, and included or reviewed-excluded records.
   There is no ancestry fallback.

### Automation Work Selection

1. Codex automation starts in a project directory.
2. The Baton skill tells Codex to call `baton snapshot --json`.
3. Baton fetches queue state and returns a repository snapshot. Automation
   continues only when its outcome is `actionable`, using the winning candidate
   set:
   - Branch health fix when the shared agent branch is red.
   - PR follow-up candidates from the highest check-state tier.
   - Issue intake when no PR follow-up blocks new work.
4. Non-actionable outcomes stop and report without selecting work.
5. Codex chooses exactly one candidate.
6. Codex verifies it is already operating in a caller-provided isolated
   checkout before editing.
7. Codex validates, pushes/comments, and reports the outcome to its caller.
   The invoking automation owns execution completion; GitHub owns
   semantic issue/PR state.

### Adoption Compatibility

1. Doctor loads the same repository policy and installer target used by init,
   resolves the live repository, and proves authentication with a repository
   read.
2. It compares managed files at the exact default-branch SHA and checks labels,
   ownership and delivery readiness, workflow trust and state, Actions policy,
   source-bound required checks, branch rules, merge settings, and queues.
3. Missing execution facts fail closed or degrade explicitly. Editable guidance
   drift warns; workflow, policy, ownership, or delivery incompatibility blocks.
4. Adoption completes only after a rerun returns `ready` or a human explicitly
   accepts every named degraded capability. A blocked result exits nonzero.

## Code Map

```text
cmd/baton/
  main.go: CLI entrypoint

internal/config/
  load and validate baton policy config

internal/gh/
  typed GitHub REST/GraphQL client and fixtures

internal/delivery/
  bounded delivery-ledger contracts, codecs, planners, and integration facts

internal/policy/
  pure issue and PR policy decisions

internal/queue/
  queue snapshots and next-action classifier

internal/git/
  branch planning and refs

internal/install/
  embedded target-repo templates and init planning

internal/doctor/
  live adoption fact acquisition and compatibility evaluation

internal/labels/, internal/workitem/
  label manifest sync, readiness, and PR transition planning

skills/baton/
  Codex skill package
```

## Invariants

- Automation work never mutates the user's primary checkout.
- Baton does not create, delete, prune, or lease worktrees.
- The caller owns checkout isolation and lifecycle.
- One automation run handles at most one unit of work.
- Baton Recommendations are not repository Candidate claims. Until a verified
  coordination contract exists, each repository has one unattended dispatcher.
- GitHub Issues are the operational queue.
- Target repo config is the policy source of truth.
- The CLI returns facts and policy decisions; Codex performs code changes.
- All mutating commands have a dry-run mode, pure planner, or explicit
  `--apply`/`--yes` gate.
- Human review comments outrank bot comments.
- Unresolved blocking review comments and failing required checks block merge.
- Baton never relies on PR-modified policy code in `pull_request_target`.
- Labels are an ownership index, not ownership authority. A trusted ownership
  record is required once the bounded legacy-fingerprint migration ends.
- Branch targets do not establish ownership. Same-repository reserved work
  prefixes and the exact staging-to-base pair express intent; durable issue and
  delivery records establish resource and completion authority.
- Promotion and recommendation authority comes from complete sealed delivery
  state, never mutable PR prose, raw ancestry selection, or unbounded history.

## External Dependencies

- GitHub REST and GraphQL APIs.
- Local `git`.
- Optional `gh` fallback for auth or operations that are too costly to
  duplicate initially.

## Decision Records

- [GitHub work-item transitions](docs/adr/0002-github-work-item-transitions.md)
  defines explicit post-merge lifecycle effects.
- [Explicit resource ownership](docs/adr/0004-explicit-resource-ownership.md)
  separates intent, index facts, and durable authority.
- [Dedicated delivery ledger](docs/adr/0005-dedicated-delivery-ledger.md)
  defines bounded delivery, promotion, and synchronization evidence.
