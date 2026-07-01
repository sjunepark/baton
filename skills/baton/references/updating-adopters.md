# Updating Baton Adopters

Use this reference when asked to update or review a repository that already
uses Baton.

Skill command: `$baton update [repo]`.

## Version Markers

- `.github/baton.yml` `setup.baseline_baton_version`: the Baton release the
  repository setup files were last reviewed or applied against.
- `.github/workflows/*` Baton install pin: the runtime version GitHub Actions
  installs.
- `.github/baton.yml` `version`: the config schema version.

Do not treat the setup baseline as a compatibility guarantee.

## Update Flow

Read the per-release files under `docs/adopter-updates/` for the Baton versions
being considered. Read `CHANGELOG.md` only when broader release context is
needed.

Run dry-run or read-only checks before recommending changes:

```sh
baton init --dry-run --json
baton migrate-config --dry-run
baton sync-labels --dry-run --repo owner/name --json
baton ensure-branch --json
baton doctor --format toon
```

Open or recommend a normal reviewed PR for any updates. Do not apply workflow,
config, label, or branch changes without explicit approval.

When the repository setup has been reviewed or updated for a Baton release,
update `setup.baseline_baton_version` to that release in the same PR.
