# AGENTS.md

## Product boundary

Baton is a setup-free Go CLI for GitHub issues explicitly enrolled as Tasks.
Keep deterministic Task facts and transitions in Go and semantic
classification suggestions in the bundled skill. Project implementation and
delivery behavior belongs to each target project's instructions and tools.

Do not add repository config, setup/readiness commands, managed templates,
policy workflows or comments, branches, pull requests, CI/review/merge logic,
delivery state, candidate/recommendation/run concepts, or legacy output
projections to the active product.

## Implementation

- Keep one canonical Task model and have commands call the Task service
  directly.
- Use the typed GitHub client for issue facts; do not scrape human `gh` output.
- Keep commands JSON-first while deriving concise text from the same results.
- Validate arguments before repository, authentication, or network work.
- Model errors explicitly and preserve confirmed state on partial failures.
- Every mutation must have the same pure plan for `--dry-run` and apply,
  prefix-safe ordering, explicit intent, and idempotent no-op behavior.
- Core Task commands must not write local files or mutate Git state.

## Adopter decommission safety

The separate v0.7 adopter guide may describe reviewed removal of old v0.5 or
v0.6 repository coupling. For that work only:

- inventory first and treat unknown settings or unmatched files as unresolved;
- remove retired required checks before their producer workflows;
- preview file changes in an explicitly selected non-primary checkout and use
  normal project review;
- preserve branches, issues, pull requests, comments, labels, ledgers,
  environments, worktrees, local artifacts, and customized files;
- never merge without an explicit request and target-project permission.

Do not turn the decommission guide or fixtures into CLI, Task-package, or
repository-script compatibility behavior.

## Release management

Release Please owns `CHANGELOG.md`, `.release-please-manifest.json`, release
pull requests, version tags, GitHub releases, and marked version references.
Use intentional Conventional Commits: `fix:` for fixes, `feat:` for additive
public behavior, and `feat!:` or `BREAKING CHANGE:` for incompatible public
behavior. Review generated release changes before merge. Do not push, invoke
release automation, publish, tag, or merge without explicit authorization.

## Validation

After coherent changes run:

```sh
gofmt -w <changed-go-files>
go vet ./...
go test ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
go mod tidy -diff
```

Add table-driven tests for Task classification and planner decisions, request
boundary tests for the GitHub adapter, and CLI tests for help, JSON/text,
empty states, mutations, and invalid syntax. Live GitHub tests remain behind
explicit environment gates.

Keep the v0.7 plan and active docs current. After a reviewable implementation
slice, run the repository-required code review and apply safe findings.
