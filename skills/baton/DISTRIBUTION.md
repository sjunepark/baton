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
installer or distribution flow. For the global personal registration managed
by the `skills` CLI, refresh only Baton from the canonical repository:

```sh
npx skills add https://github.com/sjunepark/baton/tree/main/skills --list
npx skills add https://github.com/sjunepark/baton/tree/main/skills \
  --skill baton --copy -g -a codex -y
```

Then rerun the comparison above. This command intentionally replaces the
registered copy and updates its lock entry; the comparison itself remains
read-only.
