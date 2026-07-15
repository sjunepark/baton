# v0.7 adopter decommission and migration

## Purpose

Remove v0.6 repository coupling without treating old orchestration as an
ongoing v0.7 mode. Shipping a reduced binary is insufficient because adopter
workflows install pinned releases and required check settings can continue to
block ordinary pull requests.

## Migration outcomes

- Existing repositories stop running Baton on PR, branch, promotion, delivery,
  and issue-edit events. v0.7 installs no replacement policy workflow.
- Removing Baton leaves ordinary GitHub issues, understandable labels, and
  project-owned development practices.
- Existing `baton:managed` labels enroll issues directly in v0.7; hidden v0.6
  ownership comments are inert historical evidence.
- No automated migration deletes branches, issues, comments, labels, ledger
  data, environments, worktrees, or customized repository files.
- Baton does not inventory or adapt downstream orchestrators as part of
  adopter migration; removed public contracts are documented only as breaking
  Baton changes.

## Required decommission order

1. Inventory the exact default-branch files, workflow states, rulesets, branch
   protections, required checks, labels, delivery resources, environments, and
   configured Baton version.
2. Remove `Check PR policy` and other retired Baton contexts from required
   checks before deleting or disabling the workflows that produce them.
3. Submit a normal reviewed repository change removing recognizable,
   unmodified Baton PR-policy, transition, delivery-recorder, and old issue
   policy workflows.
4. Remove obsolete config, label manifest, generated guidance, and issue
   template only when exact-content evidence proves Baton ownership. Preserve
   customized files and explain manual choices.
5. If the project wants the documentation's optional issue-template example,
   it adopts that project-owned file through its normal review process; Baton
   does not install or track it.
6. Rerun a read-only audit proving no retired workflow or required check can
   affect ordinary PRs and that v0.7 can list enrolled issues.

## Migration guide and evidence

- [ ] Keep v0.6 decommissioning out of the shipped Task CLI,
  `internal/task`, and repository scripts. v0.7 provides a reviewed guide and
  exact migration fixtures, not a compatibility command or helper program.
- [ ] Preserve exact default file content/fingerprints only where they help a
  reviewer distinguish Baton-managed files from user customization.
- [ ] Document read-only `gh api` inventory examples, ordered manual changes,
  warnings, and explicit unknowns. Never report completion when required-check
  state is unknown.
- [ ] Preview every file removal as a normal reviewed diff and run it only in
  an explicitly selected non-primary checkout.
- [ ] Treat settings changes that lack safe API support as named manual steps,
  not shell snippets applied optimistically.
- [ ] If repeated real migrations later demonstrate a need for automation,
  evaluate that as a separate goal rather than pre-building it into v0.7.

## Resource handling

### Remove or disable through review

- `.github/workflows/pr-policy.yml`
- `.github/workflows/work-item-transition.yml`
- `.github/workflows/delivery-recorder.yml`
- the v0.6 body/ownership issue-policy workflow
- retired required-check/ruleset entries
- obsolete `.github/baton.yml` orchestration fields or the whole file when
  uncustomized and unnecessary
- obsolete generated guidance and label-manifest files when exact-managed

### Preserve by default

- `agent`, `dev`, staging, base, feature, and `agent-work/*` branches
- open and closed issues, PRs, comments, and check history
- locked delivery-ledger issues and their comments
- `baton-delivery-bootstrap` environments and protection settings
- existing labels, including generic project and agent labels
- modified issue templates, workflow files, or guidance

### Report for human review

- open issues carrying `needs:review` or delivery-specific labels
- old `baton:managed` issues missing a v0.7 eligibility label
- contradictory eligibility or priority labels
- required checks whose source cannot be proven
- workflow pins that would continue installing v0.6

## Release migration document

Create `docs/adopter-updates/v0.7.0.md` with:

- the product boundary change and removed public surface;
- the exact safe decommission order;
- how existing `baton:managed` and agent labels behave;
- the absence of a replacement policy workflow and the optional project-owned
  issue-template guidance;
- removed legacy JSON contracts, without downstream-specific adaptation
  guidance;
- dry-run/audit commands and expected success criteria;
- explicit statements that branches, ledgers, issues, comments, labels, and
  environments are not automatically deleted.

## Validation

- [ ] Review the guide against unmodified, modified, partially installed, and
  already removed v0.6 fixtures.
- [ ] Verify required-check-first ordering and explicit unknown-settings
  handling in every documented scenario.
- [ ] Verify no instruction deletes branches, issues, comments, labels,
  environments, or worktrees.
- [ ] Verify every audit command is read-only and safely repeatable.
- [ ] Test v0.7 list behavior against migrated v0.6 issue labels without
  reading ownership comments.

## Completion criteria

- A reviewed adopter can remove v0.6 CI and delivery coupling without breaking
  ordinary PRs or deleting historical/project-owned resources.
- A read-only audit can prove the retired workflow/check surface is absent or
  name every unresolved manual action.
- No v0.6 compatibility path remains in normal Baton task execution.
- No shipped migration code, fixture, or document makes Baton responsible for
  a downstream orchestrator or adds compatibility concepts to Task code.
