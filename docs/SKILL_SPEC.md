# Baton skill contract

The bundled skill in `skills/baton/` translates user intent into the Task CLI
without creating a second execution model.

It may:

- list, show, and select Tasks;
- create ordinary issues and explicitly enroll them;
- suggest or ask for mode, priority, and blockers;
- update classification without editing issue bodies;
- start or stop advisory activity;
- implement one Task by calling `start`, handing all project work to the
  target project's instructions/tools, and optionally calling `close` after
  explicit confirmation.

It must not make comments authoritative or prescribe checkout, branch, commit,
pull-request, CI, review, merge, release, scheduler, or delivery behavior.
When interaction is unavailable after implementation, it leaves the Task open
and reports `baton close ISSUE`.

The repository copy is canonical. `skills/baton/DISTRIBUTION.md` defines the
read-only drift check used before distribution.
