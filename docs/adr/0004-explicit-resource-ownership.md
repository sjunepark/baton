# Explicit resource ownership

Baton uses one pure ownership decision before policy acquisition, enrichment,
queue recommendation, history interpretation, or mutation.

For pull requests, the authoritative input is branch shape plus base and head
repository identity:

- same-repository `work_branch_prefix*` into staging is `work`;
- the same prefix into any other target is `misroutedWork`;
- same-repository staging into base is `promotion`;
- either managed shape with missing repository identity is `indeterminate`;
- known forks and all other shapes are `unmanaged`.

Generated `pull_request_target` workflows listen on every target. Their shared
job prefilter is conservative transport optimization only: it may admit an
uncertain candidate but cannot exclude a managed, misrouted, or indeterminate
shape. Go remains authoritative.

For issues, trusted issue policy writes a dedicated comment containing the
`baton-managed-issue:v1` record before labels or the policy comment. The record
is bound to GitHub's stable issue node ID and issue number and includes a
deterministic integrity digest. Only a comment authored by
`github-actions[bot]` with GitHub actor type `Bot` is authoritative. This
setup-free identity is shared by repository Actions: every workflow granted
`issues: write` is a trusted administrator and can compute a valid record.
Adopters must restrict that permission to reviewed default-branch workflows
and protect workflow-file changes. The digest is an integrity check, not a
signature that distinguishes Baton from another trusted workflow. The
`baton:managed` label is a query index, never ownership evidence by itself.

Equivalent trusted duplicates remain managed and produce a diagnostic.
Malformed, identity-mismatched, or otherwise contradictory trusted records
fail closed. Untrusted marker comments are ignored with a diagnostic. Issue
body and label edits do not revoke ownership. Trusted ownership-comment edit
or delete events repair the canonical record idempotently; a missed delete is
reported as an index-without-record failure until repair/backfill tooling is
run. Explicit unmanage is unsupported in this goal.

The prior issue-form fingerprint remains a bounded migration reader and reports
`legacyFingerprint` as its source. New issue-policy events dual-write the
record and index label. The fingerprint reader may be removed only after the
planned backfill/doctor slice reports no legacy-owned issues and adopters have
had at least one documented migration release.

Managed work-PR policy consumes the resulting ownership decision but not the
issue's current implementation, skip, or trivial labels. Those mutable labels
remain issue-intake and recommendation facts. Transition apply reacquires the
issue and trusted comments and rechecks ownership before any awaiting-review
write.

This replaces public target-first PR flow semantics, changes generated
workflow routing, and adds managed-issue ownership output. It is a breaking
`feat!:` change; under Baton's pre-1.0 policy it requires the v0.6.0 minor
release and adopter note.
