# Baton Adopter Updates

Per-release notes for repositories that have adopted Baton.

Use these with:

- `.github/baton.yml` `setup.baseline_baton_version`, the last Baton release
  the repo's setup files were reviewed against;
- the Baton install pin in `.github/workflows/*`, the runtime version used by
  GitHub Actions;
- `.github/baton.yml` `version`, the config schema version;
- `CHANGELOG.md` for broader release context.

Available update notes:

- [v0.6.0](v0.6.0.md): explicit ownership, sealed delivery, synchronization,
  live adoption compatibility, and migration steps.
- [v0.5.0](v0.5.0.md): repository orchestration and completion migration.
