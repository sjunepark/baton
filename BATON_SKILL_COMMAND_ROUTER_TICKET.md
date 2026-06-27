# Ticket: Add Baton skill commands and user-flow docs

## Summary

The bundled Baton Codex skill currently works, but its user interface is too
prompt-heavy. Users have to type `$baton` plus a long workflow prompt such as
"create a Baton-ready GitHub issue..." or "take one Baton-selected unit...".
That is not good skill design: the skill already knows those workflows, so the
user should only provide true arguments like a todo, issue number, PR number,
or target repository.

Add a command-router interface to `skills/baton/SKILL.md`, similar in shape to
the command table and routing rules in the Impeccable skill. Also add project
docs that explain Baton user flows and show the skill invocation and equivalent
CLI commands for each flow.

## Current state

- Done: `skills/baton/SKILL.md` now documents the `$baton <command>` command
  table, routing rules, command-level consent boundaries, and per-command
  workflows.
- Done: durable user-flow docs live in `docs/USER_FLOWS.md`; `README.md` and
  `docs/SKILL_SPEC.md` link/specify the command-router surface.
- Done: bundled skill references now use concise commands such as
  `$baton todo`, `$baton todos`, `$baton run`, `$baton follow-up`, and
  `$baton next` instead of boilerplate workflow prompts.
- Validation: `go test ./...` passes.
- Validation: `rg -n "baton todo|baton todos|\\$baton" README.md docs skills`
  only finds skill command references plus the explicit note that the Go CLI
  does not implement `baton todo`/`baton todos`.
- Validation: `wc -l skills/baton/SKILL.md skills/baton/references/*.md
  docs/USER_FLOWS.md` reports `SKILL.md` at 142 lines and 757 lines total.
- Next: decide separately whether Baton should gain real CLI issue creation
  helpers such as `baton todo`; no CLI behavior was added in this slice.

## Problem

Current examples require users to paste operational instructions that should
live in the skill:

```text
$baton

Create one Baton-ready GitHub issue for this todo:

<todo>

Use the repository's Agent-readable work item issue template...
```

The desired interaction is closer to:

```text
$baton todo <todo>
```

or:

```text
$baton run
$baton follow-up 123
$baton status
```

The command name should imply the workflow. Everything after the command should
be treated as the command argument.

## Goals

- Make `$baton` usable as a concise command surface for common Baton workflows.
- Preserve Baton safety gates: no primary-checkout mutation for automation
  work, no edits without a lease, one unit per run, and no merge without an
  explicit user request.
- Keep deterministic behavior in the Go CLI and agent judgment in the skill.
- Document user flows in the Baton project with both recommended skill prompts
  and equivalent CLI commands.
- Keep docs aligned with the implemented CLI; do not invent CLI behavior that
  does not exist.

## Non-goals

- Do not implement broad new GitHub issue creation inside the Go CLI unless a
  separate design decision is made.
- Do not make `$baton run` handle multiple queue items in one invocation.
- Do not make any command merge PRs by default.
- Do not remove the existing Baton CLI commands or JSON contracts.
- Do not turn the skill into a replacement for repository-local `AGENTS.md`
  instructions.

## Proposed skill command API

Add a `## Commands` section to `skills/baton/SKILL.md`.

Suggested command table:

| Skill command | Behavior |
| --- | --- |
| `$baton` | Show readiness, queue summary, and 2-3 recommended next commands. Read-only. |
| `$baton status [repo]` | Run readiness and queue checks. Read-only. |
| `$baton next [repo]` | Show the next Baton-selected action. Read-only. |
| `$baton queue [repo]` | Show eligible and skipped issues/PRs. Read-only. |
| `$baton todo <todo>` | Create one Baton-ready GitHub issue from the todo. No branch or PR. |
| `$baton todos <notes-or-file>` | Split notes into Baton-ready GitHub issues. No branch or PR. |
| `$baton investigate <issue>` | Investigate/comment on one issue. No file edits unless the user explicitly changes scope. |
| `$baton implement <issue>` | Implement one eligible ready issue in a Baton lease and open/update a PR to the staging branch. |
| `$baton follow-up <pr>` | Inspect checks/review threads, lease the existing PR branch, fix only that PR follow-up, push to the same branch. |
| `$baton run [repo]` | Let Baton select exactly one safe unit, lease if edits are needed, handle it, validate, report, and stop. |
| `$baton adopt [repo]` | Check target-repo Baton setup: doctor, config/init plan, labels, branch plan. Do not apply destructive branch sync. |
| `$baton automate [repo]` | Explain or prepare scheduled automation setup. Do not schedule implementation automation until a manual run succeeds. |

### Routing rules

Add rules modeled after Impeccable's command routing:

1. No argument means read-only status/menu. Never auto-run mutating work.
2. If the first word matches a command, load only the reference needed for that
   command and execute that workflow.
3. Everything after the command name is the command argument. Do not require the
   user to paste workflow instructions the skill already knows.
4. If intent clearly maps to a command, proceed as if that command was invoked.
   Example: "create a Baton todo for X" maps to `todo`.
5. If two mutating commands could fit, ask one short clarification before
   acting.
6. Preserve command-level consent boundaries:
   - `status`, `next`, `queue`, and no-arg `$baton` are read-only.
   - `todo` and `todos` may create GitHub issues but must not create branches,
     commits, PRs, or leases.
   - `investigate` may comment on an issue but must not edit files unless the
     user explicitly changes scope.
   - `implement`, `follow-up`, and `run` may edit only after acquiring a Baton
     lease and changing to the returned lease path.
   - No command may merge unless the user explicitly asks to merge and target
     repo policy allows it.

## Command behavior details

### `$baton`

Read-only dashboard:

- Run `baton home --format toon` or `baton doctor --format toon` when
  readiness is uncertain.
- Run `baton queue --format toon --repo <repo>` when repo can be resolved.
- Run `baton next --format toon --repo <repo>`.
- Report the current state and 2-3 exact next skill commands.
- Do not lease, edit, push, comment, or create issues.

### `$baton status [repo]`

Read-only readiness:

- Run `baton doctor --format toon`.
- Run `baton ensure-branch --json` if branch setup is relevant.
- Run `baton sync-labels --dry-run --repo <repo> --json` when repo is known.
- Summarize setup gaps and exact safe commands to fix them.
- Do not apply setup unless the user separately requests it.

### `$baton next [repo]`

Read-only selection:

- Run `baton next --format toon --repo <repo>`.
- Report the selected action, issue/PR number, reason, and whether it is
  read-only, issue creation, investigation, implementation, or PR follow-up.
- Do not take the action.

### `$baton todo <todo>`

Issue creation workflow:

- Read `references/todo-creation.md`.
- Convert the todo into the Agent-readable work item Markdown body using the
  required `###` headings.
- Choose the least-permissive Agent mode:
  - `Ready trivial` for tiny obvious fixes.
  - `Ready bounded` for scoped implementation with clear acceptance criteria.
  - `Investigate only` for research/diagnosis/reporting.
  - `Needs discussion` for product/security/schema/release/compatibility
    decisions.
- Run `baton issue-policy --body-file <tmp-file> --json` as a preflight.
- Create the issue with `gh issue create --repo <repo> --title ... --body-file
  <tmp-file>`.
- Report issue number, title, chosen Agent mode, and why.
- Do not create a branch, commit, lease, PR, or merge.

If the todo lacks enough information, prefer `Investigate only` or
`Needs discussion` over asking for a long prompt. Ask a concise clarification
only when creating even an investigation/discussion issue would be misleading.

### `$baton todos <notes-or-file>`

Multi-todo issue creation:

- Read `references/todo-creation.md`.
- Split unrelated work into separate issues.
- Merge duplicates that describe the same outcome.
- Use the same preflight and `gh issue create` path as `todo`.
- Report every created issue with chosen Agent mode and reason.
- Do not implement.

### `$baton investigate <issue>`

Issue investigation workflow:

- Inspect the issue through Baton/GitHub.
- Confirm the issue is `agent:investigate-only` or otherwise explicitly scoped
  for investigation.
- Read relevant repo files and run diagnostics only as needed.
- Do not edit files unless the user explicitly changes scope.
- Comment with findings, evidence, and a recommended next label.

### `$baton implement <issue>`

Issue implementation workflow:

- Confirm the issue has an implementation label and no skip label.
- Acquire a Baton lease from the configured staging branch:

  ```sh
  baton lease --purpose issue-<number> --base origin/<staging> \
    --new-branch <work-branch-prefix><number>-<slug> --repo <repo> --json
  ```

- Work only inside the returned `path`.
- Read that checkout's `AGENTS.md`.
- Implement the smallest change satisfying acceptance criteria.
- Validate with focused checks.
- Open/update a PR to the staging branch with `Refs #<issue>`, not closing
  keywords.
- Complete/comment and release the lease when clean.

### `$baton follow-up <pr>`

PR follow-up workflow:

- Run `baton pr <number> --json --repo <repo>`.
- Run `baton checks <number> --format toon --repo <repo>`.
- Run `baton review-threads <number> --format toon --repo <repo>`.
- Fix failing required checks before review nits.
- Treat unresolved human review threads as higher priority than bot comments.
- Acquire a lease for the existing PR branch.
- Push fixes to the same PR branch. Do not open a replacement PR.
- Complete/comment and release the lease when clean.

### `$baton run [repo]`

General automation worker:

- Run `baton next --format toon --repo <repo>`.
- If action is `none` or `digest`, report and stop.
- If action is investigation, investigate/comment only.
- If action is implementation, lease a new work branch.
- If action is PR follow-up, lease the existing PR branch.
- Handle exactly one unit.
- Validate, report, and stop.

### `$baton adopt [repo]`

Target repository setup workflow:

- Run `baton home --format toon` and `baton doctor --format toon`.
- Run `baton init --dry-run --json`.
- Run `baton migrate-config --dry-run` when a legacy
  `.github/agent-issue-policy.yml` exists.
- Run `baton sync-labels --dry-run --repo <repo> --json`.
- Run `baton ensure-branch --json`.
- Recommend exact next setup commands.
- Do not run `init --apply`, `migrate-config --apply`, `sync-labels --apply`,
  or `ensure-branch --apply` unless the user explicitly requests apply.
- Treat branch divergence as a separate human decision.

### `$baton automate [repo]`

Automation setup workflow:

- Read `references/automation-setup.md`.
- Verify prerequisites with read-only commands.
- Produce a scheduled automation prompt that uses the new command surface where
  possible, for example `$baton run --repo owner/name` if the skill supports
  that argument shape.
- Recommend starting with read-only automation before implementation
  automation.

## User-flow docs requirement

Add durable docs to the Baton project that explain user flows from a human
operator's point of view.

Suggested file:

```text
docs/USER_FLOWS.md
```

Update `README.md` and/or `docs/SKILL_SPEC.md` to link to it.

The docs should include at least these flows:

1. **Create one todo**
   - Skill: `$baton todo <todo>`
   - CLI: create an issue body with Agent-readable `###` headings, preflight
     with `baton issue-policy --body-file <file> --json`, then create with
     `gh issue create --body-file <file>`.
2. **Create many todos from notes**
   - Skill: `$baton todos <notes-or-file>`
   - CLI: same as above, repeated per split issue.
3. **Check readiness/status**
   - Skill: `$baton status`
   - CLI: `baton doctor --format toon`, `baton sync-labels --dry-run`, and
     `baton ensure-branch --json`.
4. **Inspect queue and next action**
   - Skill: `$baton queue`, `$baton next`
   - CLI: `baton queue --format toon`, `baton next --format toon`.
5. **Investigate an issue**
   - Skill: `$baton investigate <issue>`
   - CLI: `baton queue`, `baton next`, GitHub issue read/comment commands.
6. **Implement an issue**
   - Skill: `$baton implement <issue>` or `$baton run`
   - CLI: `baton lease`, work in returned path, validate, create PR, `baton
     complete`, `baton release`.
7. **Follow up on a PR**
   - Skill: `$baton follow-up <pr>`
   - CLI: `baton pr`, `baton checks`, `baton review-threads`, `baton lease
     --branch`, validate, push, complete, release.
8. **Adopt Baton in a target repo**
   - Skill: `$baton adopt`
   - CLI: `baton init --dry-run`, `baton migrate-config --dry-run`,
     `baton sync-labels --dry-run`, `baton ensure-branch`.
9. **Set up scheduled automation**
   - Skill: `$baton automate`
   - CLI: use read-only checks first, then schedule a one-unit worker.

For every flow, document:

- when to use it;
- required inputs;
- skill command;
- equivalent CLI commands;
- expected output;
- safety boundaries;
- common stop conditions.

## Implementation steps

1. [x] Update `skills/baton/SKILL.md` with command routing.
2. [x] Split long command-specific details into references if `SKILL.md` becomes
   too large. Prefer one-level references such as:
   - `references/commands.md`
   - `references/todo-creation.md`
   - `references/automation-setup.md`
   - optional new `references/skill-commands.md`
3. [x] Add `docs/USER_FLOWS.md`.
4. [x] Update `docs/SKILL_SPEC.md` so the spec requires the command-router surface.
5. [x] Update `README.md` project map to include `docs/USER_FLOWS.md`.
6. [x] Review existing examples in `skills/baton/references/*.md`; replace
   boilerplate prompts with concise skill commands where appropriate.
7. [x] Validate docs and skill behavior against implemented CLI commands.

## Acceptance criteria

- `$baton` with no argument is documented as a read-only dashboard/menu.
- `SKILL.md` contains a command table and explicit routing rules.
- Users can discover the concise invocation for each common flow without
  writing boilerplate workflow prompts.
- `todo` and `todos` commands clearly create GitHub issues only; they do not
  branch, commit, lease, open PRs, or merge.
- `run`, `implement`, and `follow-up` commands require a Baton lease before
  edits and work only in the returned lease path.
- `docs/USER_FLOWS.md` maps every supported user flow to both skill commands
  and current CLI commands.
- Docs do not claim that the Go CLI has a `baton todo` command unless that
  command is actually implemented.
- README links to the new user-flow docs.
- `go test ./...` passes after changes.

## Validation hints

- Run:

  ```sh
  go test ./...
  ```

- Check docs for unsupported command claims:

  ```sh
  rg -n "baton todo|baton todos|\\$baton" README.md docs skills
  ```

- Check the skill remains concise enough to load comfortably:

  ```sh
  wc -l skills/baton/SKILL.md skills/baton/references/*.md docs/USER_FLOWS.md
  ```

## Open design questions

- Should Baton eventually gain a real CLI issue-creation helper, such as
  `baton todo --repo owner/name --title ... --body ...`, or should issue
  creation remain a skill-plus-`gh issue create` workflow?
- Should `$baton run` accept flags like `--repo owner/name`, or should repo
  selection remain inferred from the current checkout plus natural-language
  arguments?
- Should command-specific shortcuts be separately pinnable, such as `$baton-todo`
  or `$baton-run`, or is `$baton <command>` enough?

## Context from original request

The motivating user feedback:

> For all the user flow you're describing, It looks like I have to pass
> additional prompts in addition to `$baton`.
>
> I don't think this is good skill design. I should only have to pass true
> arguments (like the todo).
>
> Maybe I should create a command like api or something for the baton skill?

Conclusion: implement a command API in the skill, not more example prompts.

## Progress log

- 2026-06-26: Added the skill command router, user-flow docs, spec/README
  links, and concise reference examples. Validated with `go test ./...`,
  command-reference search, line-count check, and a bounded
  post-implementation review. No Bucket I review fixes were needed. Remaining
  work is a separate product decision about whether to add CLI issue-creation
  helpers.
