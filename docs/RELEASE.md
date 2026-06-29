# Release Process

Release Please owns Baton releases:

- `CHANGELOG.md`
- `.release-please-manifest.json`
- release PRs and version updates
- `vX.Y.Z` git tags
- GitHub releases
- pinned Baton install target references marked with `x-release-please`

Do not manually create tags or GitHub releases during the normal flow. An
emergency manual fallback requires explicit confirmation of the exact version.

## Automation

`.github/workflows/release-please.yml` runs on pushes to `main`. It creates a
GitHub App installation token and runs `googleapis/release-please-action@v5`
with `release-please-config.json` and `.release-please-manifest.json`.

Required repo or org settings:

- variable: `RELEASE_PLEASE_APP_ID`
- secret: `RELEASE_PLEASE_PRIVATE_KEY`

Required GitHub App permissions:

- Contents: read/write
- Pull requests: read/write
- Metadata: read-only
- Issues: read/write for Release Please's default release PR labels

## Versioning

Baton is a single Go module. `go.mod` does not carry a project version; SemVer
git tags remain the install source for:

```sh
go install github.com/sjunepark/baton/cmd/baton@vX.Y.Z
```

Pre-1.0 release policy:

- `fix:` commits produce patch releases.
- `feat:` commits produce patch releases.
- breaking changes produce minor releases.

Public SemVer surface includes CLI flags, JSON output, exit codes, config
shape, install templates, generated workflows, and the module path.

## Commit Messages

- Use `fix:` for bug fixes.
- Use `feat:` for user-facing additions, including docs/tutorial features that
  should release.
- Use `docs:` for docs-only changes that should not release.
- Use `feat!:` or a `BREAKING CHANGE:` footer for breaking CLI, API, JSON,
  config, workflow, or install behavior.

Review every Release Please PR before merge. Do not merge solely because it is
generated.
