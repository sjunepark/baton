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
global command help.

### `baton home`

Show a local Baton dashboard without failing on missing config, auth, or remote
state.

Examples:

```sh
baton home --format toon
baton home --json
```

### `baton init`

Create or preview target-repo files.

Implemented flags:

<!-- x-release-please-start-version -->
```sh
baton init --dry-run
baton init --apply
baton init --profile default
baton init --go-install github.com/sjunepark/baton/cmd/baton@v0.4.1
baton init --install-command '<trusted install command>'
```
<!-- x-release-please-end -->

Must create or update, with explicit user confirmation:

- `.github/baton.yml`
- `.github/labels.yml`
- `.github/ISSUE_WORKFLOW.md`
- `.github/ISSUE_TEMPLATE/agent-work.yml`
- `.github/workflows/issue-policy.yml`
- `.github/workflows/pr-policy.yml`

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

- create local/published `agent` only when it exactly matches configured base;
- refuse force resets;
- warn when remote staging branch differs from base;
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
  "flow": "work",
  "errors": [],
  "warnings": [],
  "referencedIssues": [4],
  "closingIssues": [],
  "commitListingReachedCap": false
}
```

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
- review-thread count and unresolved human/bot summary counts;
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

### `baton complete`

Record local completion metadata and optionally comment on GitHub.

GitHub comments require explicit `--comment --repo owner/name --issue N|--pr N`.

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
