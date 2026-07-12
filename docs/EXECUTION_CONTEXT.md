# Execution Context

## Repository identity

A policy-backed Baton repository observation (`queue`, `prs`, or `next`) binds
one canonical local root, one config source path, the configured git remote and
URL, one GitHub `owner/name`, and the config's branch policy. If `--repo` or
`GITHUB_REPOSITORY` disagrees with the configured checkout remote, resolution
fails before authentication or GitHub requests. The remote host must match the
default GitHub.com API or the configured GitHub Enterprise API host. Callers
such as Coda should run Baton with the Project checkout as the working directory
only when that checkout's configured remote identifies the Project metadata
repository passed as `--repo`. Agreement is a safety precondition;
cross-repository checkout/metadata invocation is unsupported and must stop with
the structured config error before authentication or GitHub I/O.

For compatibility, an invocation outside a git checkout may still use an
explicit repository and config path. In that mode Baton has no local remote to
assert; the context records that absence rather than claiming remote agreement.

PR detail, check, and review-thread workflows use the same identity checks. If
policy is available they reuse its configured remote and load it once; fact-only
commands may resolve the checkout target through `origin` without requiring a
Baton config. Policy-event, label-sync, and work-item transition workflows keep
their established explicit/event/environment target precedence and validate
the resulting `owner/name` before GitHub I/O.

CLI handlers only parse explicit inputs and render results. Workflow or focused
domain modules own config discovery, target resolution, clocks, external fetch
sequencing, result composition, and mutation ordering.

Baton does not manage worktrees.

Automation isolation is the caller's responsibility. A Coda job, Treehouse
checkout, Codex background checkout, or manually prepared git worktree should
provide the directory where edits happen before Baton-guided implementation
starts.

## Baton Owns

- GitHub issue and PR policy decisions.
- Queue and next-candidate classification.
- Branch and ref facts needed to choose one unit of work.
- Install, label, and staging-branch planners with explicit apply gates.
- Work-item state transitions and their explicit operation reports.

## Caller Owns

- Creating, selecting, reusing, and deleting worktrees or checkouts.
- Ensuring automation does not mutate the user's primary checkout.
- Preventing overlapping runs for the same repository or branch.
- Cleaning dirty or abandoned execution directories.
- Recording execution completion, provenance, retries, and validation evidence.

## Agent Rule

Before editing files for implementation or PR follow-up, an agent must verify it
is already in an isolated checkout supplied by the caller. If that is not true,
it should stop and report the missing execution context rather than asking
Baton to create one.
