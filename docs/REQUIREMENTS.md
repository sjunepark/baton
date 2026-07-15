# Requirements

## Problem

Codex Desktop automations can run periodically, but each automation still needs
safe project context, deterministic GitHub state, and protection against
overlapping branch mutations. The original reference automation already uses
GitHub Issues and an `agent` staging branch, but the policy and helper scripts
live inside one repo and are not reusable.

Baton should extract this workflow into a reusable tool for the user's projects.

## Goals

- Reuse the GitHub issue and PR policy workflow across multiple repositories.
- Let Codex automations ask for one safe next candidate set instead of rediscovering
  GitHub state manually.
- Avoid instructing automations to mutate a user's primary checkout. Checkout
  isolation is supplied by the caller rather than managed by Baton.
- Keep policy and queue state inspectable through GitHub Issues, PRs, comments,
  checks, and labels.
- Make the CLI useful to humans and agents.
- Package a Codex skill that gives agents workflow judgment and CLI usage rules.

## Non-Goals

- General-purpose CI/CD platform.
- Multi-tenant service.
- Web dashboard in v1.
- Replacing GitHub Issues or Projects.
- Fully autonomous merge by default.
- Code generation inside the CLI.
- Worktree leasing, pooling, pruning, or checkout lifecycle management.
- Support for non-GitHub forges in v1.

## Users

- Primary user: solo developer running Codex automations across personal or
  private GitHub projects.
- Secondary user: Codex instances launched by scheduled automations.

## Functional Requirements

### Policy Extraction

- Baton must provide reusable issue policy behavior equivalent to the original
  reference issue policy action.
- Baton must provide reusable PR policy behavior equivalent to the original
  reference PR policy action.
- Baton must support repository-local policy config.
- Baton must provide installable GitHub workflow templates that call the Baton
  CLI rather than copied repo-local scripts.
- Baton must support controlled label groups so policy automation removes only
  labels it owns.
- Managed work-PR policy must use durable issue ownership, PR-local references
  and closing-keyword rules, and revision-bound commit facts. Mutable issue
  intake labels must not become merge authority.

### Queue Inspection

- Baton must list eligible issues according to policy labels and skip labels.
- Baton must detect active linked implementation PRs for an issue.
- Baton must list open agent PRs targeting the staging branch.
- Baton must fetch PR status checks and expose failing, pending, and successful
  checks.
- Baton must fetch thread-aware review comments, including resolved and outdated
  state.
- Baton must distinguish human, Codex, CodeRabbit, Greptile, and other bot
  comments when GitHub exposes enough author information.

### Next Action Classification

- Baton must produce the highest-priority next candidate set.
- Automation must choose exactly one returned candidate before acting.
- PR follow-up must outrank new issue intake when open agent PRs need action.
- Shared staging branch health problems must block ordinary new work.
- Human unresolved review threads must outrank bot review threads.
- Failing required checks must be actionable before review nits.
- Baton must return "no-op/report" when no safe mutation exists.
- Recommendations are advice, not Candidate claims. Until an implemented
  automatic dispatcher proves the need and a conditional mutation primitive,
  unattended dispatch must be singleton per repository.

### Execution Context

- Baton must not create, delete, prune, or lease worktrees.
- Baton must not instruct automation to mutate the user's primary checkout.
- Baton must expose enough branch/ref facts for a caller-provided execution
  context to do one selected unit safely.
- Baton must work well when invoked from isolated automation checkouts or
  manually prepared worktrees.
- Baton must not maintain a caller-style execution or completion ledger. The
  invoking automation owns execution provenance, retries, and validation
  evidence; GitHub owns semantic issue and PR state.

### GitHub Writes

- Baton must apply issue-policy labels and comments only when explicitly run
  with apply semantics.
- A merged work PR must move referenced issues to the configured
  awaiting-review state through a pure, explicitly applied, idempotent planner.
- Baton must support issue/PR comments needed by the workflow.
- Baton must support label sync.
- Baton must not merge PRs in v1 except behind an explicit command and explicit
  user opt-in.

### Skill Packaging

- Baton must include a `skills/baton` skill package.
- The skill must instruct Codex to use Baton for deterministic state and to
  require caller-provided checkout isolation before edits.
- The skill must define stop conditions and escalation rules.
- The skill must not duplicate long config schemas or GitHub API details that
  are already enforced by the CLI.

## Safety Requirements

- Baton must not perform destructive worktree cleanup.
- Dry-run must be available for install, label sync, and branch setup.
- `pull_request_target` workflows must execute trusted Baton code, not
  untrusted PR-modified scripts.
- Generated PR and transition workflows must cover all target branches. PR
  Policy's conservative prefilter may over-admit but must not skip a candidate
  the authoritative Go classifier would manage or reject. Transition admits
  promotions; Delivery Recorder owns managed work and synchronization events.
- Ordinary and known-fork PRs must be observable no-ops. Same-repository use of
  the reserved work prefix on the wrong target must fail explicitly; incomplete
  repository identity for a managed shape must fail as indeterminate.
- Managed issue ownership must come from a trusted, versioned record bound to
  stable issue identity. Labels are an index only and body/label edits do not
  revoke ownership.
- Delivery bootstrap and recording must use a pinned locked issue/checkpoint,
  bounded exact record reads, explicit dry-run/apply consent, stale
  preconditions, and operation reports. `delivery.authority: shadow` permits
  migration writes only; a reviewed change to `sealed` is the documented
  authority cutover.
- Managed promotion, work transition, and recommendation readers must require
  complete sealed delivery state and must not fall back to mutable PR bodies,
  ancestry selection, or an unbounded closed-staging-PR scan.
- A merged managed promotion must close still-open sealed-plan issues, remove
  their awaiting-review index, append exact base-integration evidence, and
  commit the cursor last. A committed duplicate must not inspect or re-close
  issues, and promotion closing keywords must not be completion authority.
- Bootstrap writes must run as the trusted generated recorder under its shared
  concurrency group; plan and apply must preserve exact writer provenance.
- A staged-record write or repair must re-request any successful managed
  promotion-policy check whose exact head contains that work revision.
- Promotion policy must bind observed base/staging SHAs to explicit integration
  evidence. Recorded merge/squash/rebase promotion results remain integrated
  even when absent from staging, provided the recorded promotion head remains
  in staging's lineage; later base movement requires synchronization.
- Synchronization is a human-reviewed base-to-staging PR whose merge preserves
  both histories. Baton records it but never pushes, merges, rebases, squashes,
  or rewrites staging.
- Adoption readiness must prove an authenticated live repository read and
  verify exact managed files, trusted active workflows, Actions policy,
  source-bound required checks, labels, ownership/delivery readiness, merge
  settings, branch rules, and queues. Unknown execution policy must degrade or
  block explicitly; blocked doctor results must exit nonzero.
- Incomplete delivery facts stop recommendations as degraded. Otherwise the
  order is staging-branch health, `sync-staging`, existing PR follow-up, then
  new issue intake.
- PR Policy and delivery mutations must share repository concurrency, and a
  record batch must re-list promotions and reacquire check rollups only after
  its final checkpoint commit before re-requesting checks.
- Baton must fail closed when GitHub API pagination limits prevent complete
  policy validation.
- Baton must redact tokens from logs and never print GitHub auth secrets.
- Baton must not auto-resolve review threads in v1.

## Portability Requirements

- CLI should be implemented in Go and installable as a single binary.
- macOS support is required first.
- Linux support should be straightforward.
- Windows support is optional for v1 but command design should not block it.

## Output Requirements

- Every automation-facing command must support `--json`.
- JSON must include stable `schemaVersion`.
- Error output must identify whether the failure is auth, config, GitHub API,
  git state, policy, or unsafe local state.
- Human output should be concise and derive from the same internal result.

## Observability Requirements

- Baton should expose enough state to answer:
  - why this item was selected;
  - why other items were skipped;
  - which branch/ref should be used for the selected item;
  - what GitHub writes were made;
  - what remains blocked.
