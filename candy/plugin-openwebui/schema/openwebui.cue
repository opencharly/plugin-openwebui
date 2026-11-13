// plugin-openwebui's OWN self-contained CUE schema — the SINGLE SOURCE for the
// `kind: openwebui` entity's authored surface.
//
// SELF-CONTAINED and PACKAGE-LESS: it references no base def and carries no package
// clause, so it compiles STANDALONE (the property the SDK's serve-side compile needs,
// and the property that lets the host splice `base ++ plugin` at the load gate). A
// self-contained schema that will not splice is a LOUD load failure
// (`TestExternalSchemaSelfContained`).
//
// WHAT THIS ENTITY IS. One `openwebui:` node in a project's `charly.yml` declares the
// DESIRED configuration of one deployed Open WebUI server:
//
//     openwebui-config:
//         openwebui:
//             endpoint: https://openwebui.example
//             auth: {email: admin@example}
//             config: {…}
//
// `charly openwebui save` fills that node from a live server (secrets become
// `{keep: true}`), and `charly openwebui apply` posts it back. The entity is FLAT
// (no deploy members): its body rides op.Params, is validated here at load time, and
// is folded into `uf.PluginKinds["openwebui"][name]` as opaque JSON that only this
// plugin reads.
//
// THE FIELD NAMES ARE THE SERVER'S OWN. Each `config.<surface>` body is the JSON body
// of that surface's own POST endpoint (`POST /api/v1/configs/connections`,
// `POST /openai/config/update`, …), so `save` is a GET→node copy and `apply` is a
// node→POST copy with NO translation table that could drift from the server. The one
// deliberate exception is a credential leaf, which is a `#Secret` instead of a string
// (see below). This is why the keys read `ENABLE_DIRECT_CONNECTIONS` rather than a
// friendlier re-spelling: a prettier name would need a mapping layer, and a mapping
// layer is a second contract that drifts silently (R2/R3).
//
// TYPED vs VERSION-COUPLED. A surface whose form is small and stable is a CLOSED def,
// so a typo is a load error. A surface whose upstream form is large and churns
// (`retrieval` 66 fields, `auth` 38, `images` 33, `audio` three nested forms) is
// carried as an OPEN object: closing it would turn an upstream field addition into a
// failed `save`, which is the opposite of what a backup tool must do. The tradeoff is
// named per surface rather than left implicit.

// #OpenwebuiInput — the authored entity body.
#OpenwebuiInput: close({
	// endpoint — the server root, e.g. https://a.example:40045. Optional: the CLI's
	// --server flag and $OPENWEBUI_URL answer it too, with that precedence
	// (flag > entity > env).
	endpoint?: string & =~"^https?://"

	auth?: #Auth

	config?: #Config
})

// #Auth — who the CLI signs in as. It carries REFERENCES to credentials, never a
// credential: a `*_secret` value is a charly credential KEY (the name a
// `SECRET_*`-style variable or the credential store holds), resolved at apply time.
// A password written inline is rejected by `apply` unless --allow-literal-secrets.
#Auth: close({
	email!: string & =~"@"
	password_secret?: string & !=""
	api_key_secret?: string & !=""
})

// #Secret — one credential-bearing leaf.
//
//	string                      a literal value (apply refuses it unless
//	                            --allow-literal-secrets; save never writes one)
//	{secret: "<credential-key>"} resolve that key at apply time
//	{keep: true}                saved by `save`: the live value is preserved as-is,
//	                            so a save→apply round-trip changes nothing
#Secret: string | close({secret: string & !=""}) | close({keep: true})

// #Config — the per-surface bodies, keyed by the CLI's surface names.
#Config: close({
	connections?:      #Connections
	models?:           #Models
	openai?:           #OpenAI
	ollama?:           #Ollama
	tool_servers?:     #ToolServers
	terminal_servers?: #TerminalServers
	code_execution?:   #CodeExecution
	subagents?:        #Subagents
	tasks?:            #Tasks
	chat?:             #VersionCoupled
	evaluations?:      #Evaluations
	user_permissions?: #UserPermissions
	banners?:          #Banners
	suggestions?:      #Suggestions
	interface?:        #Interface

	// Version-coupled surfaces (open bodies; see the header).
	audio?:     #VersionCoupled
	images?:    #VersionCoupled
	retrieval?: #VersionCoupled
	auth?:      #VersionCoupled
})

// #VersionCoupled — a body carried verbatim because upstream's form is large and
// changes between releases. Not closed on purpose: a closed def here would make the
// first upstream field addition break `save`.
#VersionCoupled: {...}

// ---------------------------------------------------------------------------
// Typed surfaces — closed, field-for-field with the server's own POST body.
// ---------------------------------------------------------------------------

// POST /api/v1/configs/connections
#Connections: close({
	ENABLE_DIRECT_CONNECTIONS!:  bool
	ENABLE_DIRECT_INTEGRATIONS?: bool
	ENABLE_BASE_MODELS_CACHE!:   bool
})

// POST /api/v1/configs/models
#Models: close({
	DEFAULT_MODELS!:         string | null
	DEFAULT_PINNED_MODELS!:  string | null
	MODEL_ORDER_LIST!:       [...string] | null
	DEFAULT_MODEL_METADATA?: {[string]: _} | null
	DEFAULT_MODEL_PARAMS?:   {[string]: _} | null
})

// POST /openai/config/update
#OpenAI: close({
	ENABLE_OPENAI_API?:   bool | null
	OPENAI_API_BASE_URLS!: [...string]
	OPENAI_API_KEYS!:     [...#Secret]
	OPENAI_API_CONFIGS!:  {[string]: _}
})

// POST /ollama/config/update
#Ollama: close({
	ENABLE_OLLAMA_API?:   bool | null
	OLLAMA_BASE_URLS!:    [...string]
	OLLAMA_API_CONFIGS!: {[string]: _}
})

// POST /api/v1/configs/tool_servers
// The connection elements are OPEN objects: upstream's ToolServerConnection is
// permissive (extra='allow') and gains keys between releases, so a closed element
// would reject a saved body. `config.enable` is the one load-bearing key and is
// documented by the skill, not pinned here.
#ToolServers: close({
	TOOL_SERVER_CONNECTIONS!: [...{...}]
})

// POST /api/v1/configs/terminal_servers
#TerminalServers: close({
	TERMINAL_SERVER_CONNECTIONS!: [...{...}]
})

// POST /api/v1/configs/code_execution
#CodeExecution: close({
	ENABLE_CODE_EXECUTION!:          bool
	CODE_EXECUTION_ENGINE!:          string
	CODE_EXECUTION_JUPYTER_URL!:     string | null
	CODE_EXECUTION_JUPYTER_AUTH!:    string | null
	CODE_EXECUTION_JUPYTER_AUTH_TOKEN!:    #Secret | null
	CODE_EXECUTION_JUPYTER_AUTH_PASSWORD!: #Secret | null
	CODE_EXECUTION_JUPYTER_TIMEOUT!: int | null
	ENABLE_CODE_INTERPRETER!:        bool
	CODE_INTERPRETER_ENGINE!:        string
	CODE_INTERPRETER_PROMPT_TEMPLATE!: string | null
	CODE_INTERPRETER_JUPYTER_URL!:   string | null
	CODE_INTERPRETER_JUPYTER_AUTH!:  string | null
	CODE_INTERPRETER_JUPYTER_AUTH_TOKEN!:    #Secret | null
	CODE_INTERPRETER_JUPYTER_AUTH_PASSWORD!: #Secret | null
	CODE_INTERPRETER_JUPYTER_TIMEOUT!: int | null
})

// POST /api/v1/configs/subagents
//
// The four limits are NULLABLE in 0.13.3: a fresh deployment answers them as null (measured
// against a disposable 0.13.3 server), so requiring a concrete int would reject the server's
// own body — the exact mismatch the live schema test exists to catch.
#Subagents: close({
	ENABLE_SUBAGENTS!:            bool
	SUBAGENTS_BACKGROUND_ENABLED!: bool
	SUBAGENTS_MAX_CONCURRENT!:    int | null
	SUBAGENTS_MAX_ASYNC!:         int | null
	SUBAGENTS_MAX_ITERATIONS!:    int | null
	SUBAGENTS_MAX_OUTPUT!:        int | null
	SUBAGENTS_SYSTEM_PROMPT!:     string
})

// POST /api/v1/tasks/config/update
#Tasks: close({
	TASK_MODEL!:                              string | null
	TASK_MODEL_EXTERNAL!:                     string | null
	TASK_MODEL_PARAMS?:                       {[string]: _} | null
	ENABLE_TITLE_GENERATION!:                 bool
	TITLE_GENERATION_PROMPT_TEMPLATE!:        string
	IMAGE_PROMPT_GENERATION_PROMPT_TEMPLATE!: string
	ENABLE_AUTOCOMPLETE_GENERATION!:          bool
	AUTOCOMPLETE_GENERATION_INPUT_MAX_LENGTH?: int | null
	AUTOCOMPLETE_GENERATION_PROMPT_TEMPLATE!: string
	TAGS_GENERATION_PROMPT_TEMPLATE!:         string
	FOLLOW_UP_GENERATION_PROMPT_TEMPLATE!:    string
	ENABLE_FOLLOW_UP_GENERATION!:             bool
	ENABLE_TAGS_GENERATION!:                  bool
	ENABLE_SEARCH_QUERY_GENERATION!:          bool
	ENABLE_RETRIEVAL_QUERY_GENERATION!:       bool
	QUERY_GENERATION_PROMPT_TEMPLATE!:        string
	TOOLS_FUNCTION_CALLING_PROMPT_TEMPLATE!:  string
	ENABLE_VOICE_MODE_PROMPT!:                bool
	VOICE_MODE_PROMPT_TEMPLATE!:              string | null
})

// POST /api/v1/evaluations/config
#Evaluations: close({
	ENABLE_EVALUATION_ARENA_MODELS?: bool | null
	EVALUATION_ARENA_MODELS?:        [...{[string]: _}] | null
})

// POST /api/v1/users/default/permissions — six permission families of booleans.
#UserPermissions: close({
	workspace:     {[string]: bool}
	sharing:       {[string]: bool}
	access_grants: {[string]: bool}
	chat:          {[string]: bool}
	features:      {[string]: bool}
	settings:      {[string]: bool}
})

// POST /api/v1/configs/banners
#Banners: close({
	banners!: [...{...}]
})

// POST /api/v1/configs/suggestions
#Suggestions: close({
	suggestions!: [...{...}] | null
	i18n?:        {[string]: _} | null
})

// POST /api/v1/configs/interface/visibility — added in Open WebUI 0.13.3 (absent in
// 0.12.3), so `apply` treats it as unavailable when the server answers 404.
#Interface: close({
	visibility!: {[string]: bool}
})
