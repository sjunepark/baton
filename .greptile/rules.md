# Baton Review Rules

## Safety Boundary

Baton automates issue and PR workflows across repositories. Treat any code that
can affect git state, GitHub state, branch policy, labels, worktrees, or agent
handoffs as safety-sensitive.

- Do not allow automation work to mutate a user's primary checkout.
- Do not allow PR merges unless a user explicitly requested the merge and the
  target repository policy allows it.
- Do not allow worktree deletion, reset, pruning, or cleanup unless Baton can
  prove the candidate is managed, idle, clean, and safe.
- GitHub Actions policy commands must run trusted Baton code, not PR-modified
  repository code.

## Implementation Shape

- Keep deterministic decisions in Go code and agent judgment in the bundled
  skill.
- Prefer typed GitHub GraphQL or REST client behavior over parsing `gh` output.
- Keep commands JSON-first; human output should wrap the same internal result
  objects.
- Model invalid states directly and return explicit errors with useful context.
- Avoid speculative compatibility layers, broad catch-all error handling, and
  hidden side effects.

## Test Expectations

- Add table-driven tests for policy parsing and decisions.
- Add tests around GitHub event fixtures and unsafe-state rejection.
- Keep live GitHub integration tests behind explicit environment gates.
- Cover dry-run and pure planner behavior for every GitHub or git mutation path.
