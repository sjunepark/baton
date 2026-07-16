# Baton v0.6 decommission evidence

This directory preserves only the immutable evidence needed to review a manual
v0.7 adopter decommission. It is not loaded by the Task CLI and does not define
a compatibility mode.

- `managed-files.json` contains the exact default v0.6.0 rendered file bytes as
  base64 plus their SHA-256 fingerprints. A removal guide may use those facts
  to distinguish an unmodified Baton file from project customization.
- `adopter-inventories.json` contains representative read-only inventory facts
  for unmodified, modified, partial, and already-removed adopters. Unknown
  settings remain explicit; preserved labels, issues, ledgers, and environments
  are evidence, not cleanup targets.

The source templates, installer, doctor, policy engine, and orchestration tests
must not be retained merely to interpret these fixtures.
