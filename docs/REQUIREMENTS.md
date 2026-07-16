# Baton v0.7 requirements

## Product boundary

Baton manages GitHub issues explicitly enrolled as Tasks. It requires no
repository setup or config and works with an explicit repository plus GitHub
credentials. It does not prescribe project implementation or delivery.

## Task contract

- `baton:managed` is the complete enrollment fact.
- A Task exposes issue identity/state, derived Task state, one optional mode,
  one effective priority, advisory activity, blockers, project labels,
  reasons, and an optional bounded body.
- Modes are `trivial`, `bounded`, and `investigate`; priorities are `p0`
  through `p3`; blockers are `needs-info` and `needs:discussion`.
- A missing priority defaults to `p2`. Missing/conflicting mode or conflicting
  priority blocks the Task.
- Closed issues are done. Only explicit `close` intent closes an issue.
- `next` returns one deterministic ready Task or a definitive empty result.

## Mutations

- `enroll`, `update`, `unenroll`, `start`, `stop`, and `close` apply on the
  explicit verb and support uniform `--dry-run`.
- Plans are pure, prefix-safe, idempotent, and shared by preview and apply.
- Needed fixed labels may be created lazily. Arbitrary project taxonomy may
  not be created or removed.
- Bodies, comments, and project labels are preserved. Comments are never
  authoritative state.
- Partial failures expose confirmed changes and last confirmed Task state.

## CLI and integrations

- Validate syntax before repository, authentication, or network work.
- Resolve an explicit `--repo`, environment repository, or local Git remote;
  never read Baton config.
- Use a typed, bounded GitHub client. Core facts never come from scraped
  human command output.
- Return exit 0 for success/empty, 1 for operational failures, and 2 for
  invalid use. JSON errors are written to stderr.
- Core commands never write local files, mutate Git, or manage branches, pull
  requests, checks, reviews, merges, releases, or delivery.

## Agent skill

The bundled skill may suggest classification, create then explicitly enroll
issues, and provide a thin start/project-work/optional-close convenience.
All project work follows the target project's own instructions and tools.
