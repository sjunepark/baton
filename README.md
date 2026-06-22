# Baton

Baton is a planned Go CLI and companion Codex skill for running a reusable
solo-developer agent workflow across GitHub projects.

The first target is to extract the GitHub issue and PR policy workflow from
`/Users/sejunpark/IT/creo` and make it reusable in other repositories. Baton
should provide deterministic GitHub, git, and worktree operations while leaving
code design and implementation judgment to Codex through a bundled skill.

## Status

This repository currently contains requirements and implementation notes only.
Do not add runnable CLI code until the first implementation session starts.

## Product Boundary

Baton owns:

- GitHub issue policy evaluation and label application.
- PR policy evaluation for agent work branches and promotion PRs.
- GitHub queue inspection for issues, PRs, checks, and review threads.
- One-unit-at-a-time triage recommendations for Codex automations.
- Safe worktree leasing so automations do not mutate the user's main checkout.
- Installation templates for GitHub workflows, issue templates, labels, and
  repo policy config.
- A companion skill that tells Codex how to use the CLI safely.

Baton does not own:

- Implementing product code changes itself.
- Designing source changes.
- Resolving ambiguous review comments automatically.
- Merging PRs by default.
- Replacing GitHub Issues as the durable queue.

## Planned Shape

```text
baton CLI
  deterministic state, policy, leases, and JSON

baton skill
  agent judgment, stop rules, and workflow guidance

target repository
  small config, GitHub workflow wrappers, labels, issue template, docs
```

The CLI should be boring and reliable. The skill should be concise and
project-aware enough for Codex to make good decisions from Baton JSON.

## Start Here

- [docs/REQUIREMENTS.md](docs/REQUIREMENTS.md): Product and safety requirements.
- [ARCHITECTURE.md](ARCHITECTURE.md): Planned system shape and invariants.
- [docs/CLI_SPEC.md](docs/CLI_SPEC.md): Command and JSON contract.
- [docs/CONFIG_SPEC.md](docs/CONFIG_SPEC.md): Reusable policy config.
- [docs/GITHUB_POLICY_EXTRACTION.md](docs/GITHUB_POLICY_EXTRACTION.md): Creo
  extraction plan.
- [docs/WORKTREE_LEASING.md](docs/WORKTREE_LEASING.md): Automation isolation
  design.
- [docs/SKILL_SPEC.md](docs/SKILL_SPEC.md): Companion skill requirements.
- [docs/IMPLEMENTATION_PLAN.md](docs/IMPLEMENTATION_PLAN.md): Suggested build
  sequence.

