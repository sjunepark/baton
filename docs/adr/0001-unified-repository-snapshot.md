# Use one repository snapshot for observation and recommendation

Baton will expose one versioned `repositorySnapshot` produced by a single
bounded GitHub acquisition. The snapshot owns a typed Recommendation whose
Outcome is distinct from its optional Action, while `queueSnapshot` v1 and
`nextCandidates` v2 remain pure, lossy projections during Coda's migration.
This prevents facts from different acquisition times being combined and keeps
Coda's execution ledger outside Baton. A generic derived-observation catalog
was rejected until a second real observation requires it; adding registration
and dependency ordering now would weaken the module's interface without
current leverage.
