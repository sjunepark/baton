# Architecture

Baton is a setup-free GitHub issue Task CLI. Its boundary ends at issue facts,
fixed-label transitions, and explicit issue closure. Project implementation
and delivery remain outside the system.

## Component map

```text
CLI arguments
    |
    v
internal/cli ---- repository resolver
    |
    v
internal/task service + pure planner
    |
    v
IssueStore interface
    |
    v
typed GitHub REST adapter ---- GitHub Issues API
```

- `cmd/baton/main.go` is the process entrypoint and signal boundary.
- `internal/cli/` validates all syntax before external calls, resolves the
  repository, and renders canonical Task results as text or JSON.
- `internal/repository/` resolves an explicit repository, environment
  repository, or local Git remote without reading Baton config.
- `internal/task/` owns the Task model, label vocabulary, classification,
  deterministic `next` ordering, mutation planning, and service orchestration.
- `internal/task/github_store.go` implements the narrow `IssueStore` seam over
  the typed transport in `internal/gh/`.
- `skills/baton/` supplies classification and workflow judgment without
  duplicating deterministic CLI behavior.

The system is small enough that one root architecture document is sufficient;
there are no independent runtime subsystems that need nested maps.

## Runtime flows

For a read, the CLI resolves the repository and credentials, then the Task
service asks the store for issues filtered by `baton:managed`. Classification
maps GitHub issue state and fixed labels to one Task. `next` considers only
ready open Tasks and selects deterministically by priority, then issue number;
mode does not affect ordering.

For a mutation, the service reads the issue and builds an ordered pure plan.
`--dry-run` returns its projected Task without writes. Apply lazily creates
needed fixed labels, performs label or close operations in prefix-safe order,
then re-reads the issue. Partial failures report confirmed changes and the
last confirmed Task state.

## Invariants

- `baton:managed` is the complete enrollment fact. Bodies and comments do not
  affect enrollment or classification.
- CLI commands call the Task service directly; no orchestration facade or
  generic operation-report layer sits between them.
- Every mutation has the same pure plan for dry-run and apply, preserves
  project labels, and is idempotent when already satisfied.
- Unknown arguments fail before repository, auth, or network work.
- GitHub requests are typed and bounded by deadlines; human `gh` output is
  never scraped for Task facts.
- Core commands do not read or write repository files and never run Git
  mutations or manage implementation/delivery state.
- Human text wraps the same result values used for JSON; there is no alternate
  output projection.

## Start here

Trace reads and writes from `internal/cli/cli.go` to
`internal/task/service.go`, then to `internal/task/planner.go` and the
`IssueStore` in `internal/task/model.go`. Contract expectations live in
`internal/task/task_contract_test.go` and `internal/cli/cli_test.go`.
