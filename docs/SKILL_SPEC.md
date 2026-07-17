# Baton skill contract

The bundled skill in `skills/baton/` translates user intent into the Task CLI
without creating a second command manual or execution model. Live command help
is authoritative for command behavior, syntax, flags, and accepted values;
consult it with `baton COMMAND --help`. The skill owns issue-writing and
classification judgment, authorization, and cross-command sequencing.

It may:

- list, show, and select Tasks;
- create ordinary issues and explicitly enroll them;
- suggest or ask for mode, priority, and blockers;
- update classification without editing issue bodies;
- start or stop advisory activity;
- implement one Task by calling `start`, handing all project work to the
  target project's instructions/tools, and optionally calling `close` after
  explicit confirmation.

It preserves project-owned issue content and labels and delegates project work
and delivery to the target project's instructions and tools. When interaction
is unavailable after implementation, it leaves the Task open and reports the
CLI-derived close command.

The repository copy is canonical. [SKILL_DISTRIBUTION.md](SKILL_DISTRIBUTION.md)
defines the maintainer-only drift check used before distribution and remains
outside the delivered skill package.
