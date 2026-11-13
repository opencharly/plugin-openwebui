# plugin-openwebui

The `charly openwebui` management CLI and the CUE-validated `kind: openwebui` configuration
entity — save a deployed Open WebUI's configuration into a project's `charly.yml`, and deploy
it back through Open WebUI's own HTTP API.

```console
$ charly openwebui status --server https://openwebui.example --email admin@example.com
server:  https://openwebui.example
version: 0.13.3

$ charly openwebui save --server https://openwebui.example --email admin@example.com
saved 17 surface(s) into charly.yml node "openwebui-config"
  skipped suggestions (write-only surface, no GET)
  skipped interface (write-only surface, no GET)

$ charly openwebui diff      # exit 1 when the live server differs from the entity
$ charly openwebui apply     # post each changed surface to its own typed endpoint
```

## The entity

```yaml
openwebui-config:
    openwebui:
        endpoint: https://openwebui.example
        auth:
            email: admin@example.com
            password_secret: charly/secret/WEBUI_ADMIN_PASSWORD
        config:
            connections:
                ENABLE_DIRECT_CONNECTIONS: true
                ENABLE_BASE_MODELS_CACHE: true
            models:
                DEFAULT_MODELS: qwen3.8-flash-next
                DEFAULT_PINNED_MODELS: null
                MODEL_ORDER_LIST: [qwen3.8-flash-next]
            openai:
                ENABLE_OPENAI_API: true
                OPENAI_API_BASE_URLS: [http://strata:8080/v1]
                OPENAI_API_KEYS: [{secret: OPENROUTER_API_KEY}]
                OPENAI_API_CONFIGS: {}
```

`config.<surface>` is the JSON body of that surface's own POST endpoint, so the names are
the server's (`ENABLE_DIRECT_CONNECTIONS`, not a friendlier re-spelling). One mapping layer
between a pretty name and the wire name would be a second contract that drifts; there is
none.

## Credentials

A credential is never written into the entity.

| Entity value | Meaning |
|---|---|
| `{keep: true}` | written by `save`: apply preserves the live value, so save→apply is a no-op |
| `{secret: OPENROUTER_API_KEY}` | look the value up in the environment at apply time |
| `"sk-literal"` | refused by `apply` unless `--allow-literal-secrets` (throwaway beds only) |

Redaction preserves the container shape: a redacted `OPENAI_API_KEYS` list stays a list of
placeholders, and the entity the CLI writes always passes the schema it serves.

## Auth

`--api-key`/`$OPENWEBUI_API_KEY` wins; otherwise the CLI signs in with
`--email`+`--password` (or the entity's `auth.email` + `auth.password_secret`). A server-side
API key needs `ENABLE_API_KEYS=true`; the sign-in path needs nothing beyond an admin account.

## Surfaces

`connections`, `models`, `openai`, `ollama`, `tool_servers`, `terminal_servers`,
`code_execution`, `subagents`, `tasks`, `chat`, `evaluations`, `user_permissions`, `banners`,
`audio`, `images`, `retrieval`, `auth` — plus the write-only `suggestions` and `interface`
(apply-only; `save` names them as skipped). Not covered by design: workspace artifacts
(models, prompts, tools, functions, knowledge) and user/group state live in their own tables
with their own APIs.

## Build

```console
$ cd candy/plugin-openwebui && GOWORK=off go test ./...
$ charly box validate          # from the repo root: the candy, ADE plan and schema splice
```
