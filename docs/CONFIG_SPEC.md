# Config Spec

## Discovery and repository binding

Implicit discovery reads `.github/baton.yml`, then the legacy
`.github/agent-issue-policy.yml`, from the resolved git top-level rather than
the process subdirectory. The selected config path and its
`repository.default_remote` are retained in the resolved repository context.
For queue and recommendation acquisition, policy from one checkout cannot be
paired with a conflicting explicit GitHub `owner/name`.

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

setup:
  baseline_baton_version: v0.5.1 # x-release-please-version

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
    priority: Priority
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
  priority_labels:
    P0: priority:p0
    P1: priority:p1
    P2: priority:p2
    P3: priority:p3
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
    priority:
      - priority:p0
      - priority:p1
      - priority:p2
      - priority:p3
    quality_gate:
      - needs-info
  implementation_labels:
    - agent:ready-trivial
    - agent:ready-bounded
  comment_only_labels:
    - agent:investigate-only
  skip_labels:
    - needs-info
    - needs:discussion
    - needs:review
  awaiting_review_label: needs:review
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

Released v1 files may still contain the obsolete `automation` mapping. Baton
accepts it only as migration wire data: it has no runtime semantics and is not
emitted when the compiled Repository Policy is rendered. Baton never gains
merge authority from repository policy.

`setup.baseline_baton_version` records the Baton release the repository's setup
files were last reviewed or applied against. It is not the config schema
version, the GitHub Actions runtime pin, or a compatibility guarantee.

`issue_policy.priority_labels` is optional for existing configs. When omitted,
Baton does not require a Priority form field or apply priority queue ordering.

`pr_policy.allow_direct_base_branch_prs` controls ordinary PRs directly into
`repository.base_branch` from branches outside Baton's work branch prefix. When
true, Baton skips those direct PRs and leaves review, CI, and branch protection
to the repository. Promotion PRs from `repository.staging_branch` and mistaken
direct work PRs from `repository.work_branch_prefix` are still enforced.

Promotion enforcement uses the pull request event's base and head revisions to
derive the merged Baton work PRs included in that promotion. A promotion with
complete evidence and no included Baton work may contain only manual changes
and needs no closing keyword. When Baton work is included, the promotion must
close every issue referenced by those work PRs with a configured closing
keyword. Unrelated closing references do not satisfy an omitted expected issue,
and incomplete comparison, association, or work-reference evidence fails the
policy check.
Expected issues come from the current title and body of each merged work PR.
Repositories must treat post-merge edits to that metadata as trusted policy
changes. GitHub request failures remain operational errors rather than partial
policy decisions.

## Legacy Mapping

Creo `.github/agent-issue-policy.yml` maps as:

- `target_branch` -> `repository.staging_branch`
- `work_branch_prefix` -> `repository.work_branch_prefix`
- `policy_comment_marker` -> `issue_policy.policy_comment_marker`
- `form_sections` -> `issue_policy.form_sections`
- `work_kind_labels` -> `issue_policy.work_kind_labels`
- `agent_mode_labels` -> `issue_policy.agent_mode_labels`
- `priority_labels` -> `issue_policy.priority_labels` when present
- `controlled_label_groups` -> `issue_policy.controlled_label_groups`
- `implementation_labels` -> `issue_policy.implementation_labels`
- `comment_only_labels` -> `issue_policy.comment_only_labels`
- `skip_labels` -> `issue_policy.skip_labels`
- `required_sections` -> `issue_policy.required_sections`

## Validation Rules

- Branches must satisfy Git ref-name rules, and the configured remote must be a
  normalized, non-option remote name. Rendered workflows quote branch scalars.
- Unknown fields and unsupported config versions fail closed.
- `work_branch_prefix` must end with `/`.
- Controlled label groups must contain non-empty, case-insensitively unique
  labels. Work-kind and agent-mode mappings must exactly match their groups.
- Every implementation and comment-only label must appear in an agent-mode
  mapping, and the two capability sets must be disjoint.
- `awaiting_review_label` must be a skip label and must not be a controlled
  form-derived label; it records workflow state without changing Agent Mode.
- Every required section ID must exist in `form_sections`, and every required
  mode slug must correspond to an agent-mode option.
- When `issue_policy.priority_labels` is present, `form_sections.priority` and
  `controlled_label_groups.priority` are required. Priority label mappings must
  match the controlled priority labels as a set. Separately,
  `controlled_label_groups.priority` order defines queue rank.
- The policy marker must be a non-empty, versioned HTML comment. This prevents
  accidental matching or replacement of unrelated issue comments.

## Config Loading Order

1. Explicit `--config <path>`.
2. `.github/baton.yml`.
3. `.github/agent-issue-policy.yml` legacy mode.
4. Built-in defaults only for `baton init`, not for policy enforcement.

Policy enforcement should fail when no target repo config exists, except for
commands explicitly intended to bootstrap config.
