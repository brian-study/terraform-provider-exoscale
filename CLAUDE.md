# terraform-provider-exoscale

This is a **fork**. `origin` = `git@github.com:brian-study/terraform-provider-exoscale.git`.

All git and GitHub work targets the `brian-study/terraform-provider-exoscale` fork. Never push to,
PR against, comment on, or open issues on `exoscale/terraform-provider-exoscale` (upstream). The user
does not own upstream.

- `git push` → `origin` (the fork).
- `gh pr create` → always pass `--repo brian-study/terraform-provider-exoscale --base master`.
  Omitting `--repo` defaults to upstream via the parent-fork relationship — this is the trap.
  Always pass `--repo` explicitly.
- `gh pr close/comment/review`, `gh issue create/comment`, etc. → same: `--repo brian-study/...`.
- If the user explicitly asks to upstream something to `exoscale/...`, confirm in plain text first.
