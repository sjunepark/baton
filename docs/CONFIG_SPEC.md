# Config Spec

## File Names

Preferred target-repo config:

```text
.github/baton.yml
```

Compatibility input for Creo migration:

```text
.github/agent-issue-policy.yml
```

The first implementation may read the legacy file and normalize it into the
new Baton config model.

## Top-Level Shape

```yaml
version: 1

repository:
  default_remote: origin
  base_branch: main
  staging_branch: agent
  work_branch_prefix: agent-work/

issue_policy:
  policy_comment_marker: '<!-- baton-issue-policy:v1 -->'
  form_sections:
    work_kind: Work kind
    agent_mode: Agent mode
    summary: Summary
    context_evidence: Context / evidence
    acceptance_criteria: Acceptance criteria
    non_goals: Non-goals / constraints
    validation_hints: Validation hints
    notes: Notes
  work_kind_labels:
    Bug: bug
    Documentation: documentation
    Enhancement: enhancement
    Question: question
  agent_mode_labels:
    Ready trivial: agent:ready-trivial
    Ready bounded: agent:ready-bounded
    Investigate only: agent:investigate-only
    Needs discussion: needs:discussion
  controlled_label_groups:
    work_kind:
      - bug
      - documentation
      - enhancement
      - question
    agent_mode:
      - agent:ready-trivial
      - agent:ready-bounded
      - agent:investigate-only
      - needs:discussion
    quality_gate:
      - agent:blocked
  implementation_labels:
    - agent:ready-trivial
    - agent:ready-bounded
  comment_only_labels:
    - agent:investigate-only
  skip_labels:
    - agent:blocked
    - needs:discussion
    - needs:review
  required_sections:
    ready-trivial:
      - summary
      - context_evidence
      - acceptance_criteria
    ready-bounded:
      - summary
      - context_evidence
      - acceptance_criteria

pr_policy:
  required_reference_keyword: Refs
  forbidden_closing_keywords:
    - Closes
    - Fixes
    - Resolves
  allow_direct_base_branch_prs: true
  reject_all_trivial_multi_issue_prs: true
  noisy_commit_subjects:
    - address comments
    - address review
    - changes
    - fix
    - fix lint
    - lint
    - misc
    - oops
    - try again
    - update
    - wip
    - work in progress
  fail_when_commit_listing_reaches_cap: true

labels:
  manifest: .github/labels.yml

worktrees:
  backend: native
  root: ~/.baton/worktrees
  max_leases: 8
  stale_after: 8h

automation:
  prefer_pr_followup_before_issue_intake: true
  allow_merge: false
```

## Required Fields

Required for v1:

- `version`
- `repository.base_branch`
- `repository.staging_branch`
- `repository.work_branch_prefix`
- `issue_policy.form_sections`
- `issue_policy.agent_mode_labels`
- `issue_policy.implementation_labels`
- `issue_policy.skip_labels`
- `issue_policy.required_sections`

Optional fields use the defaults shown in the top-level shape unless a command
documents a narrower bootstrap behavior.

`pr_policy.allow_direct_base_branch_prs` controls ordinary PRs directly into
`repository.base_branch` from branches outside Baton's work branch prefix. When
true, Baton skips those direct PRs and leaves review, CI, and branch protection
to the repository. Promotion PRs from `repository.staging_branch` and mistaken
direct work PRs from `repository.work_branch_prefix` are still enforced.

## Legacy Mapping

Creo `.github/agent-issue-policy.yml` maps as:

- `target_branch` -> `repository.staging_branch`
- `work_branch_prefix` -> `repository.work_branch_prefix`
- `policy_comment_marker` -> `issue_policy.policy_comment_marker`
- `form_sections` -> `issue_policy.form_sections`
- `work_kind_labels` -> `issue_policy.work_kind_labels`
- `agent_mode_labels` -> `issue_policy.agent_mode_labels`
- `controlled_label_groups` -> `issue_policy.controlled_label_groups`
- `implementation_labels` -> `issue_policy.implementation_labels`
- `comment_only_labels` -> `issue_policy.comment_only_labels`
- `skip_labels` -> `issue_policy.skip_labels`
- `required_sections` -> `issue_policy.required_sections`

## Validation Rules

- Branch names must be non-empty and must not contain whitespace.
- `work_branch_prefix` must end with `/`.
- Controlled label groups must not contain duplicate labels across unrelated
  groups unless explicitly allowed later.
- Every implementation label must appear in an agent-mode label mapping or be
  marked as externally managed.
- Every required section ID must exist in `form_sections`.
- The policy marker must be stable once deployed, otherwise old policy comments
  cannot be updated in place.

## Config Loading Order

1. Explicit `--config <path>`.
2. `.github/baton.yml`.
3. `.github/agent-issue-policy.yml` legacy mode.
4. Built-in defaults only for `baton init`, not for policy enforcement.

Policy enforcement should fail when no target repo config exists, except for
commands explicitly intended to bootstrap config.
