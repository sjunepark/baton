# Baton JSON Contracts

Common fields:

- `schemaVersion`: stable contract version.
- `kind`: result type.
- `repo`: GitHub `owner/name` when available.
- `count`, `counts`, or `summary`: precomputed totals for agent triage.
- `help`: concrete next commands or stop-condition guidance.

`nextCandidates`:

- `selectedAction`: one of `pr-followup`, `branch-health`,
  `issue-implementation`, `issue-investigation`, or `none`.
- `reason`: why Baton selected the candidate tier.
- `selectionReason`: more specific priority explanation when eligible lower-tier
  work exists but is not returned as a candidate.
- `selectionRequired`: whether multiple tied candidates require a choice.
- `candidates[]`: the highest-priority tied candidates. PR candidates include
  number, title, URL, head ref, and base ref. Issue candidates include number,
  title, and URL. Branch candidates include ref, SHA, and check state.
- `deferredEligibleItems[]`: eligible lower-priority work not returned in
  `candidates[]` for the selected tier.
- `instructions`: operational constraints to follow, including caller-provided
  checkout isolation before edits.

`queueSnapshot`:

- `counts.eligibleByAction`: eligible issue counts keyed by action.
- `issues[].eligible`: whether an issue can be started.
- `issues[].reasons`: why it is eligible or skipped.
- `issues[].linkedPrs`: active PRs already referencing that issue.
- `pullRequests[].referencedIssues`: issue references found in PR title/body.

`reviewThreads`:

- `summary.unresolved`: unresolved thread count.
- `summary.humanUnresolved`: unresolved threads with human comments.
- `threads[].isResolved`: whether the thread is resolved.
- `threads[].isOutdated`: whether GitHub marks the thread outdated.
- `threads[].comments[].authorKind`: `human`, `codex`, `coderabbit`,
  `greptile`, `bot`, or `unknown`.
- `threads[].comments[].bodyTruncated`: whether the body was bounded for
  output. Use `fullCommand` when more body context is required.
