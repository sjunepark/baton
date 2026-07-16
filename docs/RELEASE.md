# Release management

Release Please owns `CHANGELOG.md`, `.release-please-manifest.json`, version
updates in marked files, `vX.Y.Z` tags, release pull requests, and GitHub
releases. Do not edit or create those outputs manually during normal release
preparation.

Baton's public SemVer surface includes commands and flags, JSON/text results,
exit codes, fixed labels and their meaning, repository resolution, the module
path, and the bundled skill. Breaking changes use `feat!:` or a
`BREAKING CHANGE:` footer. Under the configured pre-1.0 policy, the v0.7 Task
reset is a minor-version release.

Before any release automation, run the documented CI checks, validate the
skill and adopter note, and verify every Release Please extra-file target
exists and contains its marker. Review the generated release PR before merge.
Pushing, invoking release automation, publishing, tagging, and merging require
separate explicit authorization.
