# Test Data

Reusable event, issue, PR, and git fixtures belong here when they are shared
across packages. Prefer inline fixtures for narrow package tests.

`contracts/coda/` freezes the versioned Baton JSON that the maintained Coda
consumer validates and renders. It includes the current legacy projections and
the `repositorySnapshot` v1 adoption target. Keep these fixtures additive
within their current schema versions; replace them only as part of a documented
migration.

`events/` contains representative GitHub webhook payloads used to verify
event-driven policy and work-item transitions.
