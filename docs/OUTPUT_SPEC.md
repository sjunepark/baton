# Baton v0.7 output

`--json` emits one JSON document to stdout on success. Errors are one document
on stderr. There is no alternate format or field projection.

## Task

Every operation uses one Task shape:

```json
{
  "number": 42,
  "title": "Example",
  "url": "https://github.com/owner/repo/issues/42",
  "issueState": "open",
  "state": "ready",
  "mode": "bounded",
  "priority": "p2",
  "inProgress": false,
  "blockers": [],
  "projectLabels": [],
  "reasons": []
}
```

`mode` may be null. `priority` is null only for conflicting fixed labels;
otherwise an omitted label is reported as `p2`. `body` and `bodyTruncated`
appear only on `show`.

## Envelopes

- `list`: `{"repository":"owner/repo","tasks":[...]}`
- `show` and `next`: `{"repository":"owner/repo","task":TASK_OR_NULL}`
- mutation: `{"repository":"owner/repo","changed":true,"dryRun":false,
  "changes":[...],"task":TASK_OR_NULL}`
- error: `{"error":{"code":"...","message":"...","hint":"..."}}`

A mutation change contains an `action` and optional `label`. On a confirmed
partial failure, the error may also contain `changes` and the last confirmed
`task`. Empty reads and idempotent no-op mutations are successful results.
