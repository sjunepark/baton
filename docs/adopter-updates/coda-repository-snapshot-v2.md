# Coda adoption: repository snapshot

This note began with the v1 contract. The current contract is
`repositorySnapshot` v2; current schema and migration details in
[the v0.6.0 adopter update](v0.6.0.md) supersede older version references.

Coda can replace its sequential `baton next --json` and `baton queue --json`
reads with one command executed from the Project checkout:

```sh
baton snapshot --repo owner/name --json
```

The contract is `repositorySnapshot` schema v2. Its producer-backed fixtures
are documented in `testdata/README.md`. The nested `queue` field supplies the
counts and issue facts Coda reads from `queueSnapshot` v2, while
`recommendation` replaces display information formerly inferred from
`nextCandidates`.

## Display mapping

| Baton field | Coda external snapshot display |
| --- | --- |
| `repository` | Project GitHub repository identity |
| `completeness` | observation completeness badge |
| `warnings[]` | safe diagnostic details; degraded observations need attention |
| `queue.counts` | existing eligible/total counts and percentage calculation |
| `recommendation.outcome` | Baton snapshot status |
| `recommendation.action` | suggested work kind, shown only as advice |
| `recommendation.candidates` | issue, PR revision, or branch labels |
| `recommendation.reasons` | status explanation |

| Outcome | Coda behavior |
| --- | --- |
| `actionable` | display one useful Action and Candidate; a dispatcher may separately decide to create a Run |
| `human_choice_required` | display the Candidate set and require a person or caller policy to decide; one ready PR can require disposition without a tie |
| `waiting` | display the waiting reason; do not create work |
| `blocked` | display the blocker; do not create work |
| `idle` | display that Baton found no useful repository work |
| `degraded` | display warnings and retry guidance; never infer an Action |

An Action is not a Coda Job or Run state. Recording a Repository Snapshot must
not start a process, lease a worktree, retry work, or imply completion. Coda
continues to own those lifecycle decisions.

### Action advice mapping

| Action | Coda display/advice |
| --- | --- |
| `issue_implementation` | show “implement issue” with the issue number; creating a Run remains a separate dispatcher decision |
| `issue_investigation` | show “investigate issue” and the no-edit constraint; do not imply implementation work |
| `pull_request_follow_up` | show “follow up PR” with base/head revisions and isolated-checkout/no-merge constraints |
| `branch_health` | show “repair branch health” with ref/SHA; Coda must supply an isolated checkout |
| `sync_staging` | show the reviewed base-to-staging synchronization; never imply Baton will merge it |
| omitted | show status and reasons only; do not offer work creation |

## Compatibility window

`nextCandidates` v3 and `queueSnapshot` v2 remain lossy compatibility
projections while Coda migrates to the single `repositorySnapshot` v2
acquisition. They may be considered for removal only after Coda's maintained
branch validates v2 and at least one released Coda version has used the
single-command path. Removal then requires a separately reviewed Baton major
release; until those conditions hold, the projection commands and current
golden fixtures remain maintained.
