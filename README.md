# Baton

Baton is a small Go CLI for managing GitHub issues as explicit Tasks. An issue
is a Baton Task exactly when it has the `baton:managed` label. Baton can list,
inspect, classify, select, start, stop, unenroll, and explicitly close Tasks.
It does not manage project setup, branches, pull requests, CI, reviews,
merges, releases, or delivery.

## Install

<!-- x-release-please-start-version -->
```sh
go install github.com/sjunepark/baton/cmd/baton@v0.7.2
```
<!-- x-release-please-end -->

Baton uses `GITHUB_TOKEN`, `GH_TOKEN`, or existing `gh` authentication. Pass
`--repo owner/name` from any directory, or run inside a Git checkout whose
remote identifies the repository. No repository config or setup command is
required.

## Quick start

```sh
baton --repo owner/name enroll 42 --mode bounded --priority p2 --dry-run
baton --repo owner/name enroll 42 --mode bounded --priority p2
baton --repo owner/name list
baton --repo owner/name next
baton --repo owner/name start 42
baton --repo owner/name stop 42
baton --repo owner/name close 42
```

Mutation verbs apply directly and all support `--dry-run`. `start` adds only
the advisory `baton:in-progress` label. `close` is the only Baton operation
that closes an issue; no pull request, commit, check, or merge implies done.
Use `--json` for the canonical machine-readable result.

## Classification

Baton lazily creates only the fixed labels needed by an explicit mutation:
modes `trivial`, `bounded`, and `investigate`; priorities `p0` through `p3`;
and blockers `needs-info` and `needs:discussion`. Existing project labels and
issue bodies are preserved. Comments are optional human context and never
authoritative state.

Projects may provide their own issue form or template, but Baton does not
install or track one. A useful template asks for the outcome, evidence,
constraints, and acceptance criteria; enrollment still requires the explicit
`baton enroll ISSUE` command.

## Documentation

- [Architecture](ARCHITECTURE.md)
- [CLI reference](docs/CLI_SPEC.md)
- [Task output](docs/OUTPUT_SPEC.md)
- [Skill behavior](docs/SKILL_SPEC.md)
- [v0.7 adopter decommission](docs/adopter-updates/v0.7.0.md)
