# TODO

1. [x] Audit for Creo-specific symbols, names, and assumptions. Use `rg` to find
   occurrences of `creo`; this repository should be general-purpose rather
   than tied to the Creo project name.
2. Review the entire codebase against https://axi.md/.

## Progress Log

### 2026-06-22

- Completed item 1. Code and active examples no longer use Creo-specific
  symbols, repositories, paths, or default policy markers.
- Remaining `creo` matches are intentionally historical migration/source
  references in AGENTS.md and migration planning docs.
