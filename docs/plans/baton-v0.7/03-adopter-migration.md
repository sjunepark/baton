# v0.7 adopter decommission and migration

## Purpose

Remove repository coupling left by v0.5.0, v0.5.1, or v0.6.0 without treating
old orchestration as an ongoing v0.7 mode. Shipping a reduced binary is
insufficient because adopter workflows install pinned releases and required
check settings can continue to block ordinary pull requests.

## Current state

- M4 is complete as repository guidance and inert evidence. No adopter
  repository or GitHub setting was changed while preparing it.
- Exact v0.5.0, v0.5.1, and v0.6.0 renderer bytes and SHA-256 manifests are
  preserved under `testdata/migration/` and were verified against binaries
  built from each exact tag.
- Version-specific and cross-version inventories cover unmodified, modified,
  partial, already-removed, mixed, customized, older, and unknown sources.
- The v0.7 adopter note requires read-only inventory, full ruleset and branch
  rule inspection, required-check-first removal, reviewed file diffs, and
  explicit enrollment that keeps known-blocked issues blocked at every prefix.

## Supported source installations

- The v0.7 guide supports direct decommissioning from the released v0.5.0,
  v0.5.1, and v0.6.0 default installation surfaces. It must not require an
  adopter to run an old Baton release or upgrade through v0.6 first.
- Keep an exact evidence profile for each source release. Shared bytes may be
  deduplicated, but every source release must have a complete path-to-hash
  manifest so a reviewer can prove ownership without inference.
- Treat contradictory pins, mixed-version files, custom install commands,
  unmatched bytes, older releases, and unknown installations conservatively:
  inventory and report them, preserve uncertain resources, and require manual
  review. Do not choose the nearest known release.
- These profiles support a one-time reviewed migration. They do not add old
  config decoding, migration commands, or compatibility behavior to v0.7.

## Migration outcomes

- Existing repositories stop running Baton on PR, branch, promotion, delivery,
  and issue-edit events. v0.7 installs no replacement policy workflow.
- Removing Baton leaves ordinary GitHub issues, understandable labels, and
  project-owned development practices.
- Any existing `baton:managed` label enrolls its issue directly in v0.7;
  hidden v0.6 ownership comments are inert historical evidence.
- v0.5-era issues without `baton:managed` remain unenrolled until a reviewer
  explicitly selects them for `baton enroll`. Fixed eligibility, priority, and
  blocker labels may help find and classify those issues, but neither labels
  nor the old body fingerprint may imply enrollment.
- No automated migration deletes branches, issues, comments, labels, ledger
  data, environments, worktrees, or customized repository files.
- Historical local completion artifacts remain inert and untouched.
- Baton does not inventory or adapt downstream orchestrators as part of
  adopter migration; removed public contracts are documented only as breaking
  Baton changes.

## Required decommission order

1. Inventory the exact default-branch files, workflow states, rulesets, branch
   protections, required checks, labels, delivery resources, environments, and
   every configured Baton pin. Classify the source as v0.5.0, v0.5.1, v0.6.0,
   mixed, customized, or unknown without guessing.
2. Remove `Check PR policy` and other retired Baton contexts from required
   checks before deleting or disabling the workflows that produce them.
3. Submit a normal reviewed repository change removing recognizable,
   unmodified Baton PR-policy, transition, delivery-recorder, and old issue
   policy workflows.
4. Remove obsolete config, label manifest, generated guidance, and issue
   template only when exact-content evidence proves Baton ownership. Preserve
   customized files and explain manual choices.
5. For a v0.5 source, inventory issues carrying fixed eligibility labels but
   lacking `baton:managed`. Run `baton enroll ISSUE --dry-run`, then run the
   command without `--dry-run` only for the exact issue numbers a reviewer
   explicitly approves; preserve bodies and existing project labels. A valid
   outcome may leave every old issue unenrolled.
6. If the project wants the documentation's optional issue-template example,
   it adopts that project-owned file through its normal review process; Baton
   does not install or track it.
7. Rerun a read-only audit proving no retired workflow or required check can
   affect ordinary PRs, reporting every unresolved migration action and the
   disposition of the discovered v0.5-era issue set, and confirming that v0.7
   lists exactly the explicitly enrolled Tasks.

## Migration guide and evidence

- [x] Keep v0.5/v0.6 decommissioning out of the shipped Task CLI,
  `internal/task`, and repository scripts. v0.7 provides a reviewed guide and
  exact migration fixtures, not a compatibility command or helper program.
- [x] Preserve exact v0.5.0 and v0.5.1 default rendered files and SHA-256
  fingerprints beside the existing v0.6.0 evidence. Record a complete
  path-to-hash manifest for each version and verify it against a fresh renderer
  built from that exact release tag.
- [x] Add unmodified, modified, partial, already-removed, mixed-version, and
  unknown-version read-only inventories. Preserve any file whose exact source
  cannot be proven.
- [x] Document read-only `gh api` inventory examples, ordered manual changes,
  warnings, and explicit unknowns. Never report completion when required-check
  state is unknown.
- [x] Document read-only discovery of v0.5-era issues without
  `baton:managed`, followed by per-issue `baton enroll ISSUE --dry-run` and
  explicit approval before running the mutation. Do not use body parsing or old
  labels as enrollment authority.
- [x] Preview every file removal as a normal reviewed diff and run it only in
  an explicitly selected non-primary checkout.
- [x] Treat settings changes that lack safe API support as named manual steps,
  not shell snippets applied optimistically.
- [x] If repeated real migrations later demonstrate a need for automation,
  evaluate that as a separate goal rather than pre-building it into v0.7.

## Resource handling

### Remove or disable through review

- `.github/workflows/pr-policy.yml`
- `.github/workflows/work-item-transition.yml`
- `.github/workflows/delivery-recorder.yml` when present in v0.6
- the v0.5 body-policy or v0.6 ownership-policy workflow
- retired required-check/ruleset entries
- obsolete `.github/baton.yml` orchestration fields or the whole file when
  uncustomized and unnecessary
- obsolete `.github/ISSUE_WORKFLOW.md`, generated issue template, and label
  manifest when exact-managed

### Preserve by default

- `agent`, `dev`, staging, base, feature, and `agent-work/*` branches
- open and closed issues, PRs, comments, and check history
- v0.5 issue bodies and v0.6 ownership comments
- locked delivery-ledger issues and their comments
- `baton-delivery-bootstrap` environments and protection settings
- existing labels, including generic project and agent labels
- modified issue templates, workflow files, or guidance
- local completion artifacts

### Report for human review

- open issues carrying `needs:review` or delivery-specific labels
- v0.5-era issues carrying an eligibility label but no `baton:managed`
- existing `baton:managed` issues missing a v0.7 eligibility label
- contradictory eligibility or priority labels
- required checks whose source cannot be proven
- mixed, contradictory, customized, or unknown installation evidence
- workflow pins that would continue installing v0.5 or v0.6

## Release migration document

Create `docs/adopter-updates/v0.7.0.md` with:

- the product boundary change and removed public surface;
- supported direct starting points v0.5.0, v0.5.1, and v0.6.0, plus the
  conservative path for mixed, customized, older, or unknown installations;
- the exact safe decommission order;
- how existing `baton:managed` labels carry forward and how v0.5-era issues
  lacking that label require explicit reviewed enrollment;
- the absence of a replacement policy workflow and the optional project-owned
  issue-template guidance;
- removed legacy JSON contracts, without downstream-specific adaptation
  guidance;
- dry-run/audit commands and expected success criteria;
- explicit statements that branches, ledgers, issues, comments, labels, and
  environments are not automatically deleted and local artifacts are not
  touched.

## Validation

- [x] Verify each v0.5.0, v0.5.1, and v0.6.0 manifest against exact-tag
  renderer output, then review the guide against unmodified, modified,
  partially installed, and already removed fixtures for every source release.
- [x] Review mixed-version, customized, older, and unknown scenarios; every
  unmatched resource must remain preserved with a named manual action.
- [x] Verify required-check-first ordering and explicit unknown-settings
  handling in every documented scenario.
- [x] Verify no instruction deletes branches, issues, comments, labels,
  environments, worktrees, or local artifacts.
- [x] Verify every audit command is read-only and safely repeatable.
- [x] Test v0.7 list behavior against migrated v0.6 issue labels without
  reading ownership comments.
- [x] Test that v0.5 labels and bodies do not enroll an issue, then verify an
  explicitly approved `enroll` preserves its body and existing labels and
  makes exactly that issue visible as a Task with classification derived from
  the preserved labels.

## Completion criteria

- A reviewed adopter can move directly from v0.5.0, v0.5.1, or v0.6.0 to
  v0.7 without breaking ordinary PRs, passing through another old release, or
  deleting historical/project-owned resources.
- A read-only audit can prove the retired workflow/check surface is absent or
  name every unresolved manual action.
- No v0.5/v0.6 compatibility path remains in normal Baton task execution.
- No shipped migration code, fixture, or document makes Baton responsible for
  a downstream orchestrator or adds compatibility concepts to Task code.
