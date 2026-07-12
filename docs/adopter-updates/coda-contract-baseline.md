# Coda contract baseline

Coda may continue invoking, from the Project checkout:

```sh
baton next --json --repo owner/name
baton queue --json --repo owner/name
```

This slice does not change `nextCandidates` v2, `queueSnapshot` v1, or
structured error v1. Their producer-backed fixtures live in
`testdata/contracts/coda/` and cover issue, pull-request, branch, tied-selection,
and no-work recommendations.

The intentional safety correction is repository agreement: for queue-backed
commands, an explicit `--repo` or `GITHUB_REPOSITORY` that conflicts with the
configured checkout remote now returns error v1 with category `config` and exit
3 before GitHub is contacted. Coda Projects should keep `metadata.github_repo`
aligned with the remote selected by `repository.default_remote`.

No Coda source migration is required for this baseline. A later snapshot
adopter note will define the overlap period before the existing projections can
be removed in a major Baton release.
