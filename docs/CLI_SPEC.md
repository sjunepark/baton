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

depending on whether they apply a computed plan or confirm destructive cleanup.
Lease acquire/release commands are explicit state transitions and must still
return structured JSON for automation.

## Exit Codes

- `0`: success, including clean no-op.
- `1`: policy failure or unsafe state.
- `2`: invalid CLI usage.
- `3`: config error.
- `4`: auth error.
- `5`: GitHub API error.
- `6`: local git/worktree error.

## Core Commands

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

```sh
baton init --dry-run
baton init --apply
baton init --profile default
baton init --go-install github.com/sjunepark/baton/cmd/baton@v0.1.3
baton init --install-command '<trusted install command>'
```

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
- labels are present or diffable;
- worktree root is writable.

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
  "labelsToRemove": ["agent:blocked"],
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
  "errors": [],
  "warnings": [],
  "referencedIssues": [4],
  "closingIssues": [],
  "commitListingReachedCap": false
}
```

### `baton queue`

Return open issues and why each is eligible or skipped.

Example:

```sh
baton queue --format toon
baton queue --json
```

### `baton prs`

Return open agent PRs with checks and review state summary.

Example:

```sh
baton prs --format toon
baton prs --json
```

### `baton pr <number>`

Return full state for one PR.

Must include:

- branch/base/head;
- linked issues;
- labels;
- checks;
- review decision;
- unresolved review threads;
- latest bot summaries when available.

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
baton checks 12 --json
```

### `baton next`

Return one recommended next unit of work.

Example:

```sh
baton next --format toon
baton next --json
```

JSON result:

```json
{
  "schemaVersion": 1,
  "kind": "nextAction",
  "action": "pr-followup",
  "repo": "example-org/example-repo",
  "reason": "failing-checks",
  "pr": {
    "number": 8,
    "url": "https://github.com/example-org/example-repo/pull/8",
    "headRef": "agent-work/github-agent-branch-policy",
    "baseRef": "agent"
  },
  "issue": {
    "number": 7,
    "url": "https://github.com/example-org/example-repo/issues/7"
  },
  "blockedItems": [],
  "instructions": [
    "Acquire a lease before editing.",
    "Push to the existing PR branch.",
    "Do not open a new PR."
  ]
}
```

Allowed `action` values:

- `pr-followup`
- `branch-health`
- `issue-implementation`
- `issue-investigation`
- `digest`
- `none`

### `baton lease`

Acquire an isolated worktree.

Examples:

```sh
baton lease --purpose pr-followup --branch agent-work/foo --json
baton lease --purpose issue-4 --base origin/agent --new-branch agent-work/4-title --json
```

JSON result:

```json
{
  "schemaVersion": 1,
  "kind": "lease",
  "id": "20260622T103000Z-pr-8",
  "path": "/Users/example/.baton/worktrees/example-repo/lease-abc123/example-repo",
  "repo": "example-org/example-repo",
  "headRef": "agent-work/foo",
  "baseRef": "agent",
  "expiresAt": "2026-06-22T18:30:00Z"
}
```

### `baton release`

Release a lease.

Examples:

```sh
baton release --lease 20260622T103000Z-pr-8
baton release --path /path/to/worktree
```

Default behavior must refuse dirty releases unless `--keep-dirty` or a later
explicit cleanup command is used.

### `baton complete`

Record local completion metadata and optionally comment on GitHub.

GitHub comments require explicit `--comment --repo owner/name --issue N|--pr N`.

## Human Output

Text output should be concise and actionable:

```text
Next action: PR #8 follow-up
Reason: Typecheck failing
Branch: agent-work/github-agent-branch-policy
Lease required: yes
Stop conditions: unresolved human review, ambiguous scope, dirty lease conflict
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
