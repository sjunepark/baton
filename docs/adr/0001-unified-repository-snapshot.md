# Use one repository snapshot for observation and recommendation

> Historical: superseded by the v0.7 issue-only Task contract. No active
> Baton command exposes snapshots, recommendations, or candidates.

Status: Superseded by v0.7.

At acceptance, Baton exposed one versioned `repositorySnapshot` produced by a
single bounded GitHub acquisition. The snapshot owns a typed Recommendation
whose Outcome is distinct from its optional Action. The then-current
`queueSnapshot` v1 and `nextCandidates` v2 remained pure, lossy projections
during that migration; their maintained successors are v2 and v3 respectively.
This prevents facts from different acquisition times being combined and keeps
Coda's execution ledger outside Baton. A generic derived-observation catalog
was rejected until a second real observation requires it; adding registration
and dependency ordering now would weaken the module's interface without
current leverage.
