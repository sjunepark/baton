# Baton JSON Contracts

Common fields:

- `schemaVersion`: stable contract version.
- `kind`: result type.
- `repo`: GitHub `owner/name` when available.
- `count`, `counts`, or `summary`: precomputed totals for agent triage.
- `help`: concrete next commands or stop-condition guidance.

`repositorySnapshot`:

- `repository`: validated GitHub `owner/name` identity.
- `acquisition.startedAt` / `completedAt`: the bounded observation window.
- `completeness`: `complete` or `degraded`; degraded Recommendations never
  include an Action.
- `warnings[]`: scoped partial, stale, unknown, or upstream failure evidence.
- `baseIntegration`: observed and acknowledged base/staging revisions plus
  `integrated`, `direct-base-work-pending`, `diverged`, or `unknown`.
- `queue`, `branches[]`, and `pullRequests[]`: facts from the same acquisition.
- `recommendation.outcome`: `actionable`, `human_choice_required`, `waiting`,
  `blocked`, `idle`, or `degraded`.
- `recommendation.action`: optional typed repository work, never execution
  state or authority.
- `recommendation.candidates[].identity`: repository-scoped issue, PR revision,
  or branch revision identity.

New integrations should use `repositorySnapshot` v2. `nextCandidates` v3 and
`queueSnapshot` v2 remain migration projections.

`nextCandidates`:

- `selectedAction`: one of `sync-staging`, `pr-followup`, `branch-health`,
  `issue-implementation`, `issue-investigation`, or `none`.
- `reason`: why Baton selected the candidate tier.
- `selectionReason`: more specific priority explanation when eligible lower-tier
  work exists but is not returned as a candidate.
- `selectionRequired`: whether multiple tied candidates require a choice.
- `candidates[]`: the highest-priority tied candidates. PR candidates include
  number, title, URL, head ref, and base ref. Issue candidates include number,
  title, URL, and optional `priorityLabel`. Branch candidates include ref, SHA,
  and check state.
- `deferredEligibleItems[]`: eligible lower-priority work not returned in
  `candidates[]` for the selected tier.
- `instructions`: operational constraints to follow, including caller-provided
  checkout isolation before edits.

`queueSnapshot`:

- `counts.eligibleByAction`: eligible issue counts keyed by action.
- `issues[].eligible`: whether an issue can be started.
- `issues[].priorityLabel`: the configured priority label Baton selected from
  issue labels, when priority is enabled and present.
- `issues[].priorityRank`: the configured priority order used by `baton next`,
  when priority is enabled and present.
- `issues[].reasons`: why it is eligible or skipped.
- `issues[].linkedPrs`: active PRs already referencing that issue.
- `pullRequests[].referencedIssues`: issue references found in PR title/body.

`reviewThreads`:

- `summary.unresolved`: unresolved thread count.
- `summary.humanUnresolved`: unresolved threads with human comments.
- `summary.unknownUnresolved`: unresolved threads whose actor type is not
  available or recognized.
- `threads[].isResolved`: whether the thread is resolved.
- `threads[].isOutdated`: whether GitHub marks the thread outdated.
- `threads[].comments[].authorKind`: `human`, `codex`, `coderabbit`,
  `greptile`, `bot`, or `unknown`.
- `threads[].comments[].authorType`: GitHub's neutral GraphQL actor type when
  available; bot-like substrings in a human login do not change its kind.
- `threads[].comments[].bodyTruncated`: whether the body was bounded for
  output. Use `fullCommand` when more body context is required.

`workItemTransitionPlan`:

- `schemaVersion`: `4`.
- `eventAction`, `pullRequestNumber`, and `flow`: the verified PR lifecycle
  input and repository-policy flow.
- `operations[]`: deterministic issue-label writes for merged staging work, or
  ordered issue close/index removal, base-integration append, and cursor commit
  for a merged promotion.
- `deliveryRecordDigest`: the immutable staged-work record that supplied the
  issue set.
- `promotionPlanDigest`, `promotionCursorDigest`, and
  `baseIntegrationDigest`: exact promotion commitment identities. A committed
  duplicate has no issue operations.
- `warnings[]`: non-mutating edge conditions such as a merged work PR without
  configured issue references.
- `report`: present after apply, with per-operation `applied`, `unchanged`,
  `refused`, `failed`, or `not_attempted` outcomes.

`deliveryRecordPlan` v2:

- `complete` and `applicable`: both must be true before mutation.
- `append`: exact staged-work record, checkpoint precondition, or committed
  retry match.
- `coverage`: exact checkpoint-only coverage advance after a complete scan
  finds no missing managed record.
- `ownershipBackfill[]`: durable ownership writes that precede the record.
- `promotionRechecks[]`: exact head/check-run identities invalidated after a
  new or repaired record, including the status/conclusion observed during
  planning. Apply reacquires the rollup after checkpoint commit.
- `report`: preserves partial ownership, append, checkpoint, and recheck effects.
- `synchronization`: exact reviewed base-to-staging PR and ancestry-preserving
  result; apply records evidence but never creates or merges the PR.

`deliveryBootstrapPlan` v1 exposes a stable `planId`, every source fact and
relationship, explicit ambiguities, structured open-promotion shadow
comparisons, the reviewed genesis boundary, and exact ownership/staged records.
Apply requires the same `planId`. Initialization
returns the checkpoint body and exact locator for review into config. Record
append and checkpoint update operations are reported independently.

`prPolicyDecision` v4 and transition v4 use one ownership vocabulary:
`work`, `promotion`, `misroutedWork`, `indeterminate`, or `unmanaged`.
Unmanaged is a successful no-op; misrouted and indeterminate candidate shapes
fail or plan no writes.
For `work`, referenced issue eligibility comes from durable ownership, not
current implementation or skip labels. Queue and snapshot contracts retain
label-derived intake eligibility.
For `promotion`, `promotionFacts.baseIntegration.state` must be `integrated`.
Issue close/index operations precede the base-integration
append and cursor commit. Exact plan, integration, and cursor digests bind the
plan; a committed duplicate has no operations.
