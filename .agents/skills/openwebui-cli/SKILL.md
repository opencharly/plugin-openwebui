---
name: openwebui-cli
description: >-
  The `charly openwebui` management CLI and the CUE-validated `kind: openwebui` config
  entity — save a deployed Open WebUI's configuration into a project's charly.yml and
  deploy it back through Open WebUI's own HTTP API. Use when saving or applying Open WebUI
  configuration (models, provider connections, tool servers, code execution, RAG, audio,
  tasks), when authoring an `openwebui:` entity, or when diffing a deployment against its
  declared config.
---


# openwebui-cli — configuration as code for a deployed Open WebUI

## What this plugin is

Two capabilities over one object:

| Capability | Surface |
|---|---|
| `command:openwebui` | `charly openwebui status\|save\|apply\|diff\|export` |
| `kind:openwebui` | the flat `<name>: {openwebui: …}` entity in a project's `charly.yml` |

`save` captures a live server into the entity; `apply` posts the entity back; `diff` answers
"is the deployment what the file says?"; `status` reads the server's own version; `export`
dumps the raw config table (a credential-bearing backup).

## The entity is the server's own wire shape

Each `config.<surface>` body is the JSON body of that surface's POST endpoint. The field
names are therefore the server's (`ENABLE_DIRECT_CONNECTIONS`, `OPENAI_API_KEYS`,
`TOOL_SERVER_CONNECTIONS`) — never a friendlier re-spelling, because a mapping layer between
a pretty name and the wire name is a second contract that drifts.

```yaml
openwebui-config:
    openwebui:
        endpoint: https://openwebui.example
        auth: {email: admin@example.com, password_secret: charly/secret/WEBUI_ADMIN_PASSWORD}
        config:
            models:
                DEFAULT_MODELS: qwen3.8-flash-next
                DEFAULT_PINNED_MODELS: null
                MODEL_ORDER_LIST: [qwen3.8-flash-next]
```

## Surfaces

`connections`, `models`, `openai`, `ollama`, `tool_servers`, `terminal_servers`,
`code_execution`, `subagents`, `tasks`, `chat`, `evaluations`, `user_permissions`, `banners`,
`audio`, `images`, `retrieval`, `auth`, plus write-only `suggestions` and `interface`.

`connections`, `models`, `openai`, `ollama`, `tool_servers`, `terminal_servers`,
`code_execution`, `subagents`, `tasks`, `chat`, `evaluations`, `user_permissions`, `banners`
and `interface` are CLOSED in the schema (a typo is a load error). `audio`, `images`,
`retrieval` and `auth` are OPEN bodies: their upstream forms are large and churn, and a
closed def there would turn an upstream field addition into a failed backup.

Not covered by design: workspace artifacts (models, prompts, tools, functions, knowledge) and
user/group state — they live in their own tables with their own APIs.

## Credentials

| Entity value | Meaning |
|---|---|
| `{keep: true}` | written by `save`; `apply` preserves the live value, so save→apply is a no-op |
| `{secret: <credential-key>}` | resolved from the environment at apply time (exact key, else its basename) |
| `"literal"` | refused by `apply` unless `--allow-literal-secrets` |

Redaction is SHAPE-PRESERVING: a redacted `OPENAI_API_KEYS` list stays a list of
placeholders, so an entity the CLI writes always passes the schema it serves. Review a saved
entity before committing it — the redactor keys on field NAMES, and a future field with an
unusual name is a field it will not recognise.

## Auth precedence

1. `--api-key` / `$OPENWEBUI_API_KEY` (needs `ENABLE_API_KEYS=true` on the server, else every
   call answers 403);
2. `--email` + `--password`, or the entity's `auth.email` + `auth.password_secret`
   (JWT sign-in; default lifetime 4 weeks).

`--server` beats the entity's `endpoint:`, which beats `$OPENWEBUI_URL`.

## Why `apply` never calls `POST /api/v1/configs/import`

Upstream's bulk import is untyped (any key is stored as-is), runs NO per-surface side effect
(model-cache invalidation, tool-server re-registration, MFA session revocation), and reaches
its write through a helper named `update_mfa_config` — an accidental coupling that a future
release can remove without notice. `apply` therefore posts each surface to its own typed
endpoint, which validates the payload (422 on a bad body) and runs the side effect. `export`
still reads the raw table, because a backup should be complete rather than typed.

## Common failures

| Symptom | Cause / fix |
|---|---|
| `HTTP 401 (not authenticated…)` | no credentials; pass `--api-key` or `--email`/`--password` |
| `HTTP 403 (… auth.enable_api_keys=true …)` | an API key was used on a server that disables API keys — sign in with email/password, or set the env var |
| `the server answers without a token (MFA challenge?)` | MFA is on; use an API key |
| `surface X: HTTP 404` | that server version does not serve the surface (e.g. `interface` before 0.13.3) — remove it from the entity |
| `entity fails #OpenwebuiInput: … field not allowed` | a typo'd or unknown field; the schema is closed for that surface |
| `save` skipped a surface | it is write-only (no GET); author it by hand if you want it managed |

## Verification

```console
$ charly openwebui status --server … --email …      # the server's own version
$ charly openwebui save   --server … --email … --dry-run   # render + validate, write nothing
$ charly openwebui diff                                  # exit 1 on drift
$ charly openwebui apply --dry-run                       # the plan, no posts
```
