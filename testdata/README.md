# Test Data

Reusable event, issue, PR, and git fixtures belong here when they are shared
across packages. Prefer inline fixtures for narrow package tests.

`contracts/coda/` freezes the versioned Baton JSON that the maintained Coda
consumer validates and renders. It includes the current legacy projections and
the `repositorySnapshot` v2 adoption target. Keep these fixtures additive
within their current schema versions; replace them only as part of a documented
migration.

`events/` contains representative GitHub webhook payloads used to verify
event-driven policy and work-item transitions.

`adopter-compatibility/` freezes end-user PR collision scenarios and their
explicit ownership outcomes across policy, queue, transition, and generated
workflow prefilter behavior. It includes missing base/head repository identity
so conservative workflow routing cannot hide an indeterminate Go decision.

`doctor/` freezes common adopter repository conditions and their ready,
degraded, or blocked compatibility outcomes.

`contracts/pr-policy-v4-work.json` freezes the v4 managed-work decision after
mutable issue labels were removed from merge authority.
`contracts/work-item-transition-v4-promotion.json` freezes cursor-last explicit
promotion completion: issue close/index removal, base-integration append, then
cursor commit.
`contracts/pull-request-v2-merged-work.json` freezes ledger-backed merged-work
readiness after mutable PR text changes.
`contracts/queue-v2-label-intake.toon` keeps the complementary agent-facing
contract: labels still determine issue intake and recommendation.
`contracts/doctor-v2-ready.json` and `contracts/doctor-v2-blocked.toon` freeze
the live compatibility result, remediation, counts, help, and compact output.
`contracts/coda/next-v3-sync-staging.json` and
`contracts/coda/repository-snapshot-v2-sync-staging.json` freeze the
revision-bound direct-base synchronization recommendation.

`delivery/` describes adapter-shaped checkpoint/comment scenarios for the
versioned delivery contracts. It covers supported promotion merge strategies,
manual-only and excluded work, record tampering or loss, duplicates, and stale
seals. `bootstrap-scenarios.json` freezes explicit-boundary, ambiguous
awaiting-review, and ownership-first migration cases. Bootstrap shadow
comparisons gate the explicit `delivery.authority: sealed` cutover; production
authority never falls back to ancestry.
