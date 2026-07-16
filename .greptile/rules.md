# Baton review rules

- Treat `baton:managed` as the complete enrollment fact; bodies and comments
  must not affect Task classification.
- Require pure preview/apply parity, prefix-safe writes, idempotence, and
  confirmed partial-failure state for mutations.
- Preserve issue bodies and non-Baton labels.
- Validate CLI syntax before repository, authentication, and network work.
- Keep Task facts behind typed, bounded GitHub requests.
- Flag any active config/setup, policy workflow/comment, branch/PR/CI/review/
  merge, delivery, candidate/recommendation/run, or legacy output behavior.
- Treat migration fixtures and historical docs as inert evidence, not runtime
  contracts or deletion authority.
