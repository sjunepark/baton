# Baton JSON Contracts

Common fields:

- `schemaVersion`: stable contract version.
- `kind`: result type.
- `repo`: GitHub `owner/name` when available.
- `count`, `counts`, or `summary`: precomputed totals for agent triage.
- `help`: concrete next commands or stop-condition guidance.

`nextCandidates`:

- `action`: one of `pr-followup`, `branch-health`, `issue-implementation`,
  `issue-investigation`, or `none`.
- `reason`: why Baton selected the candidate tier.
- `selectionRequired`: whether multiple tied candidates require a choice.
- `candidates[]`: the highest-priority tied candidates. PR candidates include
  number, title, URL, head ref, and base ref. Issue candidates include number,
  title, and URL. Branch candidates include ref, SHA, and check state.
- `instructions`: operational constraints to follow.

`lease`:

- `id`: lease identifier used by `baton release`.
- `path`: only directory where automation edits may happen.
- `headRef` and `baseRef`: branch context.
- `expiresAt`: stale lease threshold.

`queueSnapshot`:

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
