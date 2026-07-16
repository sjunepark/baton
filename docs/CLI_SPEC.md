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
- `6`: local git error, or a completed doctor evaluation with blocked adoption
  compatibility.

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
baton init --go-install github.com/sjunepark/baton/cmd/baton@v0.7.0
baton init --install-command '<trusted install command>'
```
<!-- x-release-please-end -->

The generated work-item transition and delivery-recorder workflows are trusted
mutation boundaries. They always install `--go-install` at an exact `@vX.Y.Z`
and ignore the arbitrary `--install-command` escape hatch used by non-mutating
policy checks.

Must create or update, with explicit user confirmation:

- `.github/baton.yml`
- `.github/labels.yml`
- `.github/ISSUE_WORKFLOW.md`
- `.github/ISSUE_TEMPLATE/agent-work.yml`
- `.github/workflows/issue-policy.yml`
- `.github/workflows/pr-policy.yml`
- `.github/workflows/work-item-transition.yml`
- `.github/workflows/delivery-recorder.yml`

### `baton doctor`

Check local and remote readiness.

Examples:

```sh
baton doctor --format toon
baton doctor --repo owner/name --format toon
baton doctor --repo owner/name --go-install example.com/baton/cmd/baton@v0.5.1 --json
baton doctor --json
```

Checks:

- config can be loaded;
- `git` is available;
- repo root can be resolved;
- discovered credentials can read the selected live GitHub repository;
- remote repository can be resolved;
- default/base/staging branch state and delivery relationship are known;
- labels and durable issue-ownership evidence are ready;
- installed files match the trusted templates at the exact default-branch SHA;
- required workflows are active and preserve trusted triggers, permissions,
  checkout ref, credential handling, and pinned Baton version;
- repository and organization Actions policy permits the generated actions;
- organization standard-hosted-runner policy is compatible, or its unavailable
  state is reported as degraded for explicit operator confirmation;
- the exact `Check PR policy` context from the host's GitHub Actions app is
  required on base and staging;
- repository merge commits are enabled and staging does not require linear
  history; squash/rebase-only rulesets are blocked;
- active merge queues on base or staging are blocked because Baton does not
  support `merge_group` events.

The `doctor` JSON contract is schema v2. Each non-OK check may include a
`remediation` string. `readyState` is `ready`, `degraded`, or `blocked`; the
command exits nonzero for blocked results.

Pass the same reviewed `--go-install` or `--install-command` value used by
`baton init` when the adopter intentionally customized its workflow install
target. Doctor then reconciles against that explicit trusted input; the two
flags are mutually exclusive.

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
  "policyCommentBody": null,
  "ownership": {
    "schemaVersion": 1,
    "kind": "managedIssueOwnership",
    "managed": true,
    "source": "recordV1",
    "diagnostics": [],
    "errors": []
  }
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
  "schemaVersion": 4,
  "kind": "prPolicyDecision",
  "flow": "promotion",
  "errors": [],
  "warnings": [],
  "referencedIssues": [],
  "closingIssues": [4],
  "commitListingReachedCap": false,
  "promotionFacts": {
    "expectedIssues": [4],
    "includedWorkPullRequests": [27],
    "excludedWorkPullRequests": [],
    "complete": true,
    "source": "sealedDeliveryPlan",
    "planDigest": "sha256:...",
    "cursorDigest": "sha256:...",
    "coverageDigest": "sha256:...",
    "baseIntegration": {
      "state": "integrated",
      "observedBaseSha": "base-sha",
      "observedStagingSha": "staging-sha",
      "reason": "recorded"
    }
  }
}
```

`flow` is `work`, `promotion`, `misroutedWork`, `indeterminate`, or
`unmanaged`. Ordinary and known-fork PRs return the unmanaged no-op decision;
reserved-prefix PRs on another target fail as misrouted, while candidate-shaped
events missing repository identity fail as indeterminate.

For a managed work PR, every configured `Refs` target must resolve to a managed
issue through its trusted ownership record or the explicitly temporary legacy
form-fingerprint reader. Current implementation, skip, and trivial labels are
not merge-policy inputs; they remain issue intake and recommendation facts.
Closing-keyword targets are rejected from PR-local text and are not fetched as
referenced resources.
After commit acquisition, Baton re-reads the pull request and requires its head
SHA to match the event revision. A concurrent push returns a structured GitHub
error and leaves the newest event to evaluate the new revision.

`promotionFacts` is present only for staging-to-base promotions. PR Policy
first appends and activates an immutable plan bound to the exact PR node ID,
base/head SHA, cursor, coverage watermark, included staged records, and reviewed
exclusions. `expectedIssues` comes only from those durable records. A complete
empty set allows a manual-only promotion without a closing keyword. The digest
fields let callers bind diagnostics to the exact seal. These fields are part of
`prPolicyDecision` schema version 4. `promotionFacts.baseIntegration` binds the
decision to observed base/staging revisions and the selected evidence; only
`integrated` may pass.

Promotion closing references are optional presentation, not completion
authority. If any configured closing references are present, their issue set
must exactly equal `promotionFacts.expectedIssues`. The post-merge transition
performs and durably commits delivery even when no closing keyword is rendered.

Missing ownership, record, cursor, coverage, seal, or bounded acquisition state
fails before policy evaluation; Baton does not fall back to ancestry or mutable
merged-PR bodies. A failed GitHub request remains a structured GitHub error with
exit code 5. Exactly one managed promotion may be open. If another opens, PR
Policy re-requests the prior successful policy check so both revisions fail
closed until the overlap is resolved.

### `baton pr-transition`

Plan or apply delivery-ledger work-item transitions. After the recorder commits
a merged work PR, Baton adds the configured `awaiting_review_label` to every
open issue in that staged-work record. After a managed promotion merges, Baton
closes the still-open issues in its exact sealed plan, removes that index,
appends the base-integration record, and commits the promotion cursor last.

```sh
baton pr-transition --event "$GITHUB_EVENT_PATH" --dry-run --json
baton pr-transition --event "$GITHUB_EVENT_PATH" --apply --json
```

Exactly one of `--dry-run` and `--apply` is required. Apply rechecks the PR
revision, plan/record digest, cursor, bounded store, issue state, and durable
ownership before the first write. Already-satisfied effects are unchanged and
partial GitHub effects remain in `operationReport`. A committed promotion
cursor makes duplicate event delivery a total no-op, so a later human reopen
is preserved. Work or promotion PR prose cannot change the delivered issue set,
and Baton never merges the PR.

### `baton delivery-record`

Record merged staging work in either shadow migration or sealed authority:

```sh
baton delivery-record --event "$GITHUB_EVENT_PATH" --dry-run --json
baton delivery-record --event "$GITHUB_EVENT_PATH" --apply --json
baton delivery-record --apply --json # reconcile missed/coalesced events
```

Apply re-reads the PR and checkpoint, backfills legacy ownership first,
appends an immutable record derived from the latest trusted pre-merge PR-policy
evidence, and commits the checkpoint last. A complete scan with no missing
managed record advances coverage to its exact observed staging head. Closed-PR
repair scans newest updates backward to the committed coverage timestamp;
retry recovery scans ledger comments backward to the last checkpoint update,
with a 1,000-comment safety ceiling. An unconfigured repository is a
successful no-op. A new or repaired record re-requests successful PR-policy
checks on open promotion heads that contain the work revision. Shadow mode
permits recording/bootstrap but keeps policy, transition, and recommendation
readers disabled; changing config to `delivery.authority: sealed` is the
reviewed cutover.
For synchronization, exact promotion recheck targets are committed in the same
checkpoint update as the integration record. A failed or ambiguous re-request
leaves that batch pending; the next recorder dispatch drains it before any new
delivery work and clears it in a separate checkpoint update.

For an exact merged base-to-staging PR, the command records synchronization
only when both pre-merge histories are ancestors of the result. It preserves
active work and the promotion cursor and rejects squash/rebase topology. The
generated workflow also runs on base pushes to recheck open promotions.

### `baton delivery-bootstrap`

Bootstrap is reviewed in two stages through the generated `Delivery Recorder`
workflow. The underlying initialization invocation is:

```sh
baton delivery-bootstrap --initialize --ledger-issue 900 --ledger-id delivery-v1 \
  --genesis-staging-sha "$STAGING_SHA" --observed-at "$OBSERVED_AT" --dry-run --repo owner/repo --json
baton delivery-bootstrap --initialize --ledger-issue 900 --ledger-id delivery-v1 \
  --genesis-staging-sha "$STAGING_SHA" --observed-at "$OBSERVED_AT" --apply --plan-id "$PLAN_ID" --repo owner/repo --json
```

When config already pins a delivery locator, the same `--initialize` shape is
a reviewed rollover into a different locked issue. The ledger ID and committed
staging coverage must match, the predecessor active window and pending recheck
batch must be empty, and apply creates or adopts the successor checkpoint before
freezing the predecessor. Repin the returned successor locator through normal
review before routine recording resumes.

After the locator is reviewed, preview historical migration with either
`--genesis-promotion` or `--genesis-staging-sha`. Output shows every source
fact, inferred issue/PR relationship, ambiguity, exact ownership/staged record,
and stable `planId`. Apply requires that exact ID and refuses changed facts or
unresolved ambiguity. If the reviewed promotion corrects the initialized base
boundary, apply commits that exact genesis checkpoint before ownership and
staged-record writes. Direct local invocation is rejected because persisted
comments must have trusted GitHub Actions authorship and share recorder
concurrency. Configure required reviewers on the workflow's
`baton-delivery-bootstrap` environment. See
[Delivery bootstrap](DELIVERY_BOOTSTRAP.md).

### `baton snapshot`

Acquire repository facts once and return `repositorySnapshot` schema v2 with
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

`baseIntegration.state` is `integrated`, `direct-base-work-pending`,
`diverged`, or `unknown`. Pending direct-base work selects `sync_staging`
before PR or issue work and instructs a maintainer to open and merge a normal
reviewed base-to-staging PR with a merge commit. Baton never performs it.

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

Return a precomputed `pullRequest` v2 dashboard for one PR.

Includes:

- branch/base/head;
- referenced issue numbers from current PR-local text for open work, or the
  covered staged-work record for merged managed work;
- issue label readiness when Baton config, durable ownership, and open issue
  data are available;
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

`nextCandidates` v3 is a lossy compatibility projection from the unified
snapshot implementation. It cannot express the new Outcome distinction and
must not be interpreted as execution state. New integrations should consume
`baton snapshot --json`.

JSON result:

```json
{
  "schemaVersion": 3,
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
