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
- Prevent overlapping automations from corrupting checkouts or switching each
  other's branches.
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

### Worktree Leasing

- Baton must acquire a dedicated worktree for automation work.
- Baton must not mutate the caller's current checkout for work execution.
- Baton must refuse dirty managed worktrees.
- Baton must detect in-use leases.
- Baton must write lease metadata that survives process crashes.
- Baton must provide safe release and prune operations.

### GitHub Writes

- Baton must apply issue-policy labels and comments only when explicitly run
  with apply semantics.
- Baton must support issue/PR comments needed by the workflow.
- Baton must support label sync.
- Baton must not merge PRs in v1 except behind an explicit command and explicit
  user opt-in.

### Skill Packaging

- Baton must include a `skills/baton` skill package.
- The skill must instruct Codex to use Baton for deterministic state and leases.
- The skill must define stop conditions and escalation rules.
- The skill must not duplicate long config schemas or GitHub API details that
  are already enforced by the CLI.

## Safety Requirements

- Git destructive operations require proof that the target is Baton-managed.
- Dry-run must be available for install, label sync, branch setup, and prune.
- `pull_request_target` workflows must execute trusted Baton code, not
  untrusted PR-modified scripts.
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

- Baton should record local lease lifecycle events.
- Baton should expose enough state to answer:
  - why this item was selected;
  - why other items were skipped;
  - what checkout path is leased;
  - what GitHub writes were made;
  - what remains blocked.
