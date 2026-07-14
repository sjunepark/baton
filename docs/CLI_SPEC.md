# CLI Spec

## Command Style

Binary name:

```sh
baton
```

Automation-facing commands must support stable JSON:

```sh
--json
--config <path>
--repo <owner/name>
```

For the policy-backed `snapshot`, `queue`, `prs`, and `next` commands, Baton resolves the git
top-level, policy config, configured remote, and GitHub identity as one context. `--repo` is an
identity assertion, not an unchecked override: when the configured checkout
remote identifies a different repository, Baton returns structured error v1
with category `config` and exit 3 before contacting GitHub. There is no mismatch
override because no maintained workflow requires one.

The remote host must also match the GitHub endpoint: `github.com` for the
default API, or the host represented by `GITHUB_API_URL` for GitHub Enterprise.

`pr`, `checks`, and `review-threads` also validate the checkout target. They use
the configured remote when policy is available and otherwise use a policy-free
target context through `origin`. Event-backed policy commands, label sync, and
transition writes preserve their documented flag/event/environment target
precedence and validate the selected `owner/name` before GitHub I/O.

When invoked below the repository root, implicit config discovery starts at the
resolved git top-level. An explicit relative `--config` path remains relative
to the invocation directory for compatibility.

Read-heavy commands also support:

```sh
--format text|json|toon
```

`--json` is a compatibility alias for `--format json` on commands that support
both. TOON output is for compact agent consumption.

Mutating plan/apply commands must use one of:

```sh
--apply
--yes
```

depending on whether they apply a computed plan or require explicit
confirmation.

## Exit Codes

- `0`: success, including clean no-op.
- `1`: policy failure or unsafe state.
- `2`: invalid CLI usage.
- `3`: config error.
- `4`: auth error.
- `5`: GitHub API error.
- `6`: local git error.

## Core Commands

### `baton`

With no arguments, show the local Baton dashboard. Use `baton --help` for
global command help. Use `baton version` or `baton --version` to print the
current CLI version.

### `baton home`

Show a local Baton dashboard without failing on missing config, auth, or remote
state.

Examples:

```sh
baton home --format toon
baton home --json
```

### `baton init`

Reconcile target-repository files rendered from one compiled Repository
Policy. Dry-run JSON includes exact desired content, full diffs,
ownership/conflict classification, observed content preconditions, and a
stable plan identity. Apply refuses all conflicts before its first target write
and uses atomic per-file replacement.

Implemented flags:

<!-- x-release-please-start-version -->
```sh
baton init --dry-run
baton init --apply
baton init --profile default
baton init --go-install github.com/sjunepark/baton/cmd/baton@v0.5.1
baton init --install-command '<trusted install command>'
```
<!-- x-release-please-end -->

The generated work-item transition workflow is a trusted mutation boundary.
It always installs `--go-install` at an exact `@vX.Y.Z` and ignores the
arbitrary `--install-command` escape hatch used by non-mutating policy checks.

Must create or update, with explicit user confirmation:

- `.github/baton.yml`
- `.github/labels.yml`
- `.github/ISSUE_WORKFLOW.md`
- `.github/ISSUE_TEMPLATE/agent-work.yml`
- `.github/workflows/issue-policy.yml`
- `.github/workflows/pr-policy.yml`
- `.github/workflows/work-item-transition.yml`

### `baton doctor`

Check local and remote readiness.

Examples:

```sh
baton doctor --format toon
baton doctor --json
```

Checks:

- config can be loaded;
- `git` is available;
- repo root can be resolved;
- GitHub auth works;
- remote repository can be resolved;
- staging branch state is known;
- labels are present or diffable.

### `baton migrate-config`

Convert a legacy `.github/agent-issue-policy.yml` into `.github/baton.yml`.

Examples:

```sh
baton migrate-config --dry-run
baton migrate-config --apply
baton migrate-config --apply --yes
```

### `baton sync-labels`

Sync labels from config or label manifest.

Examples:

```sh
baton sync-labels --dry-run
baton sync-labels --apply
```

### `baton ensure-branch`

Plan or apply staging branch setup.

Examples:

```sh
baton ensure-branch
baton ensure-branch --apply
```

Must preserve the original reference behavior:

- create the configured local/published staging branch only when it exactly
  matches the configured base;
- refuse force resets;
- preserve an existing remote staging branch without treating its expected
  staged history as a readiness warning;
- do not configure branch protection.

### `baton issue-policy`

Evaluate and optionally apply issue policy.

Examples:

```sh
baton issue-policy --event "$GITHUB_EVENT_PATH" --json
baton issue-policy --event "$GITHUB_EVENT_PATH" --apply
baton issue-policy --body-file issue.md --labels "bug,agent:ready-trivial" --json
```

JSON result:

```json
{
  "schemaVersion": 1,
  "kind": "issuePolicyDecision",
  "isFormIssue": true,
  "labelsToAdd": ["agent:ready-trivial"],
  "labelsToRemove": ["needs-info"],
  "missingRequiredSections": [],
  "policyCommentBody": null
}
```

### `baton pr-policy`

Evaluate PR policy for GitHub Actions.

Examples:

```sh
baton pr-policy --event "$GITHUB_EVENT_PATH"
baton pr-policy --event "$GITHUB_EVENT_PATH" --json
```

JSON result:

```json
{
  "schemaVersion": 1,
  "kind": "prPolicyDecision",
  "flow": "promotion",
  "errors": [],
  "warnings": [],
  "referencedIssues": [],
  "closingIssues": [4],
  "commitListingReachedCap": false,
  "promotionFacts": {
    "expectedIssues": [4],
    "complete": true
  }
}
```

`promotionFacts` is present only for staging-to-base promotions. `expectedIssues`
is derived from merged Baton work PRs included between the event's base and head
revisions. A complete empty set allows a manual-only promotion without a closing
keyword. `complete: false` is a non-passing verification result. These additive
fields retain `prPolicyDecision` schema version 1.

`complete: false` means GitHub returned a bounded but insufficient comparison
or association result, or an included Baton work PR lacks the configured issue
reference. A failed GitHub request is not a policy decision; it returns the
existing structured GitHub error with exit code 5.

### `baton pr-transition`

Plan or apply the one persisted work-item transition: a merged work PR adds
the configured `awaiting_review_label` to every referenced open issue.

```sh
baton pr-transition --event "$GITHUB_EVENT_PATH" --dry-run --json
baton pr-transition --event "$GITHUB_EVENT_PATH" --apply --json
```

Exactly one of `--dry-run` and `--apply` is required. Apply rechecks the PR
revision and every issue before the first write, skips closed or already-labeled
issues, and reports unavoidable partial GitHub effects. Promotion and manual
issue closure remain GitHub-native; Baton does not merge or close them.

### `baton snapshot`

Acquire repository facts once and return `repositorySnapshot` schema v1 with
the acquisition window, completeness and warnings, queue facts, revision-bound
branch and pull-request facts, and one typed Recommendation.

```sh
baton snapshot --format toon
baton snapshot --repo owner/name --json
```

Recommendation Outcome and Action are separate. Callers must gate on Outcome:
only `actionable` identifies one immediately useful agent mutation.
`human_choice_required`, `waiting`, `blocked`, `idle`, and `degraded` do not
authorize work. Action is advice and never represents a running or completed
operation.

### `baton queue`

Return open issues and why each is eligible or skipped.
The `counts.eligibleByAction` aggregate separates implementation-ready work
from investigation-only work so agents can distinguish the full eligible queue
from the next automation tier without an extra call.

Example:

```sh
baton queue --format toon
baton queue --fields number,title,action,priorityLabel,reasons --format toon
baton queue --json
```

### `baton prs`

Return open agent PRs with checks and review state summary.

Example:

```sh
baton prs --format toon
baton prs --fields number,title,headRef,checkState --format toon
baton prs --json
```

### `baton pr <number>`

Return a precomputed dashboard for one PR.

Includes:

- branch/base/head;
- referenced issue numbers;
- issue label readiness when Baton config and open issue data are available;
- check state, count, and summary counts;
- review-thread count and unresolved human/bot/unknown-author summary counts;
- likely next command;
- warnings when optional enrichment cannot be fetched.

Examples:

```sh
baton pr 12 --json
```

### `baton review-threads <number>`

Return GitHub review threads with thread-aware state.

Must include:

- `isResolved`;
- `isOutdated`;
- `path`;
- `line`;
- `author`;
- bodies;
- whether Baton classifies it as human, known bot, or unknown bot.

Examples:

```sh
baton review-threads 12 --format toon
baton review-threads 12 --full --json
```

### `baton checks <number>`

Return PR checks grouped by status.

Must identify:

- failing checks;
- pending checks;
- successful checks;
- skipped/cancelled checks;
- detail URLs;
- workflow/job names.

Examples:

```sh
baton checks 12 --format toon
baton checks 12 --fields name,state,url --format toon
baton checks 12 --json
```

### `baton next`

Return the highest-priority next candidate set. Agents and users choose exactly
one returned candidate before acting.
Use `--action issue-investigation` only when a human intentionally wants to
inspect investigation candidates instead of the default automation priority
order.

Example:

```sh
baton next --format toon
baton next --action issue-investigation --format toon
baton next --json
```

`nextCandidates` v2 is a lossy compatibility projection from the unified
snapshot implementation. It cannot express the new Outcome distinction and
must not be interpreted as execution state. New integrations should consume
`baton snapshot --json`.

JSON result:

```json
{
  "schemaVersion": 2,
  "kind": "nextCandidates",
  "selectedAction": "pr-followup",
  "repo": "example-org/example-repo",
  "reason": "failing-checks",
  "selectionReason": "failing-checks-precedes-lower-priority-work",
  "selectionRequired": true,
  "candidates": [
    {
      "type": "pullRequest",
      "number": 8,
      "title": "Fix branch policy",
      "url": "https://github.com/example-org/example-repo/pull/8",
      "headRef": "agent-work/github-agent-branch-policy",
      "baseRef": "agent"
    },
    {
      "type": "pullRequest",
      "number": 12,
      "title": "Update checks",
      "url": "https://github.com/example-org/example-repo/pull/12",
      "headRef": "agent-work/checks",
      "baseRef": "agent"
    }
  ],
  "deferredEligibleItems": [
    {
      "type": "issue",
      "number": 14,
      "title": "Investigate queue drift",
      "url": "https://github.com/example-org/example-repo/issues/14",
      "priorityLabel": "priority:p2"
    }
  ],
  "blockedItems": [],
  "instructions": [
    "Choose exactly one candidate.",
    "Work in a caller-provided isolated checkout.",
    "Push to the existing PR branch.",
    "Do not open a new PR."
  ]
}
```

Allowed `selectedAction` values:

- `pr-followup`
- `branch-health`
- `issue-implementation`
- `issue-investigation`
- `none`

`deferredEligibleItems[]` contains eligible work that Baton did not return in
`candidates[]` because a higher-priority action, tier, or configured issue
priority took precedence. Issue candidates may include `priorityLabel`; queue
snapshot issue entries may also include `priorityRank` so JSON snapshots retain
configured priority ordering.

## Human Output

Text output should be concise and actionable:

```text
Next action: PR #8 follow-up
Reason: Typecheck failing
Branch: agent-work/github-agent-branch-policy
Isolation: caller-provided checkout required before edits
Stop conditions: unresolved human review, ambiguous scope, missing isolated checkout
```

## GitHub Actions Usage

Issue policy:

```yaml
- name: Apply Baton issue policy
  run: baton issue-policy --event "$GITHUB_EVENT_PATH" --apply
```

PR policy:

```yaml
- name: Check Baton PR policy
  run: baton pr-policy --event "$GITHUB_EVENT_PATH"
```

The actual install step is an implementation detail. Options include GitHub
release binary download, `go install`, or an action wrapper.
