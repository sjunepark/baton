# Bundled skill distribution

`skills/baton/` is the repository source. Before distributing a release,
compare it with the registered copy rather than editing either copy by hand.
The normal personal-skill location is `$HOME/.agents/skills/baton`; older
Codex installations may use `$CODEX_HOME/skills/baton`:

```sh
distributed=${BATON_SKILL_DISTRIBUTED_PATH:-$HOME/.agents/skills/baton}
if [ ! -d "$distributed" ]; then
  distributed=${CODEX_HOME:-$HOME/.codex}/skills/baton
fi
test -d "$distributed"
diff -ru skills/baton "$distributed"
```

A nonzero result is drift and must be resolved through the owning skill
installer or distribution flow. This check is read-only; repository release
preparation does not mutate an installed copy outside the repository.
