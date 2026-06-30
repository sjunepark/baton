# Baton Automation Setup

Use this reference when asked to create, update, or explain a recurring Codex
app automation that uses Baton to select and handle one safe unit of GitHub
issue or PR work. Also use it when asked to make a repository ready for
Baton-managed automation.

## Fit

Use a Baton automation when a Codex app run should periodically inspect a
repository, choose one policy-safe action, work on only that item in a
caller-provided isolated checkout, validate, report, and stop.

Prefer a standalone or project automation for recurring queue work. Each run
should start fresh, call Baton, and produce an independent result in Codex
Triage.

Use a thread automation only when the same conversation should keep waking up,
for example to monitor one PR, one deployment, or one long-running review loop.

Do not use a scheduled implementation automation until a manual test run of the
same prompt succeeds in a normal Codex thread.

## Prerequisites

Before scheduling, verify the target repository is Baton-ready:

```sh
baton home --format toon
baton doctor --format toon
baton ensure-branch --json
baton sync-labels --dry-run --repo owner/name --json
baton next --format toon --repo owner/name
```

If setup is incomplete, install Baton repository files first:

<!-- x-release-please-start-version -->
```sh
baton init --dry-run --json
baton init --apply --go-install github.com/sjunepark/baton/cmd/baton@v0.2.1
baton ensure-branch --apply
baton sync-labels --apply --repo owner/name --json
```
<!-- x-release-please-end -->

`baton init` installs or updates the target-repository policy files:

- `.github/baton.yml`
- `.github/labels.yml`
- `.github/ISSUE_WORKFLOW.md`
- `.github/ISSUE_TEMPLATE/agent-work.yml`
- `.github/workflows/issue-policy.yml`
- `.github/workflows/pr-policy.yml`

Review the `baton init --dry-run --json` plan before applying it. Use
`baton ensure-branch --apply` to create or verify the staging branch, normally
`agent`, without force-resetting existing branch state.

## Target Repository AGENTS.md

Add a small Baton section to the target repository's `AGENTS.md` when
autonomous agents should work from the GitHub issue/PR queue:

```md
## Baton Automation

- Use `$baton` for unattended GitHub issue and PR work.
- Run `baton next --format toon` before choosing queue work.
- Choose and handle at most one Baton candidate per run.
- Verify the automation is running in an isolated checkout before editing
  files; do not mutate the primary checkout for automation work.
- Push, comment, or open PRs only according to the chosen Baton candidate's
  action.
- Run focused validation and record completion with `baton complete` when
  useful.
- Stop and report on auth failures, missing isolation, ambiguous requirements,
  human product/security/schema decisions, unrelated red branch health, or
  unsafe branch/dirty checkout state.
- Never merge unless explicitly requested.
```

Keep repository-specific build, test, and validation commands in the same
`AGENTS.md` or a closer nested `AGENTS.md`; Baton supplies workflow safety, not
project build knowledge.

The automation environment needs:

- the Baton skill available, normally triggered as `$baton`;
- local `baton`, or the ability to run the repository's Baton binary;
- GitHub auth through `GITHUB_TOKEN`, `GH_TOKEN`, or `gh auth token`;
- `git` and network access for queue reads, pushes, and comments;
- access to the target project path when the automation runs.

For project-scoped Codex app automations, the local machine must be powered on,
Codex must be running, and the selected project must still exist on disk at run
time. If Codex offers a background worktree option for the automation, prefer it
over running directly in the user's active checkout. Baton does not create or
clean up worktrees; the automation platform owns that execution context.

## Create In Codex App

Create or update the scheduled job from a normal Codex thread by naming the
project, cadence, automation type, execution-context preference, and durable
prompt.

Use this request shape:

```text
Create a project automation named "Baton queue worker" for OWNER/REPO.
Run it every 30 minutes in a background worktree if that option is available.
Use the `$baton` skill. The automation should run the command below exactly,
choose and handle at most one Baton candidate, and report findings in Triage.

<paste the Default Queue Worker Prompt>
```

For a quick trial, ask Codex to run the same prompt once manually before
scheduling it. Only schedule implementation work after the manual run selected
the expected project, used Baton state, and respected checkout isolation
boundaries.

## Cadence

Start slower than the desired steady state and tighten after reviewing the
first few runs.

- PR follow-up monitor: every 15 to 30 minutes while active.
- General queue worker: every 30 to 120 minutes.
- Daily digest or readiness report: once per day.

Avoid overlapping implementation runs for the same repository or branch. If the
automation platform cannot provide isolated execution, report that and stop
instead of editing the active checkout.

## Default Queue Worker Prompt

Use this as the default prompt for a project or standalone automation:

```text
$baton run --repo OWNER/REPO
```

Replace `OWNER/REPO` with the target repository. Keep `--repo OWNER/REPO` in
the prompt when the automation may run from a Codex-created worktree or any
directory where remote detection could be ambiguous. The skill command expands
to the one-unit select/validate/report workflow and keeps the no-merge
boundary. The automation platform still owns checkout isolation.

## PR Follow-Up Prompt

Use this when the automation should babysit one PR until checks and review
feedback are clear:

```text
$baton follow-up NUMBER --repo OWNER/REPO
```

## Read-Only Triage Prompt

Use this for a low-risk automation that reports what would be done but never
edits files:

```text
$baton next --repo OWNER/REPO
```

## First-Run Review

After scheduling, inspect the first few automation outputs before trusting the
cadence. Check that each run:

- loaded `$baton`;
- ran `baton next` before selecting work;
- handled zero or one unit;
- verified isolated checkout context before edits;
- worked only in that isolated checkout;
- stopped on human decision points;
- reported validation status.

If the run edits the primary checkout, broadens scope beyond one item, or
ignores stop conditions, update the automation prompt before letting it run
again.

## Cleanup

Frequent Codex app worktree automations can create Codex-managed background
worktrees. Archive automation runs that no longer need their worktrees, and do
not pin runs unless their worktree state should be retained. Use the owning
tool, not Baton, for worktree cleanup.
