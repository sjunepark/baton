# Execution Context

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
- Completion metadata and optional explicit GitHub comments.

## Caller Owns

- Creating, selecting, reusing, and deleting worktrees or checkouts.
- Ensuring automation does not mutate the user's primary checkout.
- Preventing overlapping runs for the same repository or branch.
- Cleaning dirty or abandoned execution directories.

## Agent Rule

Before editing files for implementation or PR follow-up, an agent must verify it
is already in an isolated checkout supplied by the caller. If that is not true,
it should stop and report the missing execution context rather than asking
Baton to create one.
