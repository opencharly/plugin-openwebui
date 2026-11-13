# AGENTS.md — plugin-openwebui

The `charly openwebui` management CLI and the CUE-validated `kind: openwebui` config
entity: save a deployed Open WebUI's configuration into a project's `charly.yml`, and
deploy it back through Open WebUI's own HTTP API.

The authoritative rulebook is the umbrella `AGENTS.md` in `opencharly/opencharly` — Part I
(R0–R10) and Part II. Do not restate it here; this file adds only what is specific to this
repo. `README.md` is the user overview and never carries agent guidance.

## Load these skills first (R0)

- `/charly-internals:plugin` — the provider model, the `plugin:` block, the per-plugin CUE
  schema contract, external-vs-compiled placement. **Before reading or authoring any plugin
  source.**
- `/charly-internals:go` — before any Go source work; `/charly-internals:go-quality` before
  a quality sweep.
- `/charly-check:check` — the ADE plan and the disposable bed that gates a plugin change.
- `/charly-internals:git-workflow` — before any git/PR action.

## This repo's own skill

- `.agents/skills/openwebui-cli/SKILL.md` — the CLI surface, the entity shape, the credential
  model and the surface table. **Read it before adding or changing a verb, a surface, or the
  schema.**

## What is unique here

- **External placement is deliberate and stated once**: `plugin.source` names this repo's
  own candy, so nothing is compiled into the charly binary and there is no
  `compiled_plugins:` entry and no pin in `charly/charly.yml`. Never write the placement
  into prose: the docs generator computes it.
- **Every route is verified against the server's own router** (`routers/configs.py`,
  `routers/openai.py`, `routers/tasks.py`, …) at the pinned upstream tag, never inferred from
  a CLI's shape. The surface table (`surfaces.go`) is the ONE place a route lives, and a
  guessed route fails at the first live call instead of silently doing nothing.
- **`apply` uses the per-surface typed endpoints, never the bulk import.** Upstream's
  `POST /api/v1/configs/import` is untyped, runs no per-surface side effects (model-cache
  invalidation, tool-server re-registration, MFA session revocation), and reaches its write
  through a helper named `update_mfa_config` — an accidental coupling, not a contract. This
  is a deliberate, documented choice, not an omission.
- **A credential never enters the entity.** `save` writes `{keep: true}` placeholders (shape-
  preserving: a redacted LIST stays a list of placeholders) and `apply` resolves them against
  the live body; operator-authored `{secret: <key>}` resolves from the environment; a literal
  credential is refused unless `--allow-literal-secrets`. A regression here is a leak into
  git history, so it is covered by tests that assert the saved file never contains the live
  secret.
- **The schema is the gate, and it is published.** `schema/openwebui.cue` is served over
  Describe, compiled again in-process for `save`/`apply`, and rendered on opencharly.ai — so
  it must stay self-contained (no base refs, no package clause), closed where the upstream
  form is stable and open where it churns (a closed `retrieval` body would turn an upstream
  field addition into a failed backup).
- **The charly.yml write is the sanctioned one**: comment-preserving `kit.SetByDotPath`
  against a staged temp copy, atomically renamed, with a `.charly/backups/` rollback copy
  (the plugin-migrate precedent). There is no version guard in the org's yaml helpers.
