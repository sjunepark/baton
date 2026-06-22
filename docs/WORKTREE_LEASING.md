# Worktree Leasing

## Purpose

Codex automations can overlap. If two automations share one checkout, branch
switching and git mutations can corrupt each other's work or disrupt the user's
manual session. Baton must make this impossible by construction.

The invariant:

```text
Automation work happens only in a Baton-managed lease path.
```

## Lease Record

Each lease should have a record stored under Baton state, for example:

```json
{
  "schemaVersion": 1,
  "id": "20260622T103000Z-pr-8",
  "repo": "example-org/example-repo",
  "sourceRepoPath": "/Users/example/projects/example-repo",
  "worktreePath": "/Users/example/.baton/worktrees/example-repo/lease-abc123/example-repo",
  "purpose": "pr-followup",
  "baseRef": "agent",
  "headRef": "agent-work/github-agent-branch-policy",
  "owner": {
    "pid": 12345,
    "hostname": "local"
  },
  "createdAt": "2026-06-22T10:30:00Z",
  "expiresAt": "2026-06-22T18:30:00Z",
  "status": "active"
}
```

## Native Backend

The v1 native backend should use plain `git worktree`.

Acquire existing branch:

```text
git fetch origin
git worktree add <lease-path> <head-ref>
```

Acquire new branch from base:

```text
git fetch origin
git worktree add -b <new-branch> <lease-path> <base-ref>
```

Safety checks before acquisition:

- source repo is a git repo;
- source repo has no active Baton lease for the same branch unless sharing is
  explicitly allowed later;
- target lease path does not exist or is known safe;
- branch name matches configured work branch prefix for implementation work;
- base ref exists locally or remotely;
- no command will mutate the caller's current branch.

## Optional Treehouse Backend

Treehouse is relevant because it manages reusable isolated worktrees, detects
in-use worktrees, treats dirty worktrees as unavailable, and preserves caches.

Baton should not depend on Treehouse in v1 unless the integration is trivial.
Design the backend interface so Treehouse can be added later:

```text
LeaseBackend.Acquire(request) -> Lease
LeaseBackend.Release(lease) -> ReleaseResult
LeaseBackend.Status() -> PoolStatus
LeaseBackend.Prune(planOnly) -> PrunePlan
```

## Release Rules

Default release behavior:

- If worktree is clean, mark lease released.
- If worktree is dirty, refuse release and print changed files.
- If branch was pushed and clean, keep or remove worktree according to config.
- Never delete untracked files unless the worktree is Baton-managed and the user
  passes an explicit cleanup confirmation.

Supported future options:

- `--keep-dirty`: mark lease as retained for manual follow-up.
- `--force-clean`: only for managed leases, requires `--yes`.
- `--prune`: dry-run by default.

## Stale Leases

Baton should detect stale leases when:

- recorded PID is gone;
- lease expired;
- worktree has no active processes;
- worktree is clean or already merged.

Stale lease cleanup must be conservative:

- clean stale leases may be released automatically with explicit command.
- dirty stale leases are reported, not cleaned.
- unknown or corrupted records are reported, not deleted.

## Branch Collision Rules

- Only one active lease may own a specific branch by default.
- Multiple read-only leases may be allowed later, but v1 should avoid it.
- A PR follow-up lease should use the existing PR branch.
- Issue implementation lease should create one configured work branch.
- Investigation-only leases should use detached or temporary branches and should
  not push without explicit user intent.

## Commands

Required:

```sh
baton lease --purpose <purpose> --json
baton release --lease <id>
baton leases --json
baton prune --dry-run
```

Useful later:

```sh
baton lease shell --lease <id>
baton lease exec --lease <id> -- <command>
```

The first implementation can keep execution outside Baton and simply return the
lease path to Codex.
