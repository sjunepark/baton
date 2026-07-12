# Baton

Baton observes GitHub repository policy state and recommends repository work to
callers without owning their execution lifecycle.

## Language

**Repository Snapshot**:
A versioned, immutable observation produced by one bounded acquisition of a
repository's GitHub facts, queue facts, and one Recommendation.
_Avoid_: Progress, Run snapshot, status bundle

**Recommendation**:
Baton's judgment about what kind of response, if any, the observed repository
state warrants. A Recommendation is advice, not execution state or authority.
_Avoid_: Next job, scheduled work, execution

**Outcome**:
The disposition of a Recommendation: actionable, human choice required,
waiting, blocked, idle, or degraded.
_Avoid_: Status, run state

**Action**:
A typed kind of repository work that may be performed only when the Outcome and
caller policy permit it.
_Avoid_: Job, Run, command

**Candidate**:
A repository-scoped issue, pull request, or branch identity considered by a
Recommendation, including revision facts where the identity can move.
_Avoid_: Task, Job

**Completeness**:
Whether every fact required for the Repository Snapshot's Recommendation was
acquired and remained stable.
_Avoid_: Success, health

**Repository Policy**:
The validated, default-complete model that defines a repository's branches,
issue and pull-request rules, labels, and managed files.
_Avoid_: Config file, YAML policy, template settings

**Reconciliation Plan**:
A reviewable comparison between desired repository policy resources and their
observed preconditions, bound to a stable plan identity.
_Avoid_: Dry-run output, command list

**Operation Report**:
A durable account of each attempted, applied, refused, failed, or unattempted
effect in a multi-operation mutation.
_Avoid_: Success flag, error log

**Work Item State**:
The GitHub-authoritative lifecycle disposition of an issue: ready, active work
PR, awaiting review on staging, blocked, or promoted/closed. It is derived from
issue openness, workflow-state labels, and live pull requests.
_Avoid_: Agent mode, Coda Run state, progress

**Agent Mode**:
The repository-policy capability selected in the issue form, such as ready for
implementation or investigation only. It does not say where implementation is
in its GitHub lifecycle.
_Avoid_: Work Item State, execution status
