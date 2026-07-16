# v0.7 skill, documentation, and release

## Purpose

Align every user- and agent-facing surface with the issue-only product after
the Task contract and CLI are stable. Prevent an old installed skill, duplicate
manual, or v0.5/v0.6 release pin from continuing to prescribe the retired
workflow.

## Bundled skill

- [ ] Rewrite `skills/baton/SKILL.md` around explicit task enrollment,
  classification, list/show/next inspection, advisory start/stop, and explicit
  close.
- [ ] Keep `$baton todo`/`todos` as creation guidance, but explicitly enroll
  and label new issues instead of requiring a body fingerprint.
- [ ] Add the workflow for making an existing manual/external issue
  Baton-compatible: inspect it, choose or ask for the fixed mode/priority and
  blockers, then call `enroll`/`update` without editing its body.
- [ ] Do not invent a required policy comment. An ordinary explanatory comment
  is optional and never changes enrollment or classification.
- [ ] Keep `$baton implement ISSUE` only as a thin convenience: call `start`,
  hand implementation entirely to the target project's own instructions and
  tools, then ask whether to call `close` when interaction is available. If it
  cannot ask, leave the Task open and report the explicit close command.
- [ ] Do not add checkout, branch, PR, CI, review, merge, validation, Run, or
  dispatcher rules to that convenience flow; they remain project-owned.
- [ ] Remove staging branches, work-prefixes, `Refs #...`, PR targeting,
  follow-up, promotion, synchronization, delivery, adoption, scheduler,
  `$baton run`, `$baton follow-up`, `$baton investigate`, and `$baton automate`
  instructions.
- [ ] Delete `skills/baton/references/automation-setup.md`,
  `delivery-bootstrap.md`, and `json-contracts.md`. Rewrite
  `todo-creation.md` without required headings/body preflight, and reduce
  `commands.md` to links to current CLI help.
- [ ] Keep command syntax in concise CLI help/reference rather than duplicating
  it across multiple skill files.
- [ ] Add a check or documented distribution step that detects drift between
  the repository skill and installed/distributed copies.

## Active documentation

- [ ] Rewrite `README.md` as a short product overview and setup-free quick
  start, with optional project-owned issue-template guidance clearly separate
  from Baton behavior.
- [ ] Rewrite `ARCHITECTURE.md` around the Task module, issue-store seam, CLI,
  GitHub adapter, and skill judgment.
- [ ] Rewrite `CONTEXT.md` with Task, Enrollment, Mode, Priority, Blocker,
  Activity, and Done; remove Candidate, PR Flow, Delivery Ledger, and the rule
  against saying Task.
- [ ] Replace `docs/REQUIREMENTS.md`, `CLI_SPEC.md`, `OUTPUT_SPEC.md`,
  `SKILL_SPEC.md`, and `USER_FLOWS.md` with concise v0.7 sources of truth;
  delete `CONFIG_SPEC.md` because v0.7 has no active config.
- [ ] Delete the duplicated `docs/index.html`, `overview.html`,
  `reference.html`, and `tutorial.html` manuals rather than maintaining a
  second documentation system.
- [ ] Remove or rewrite `docs/AXI_REVIEW.md`, `EXECUTION_CONTEXT.md`,
  `GITHUB_POLICY_EXTRACTION.md`, `IMPLEMENTATION_PLAN.md`, `NEXT_SESSION.md`,
  `RELEASE.md`, the completed root `TODO.md`, and
  `docs/adopter-updates/README.md` so none remains an active pre-v0.7
  contract.
- [ ] Mark orchestration goals and ADRs as superseded/history, and archive or
  remove active delivery/bootstrap/session handoff documents.
- [ ] Remove downstream-tool-specific integration, fixture, and migration
  guidance; Baton's active documentation ends at its public Task contract.
- [ ] Keep `CHANGELOG.md` and versioned release notes such as the v0.5/v0.6
  adopter updates as immutable historical context. This does not preserve the
  downstream-specific current-contract documents named for deletion in the
  Task-core plan.
- [ ] Rewrite `testdata/README.md` after old contracts move or disappear, and
  validate all Markdown/HTML links after deletions.

## Repository instructions

- [ ] Update `AGENTS.md` after enforcement exists so validation expectations no
  longer require deleted GitHub event, branch-plan, or PR-policy tests.
- [ ] Preserve the Task-relevant durable rules: Go-first CLI, typed GitHub
  client, deterministic CLI versus skill judgment, explicit mutations,
  JSON-first results, and relevant validation.
- [ ] Remove source-reference guidance that future agents could mistake for
  active Baton product requirements; retain historical pointers only where
  migration evidence needs them.
- [ ] Remove named external executors and Task-irrelevant checkout/process
  lifecycle rules from Baton instructions. Retain isolated-checkout safety
  and review/merge safety only for the separate adopter-decommission workflow
  that can write project files or settings.
- [ ] Rewrite `greptile.json`, `.greptile/rules.md`, and `.coderabbit.yaml` so
  automated reviews no longer enforce PR policy, worktree, dispatcher,
  handoff, config, or orchestration assumptions.

## Release Please preparation

- [ ] Use intentional breaking Conventional Commits so Release Please proposes
  v0.7.0 under the repository's pre-1.0 policy.
- [ ] Prune `release-please-config.json` extra-file entries for deleted install,
  config, HTML, and automation files; retain only real marked version targets.
- [ ] Add the focused v0.7 adopter update from the migration plan, covering
  direct migration from v0.5.0, v0.5.1, and v0.6.0 plus conservative handling
  of mixed, customized, older, and unknown installations.
- [ ] Let Release Please update `CHANGELOG.md`, the manifest, tags, GitHub
  release, release PR, and remaining marked version references.
- [ ] Review the generated release PR's diff, version, migration note, skill,
  command help, JSON contracts, and install examples before any merge.
- [ ] Do not manually tag, publish, or merge the release without explicit user
  authorization.

## Validation

- [ ] Run `gofmt`, `go vet ./...`, pinned staticcheck, and `go test ./...`.
- [ ] Run `baton --help` and every retained subcommand help; verify removed
  commands use the ordinary concise unknown-command error.
- [ ] Golden-test only the canonical JSON and human-text results; ensure no
  TOON, `--format`, or `--fields` path remains.
- [ ] Search active docs, templates, help, skill, and source for stale terms:
  `agent-work/`, staging promotion, `Refs #`, PR policy, delivery ledger,
  sealed authority, required issue body headings, legacy output projections,
  Candidate, Recommendation, dispatcher, `queueSnapshot`, `nextCandidates`,
  `repositorySnapshot`, `--config`, `--fields`, and `operationReport`.
- [ ] Audit `Coda`, `Treehouse`, caller/Run/Job/Runner, scheduled/unattended,
  automation-contract, and background-checkout/worktree language. Remaining
  occurrences must be explicitly superseded history or the v0.7 removal plan,
  not active product guidance.
- [ ] Verify no target-repository file is required for normal task commands.
- [ ] Verify there is no active config, setup/readiness command, installed
  intake profile, issue-policy workflow, or policy-comment contract.
- [ ] Run link validation and `go mod tidy`; verify deleted docs have no active
  inbound links and YAML is no longer a Go dependency.
- [ ] Compare the bundled skill with the installed/distributed artifact using
  the supported update flow.
- [ ] Run the repository-required implementation and diet review; resolve safe
  findings and report any remaining judgment calls.

## Completion criteria

- A new reader encounters one coherent issue-task product across help, README,
  architecture, specs, skill, and examples.
- An installed skill cannot direct an agent into a staging or PR convention
  that the v0.7 CLI no longer owns.
- The only skill flow touching project implementation is the thin explicit
  start/project-work/optional-close convenience requested by the user; it adds
  no Baton execution model.
- Active Baton documentation makes no downstream-orchestrator compatibility or
  migration promise.
- Release Please proposes the intended breaking v0.7.0 with an actionable,
  non-destructive adopter migration from v0.5.0, v0.5.1, or v0.6.0.
- No release or merge occurs without explicit user authorization.
