# Coda contract baseline

This file records the original projection overlap. Current consumers use
`repositorySnapshot` v2; maintained projections are `nextCandidates` v3 and
`queueSnapshot` v2. See
[Coda repository snapshot v2](coda-repository-snapshot-v2.md).

Coda may continue invoking, from the Project checkout:

```sh
baton next --json --repo owner/name
baton queue --json --repo owner/name
```

The maintained `nextCandidates` v3, `queueSnapshot` v2, and structured error v1
producer-backed fixtures live in
`testdata/contracts/coda/` and cover issue, pull-request, branch, tied-selection,
and no-work recommendations.

The intentional safety correction is repository agreement: for queue-backed
commands, an explicit `--repo` or `GITHUB_REPOSITORY` that conflicts with the
configured checkout remote now returns error v1 with category `config` and exit
3 before GitHub is contacted. Coda Projects should keep `metadata.github_repo`
aligned with the remote selected by `repository.default_remote`.

The current snapshot adopter note defines the overlap period before the
projection commands can be removed in a major Baton release.
