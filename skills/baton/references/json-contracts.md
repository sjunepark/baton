# Baton JSON Contracts

Common fields:

- `schemaVersion`: stable contract version.
- `kind`: result type.
- `repo`: GitHub `owner/name` when available.

`nextAction`:

- `action`: one of `pr-followup`, `branch-health`, `issue-implementation`,
  `issue-investigation`, `digest`, or `none`.
- `reason`: why Baton selected the action.
- `pr`: PR number, URL, head ref, and base ref for PR follow-up.
- `issue`: issue number and URL for issue work.
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

- `threads[].isResolved`: whether the thread is resolved.
- `threads[].isOutdated`: whether GitHub marks the thread outdated.
- `threads[].comments[].authorKind`: `human`, `codex`, `coderabbit`,
  `greptile`, `bot`, or `unknown`.
