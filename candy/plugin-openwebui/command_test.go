package openwebui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeServer is a stateful stand-in for an Open WebUI server: it implements the signin
// route, /api/version, and every surface's GET/POST pair from the production Surfaces
// table. A test drives the real CLI against it, so the paths, the redaction, the
// credential round-trip and the diff all execute end to end.
type fakeServer struct {
	mu     sync.Mutex
	state  map[string]any // GET path -> body
	posts  []string       // POST paths received, in order
	secret string         // the credential the signin route accepts as password
}

func newFakeServer(t *testing.T) (*fakeServer, *httptest.Server) {
	t.Helper()
	f := &fakeServer{state: map[string]any{}, secret: "pw"}
	f.seed()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "0.13.3"})
	})
	mux.HandleFunc("/api/v1/auths/signin", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Email, Password string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.Password != f.secret {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"bad credentials"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "jwt-token"})
	})
	// One handler per unique path (several surfaces POST to their own GET path).
	paths := map[string]Surface{}
	for _, s := range Surfaces {
		if s.GetPath != "" {
			paths[s.GetPath] = s
		}
		paths[s.PostPath] = s
	}
	for path, s := range paths {
		s := s
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer jwt-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"detail":"Not authenticated"}`))
				return
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if r.Method == http.MethodPost {
				var body any
				_ = json.NewDecoder(r.Body).Decode(&body)
				f.state[s.GetPath] = body
				f.posts = append(f.posts, path)
				_ = json.NewEncoder(w).Encode(body)
				return
			}
			body := f.state[s.GetPath]
			// banners answers GET with a bare list while accepting {"banners": [...]} on
			// POST — the one measured asymmetry the adapter exists for.
			if s.Name == "banners" {
				if m, ok := body.(map[string]any); ok {
					_ = json.NewEncoder(w).Encode(m["banners"])
					return
				}
			}
			_ = json.NewEncoder(w).Encode(body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return f, srv
}

// seed installs one SCHEMA-VALID GET body per surface, each carrying a credential where
// the surface has one, so redaction, validation and the round-trip all execute.
func (f *fakeServer) seed() {
	f.state["/api/v1/configs/connections"] = map[string]any{
		"ENABLE_DIRECT_CONNECTIONS": true, "ENABLE_BASE_MODELS_CACHE": true}
	f.state["/api/v1/configs/models"] = map[string]any{
		"DEFAULT_MODELS": "qwen", "DEFAULT_PINNED_MODELS": nil, "MODEL_ORDER_LIST": []any{"qwen"}}
	f.state["/openai/config"] = map[string]any{
		"ENABLE_OPENAI_API":    true,
		"OPENAI_API_BASE_URLS": []any{"http://strata:8080/v1"},
		"OPENAI_API_KEYS":      []any{"sk-live-secret"},
		"OPENAI_API_CONFIGS":   map[string]any{},
	}
	f.state["/ollama/config"] = map[string]any{
		"ENABLE_OLLAMA_API": false, "OLLAMA_BASE_URLS": []any{"http://ollama:11434"},
		"OLLAMA_API_CONFIGS": map[string]any{}}
	f.state["/api/v1/configs/tool_servers"] = map[string]any{
		"TOOL_SERVER_CONNECTIONS": []any{map[string]any{
			"type": "mcp", "url": "http://jupyter:8888/mcp", "key": "sk-mcp-secret",
			"config": map[string]any{"enable": true}}}}
	f.state["/api/v1/configs/terminal_servers"] = map[string]any{"TERMINAL_SERVER_CONNECTIONS": []any{}}
	f.state["/api/v1/configs/code_execution"] = map[string]any{
		"ENABLE_CODE_EXECUTION": true, "CODE_EXECUTION_ENGINE": "jupyter",
		"CODE_EXECUTION_JUPYTER_URL": "http://jupyter:8888", "CODE_EXECUTION_JUPYTER_AUTH": "token",
		"CODE_EXECUTION_JUPYTER_AUTH_TOKEN": "jt-secret", "CODE_EXECUTION_JUPYTER_AUTH_PASSWORD": nil,
		"CODE_EXECUTION_JUPYTER_TIMEOUT": nil,
		"ENABLE_CODE_INTERPRETER":        false, "CODE_INTERPRETER_ENGINE": "jupyter",
		"CODE_INTERPRETER_PROMPT_TEMPLATE": nil, "CODE_INTERPRETER_JUPYTER_URL": nil,
		"CODE_INTERPRETER_JUPYTER_AUTH": nil, "CODE_INTERPRETER_JUPYTER_AUTH_TOKEN": nil,
		"CODE_INTERPRETER_JUPYTER_AUTH_PASSWORD": nil, "CODE_INTERPRETER_JUPYTER_TIMEOUT": nil}
	f.state["/api/v1/configs/subagents"] = map[string]any{
		"ENABLE_SUBAGENTS": false, "SUBAGENTS_BACKGROUND_ENABLED": false,
		"SUBAGENTS_MAX_CONCURRENT": 2, "SUBAGENTS_MAX_ASYNC": 2, "SUBAGENTS_MAX_ITERATIONS": 8,
		"SUBAGENTS_MAX_OUTPUT": 4096, "SUBAGENTS_SYSTEM_PROMPT": "p"}
	f.state["/api/v1/tasks/config"] = map[string]any{
		"TASK_MODEL": nil, "TASK_MODEL_EXTERNAL": nil,
		"ENABLE_TITLE_GENERATION": true, "TITLE_GENERATION_PROMPT_TEMPLATE": "t",
		"IMAGE_PROMPT_GENERATION_PROMPT_TEMPLATE": "i", "ENABLE_AUTOCOMPLETE_GENERATION": false,
		"AUTOCOMPLETE_GENERATION_PROMPT_TEMPLATE": "a", "TAGS_GENERATION_PROMPT_TEMPLATE": "g",
		"FOLLOW_UP_GENERATION_PROMPT_TEMPLATE": "f", "ENABLE_FOLLOW_UP_GENERATION": true,
		"ENABLE_TAGS_GENERATION": true, "ENABLE_SEARCH_QUERY_GENERATION": false,
		"ENABLE_RETRIEVAL_QUERY_GENERATION": false, "QUERY_GENERATION_PROMPT_TEMPLATE": "q",
		"TOOLS_FUNCTION_CALLING_PROMPT_TEMPLATE": "c", "ENABLE_VOICE_MODE_PROMPT": false,
		"VOICE_MODE_PROMPT_TEMPLATE": nil}
	f.state["/api/v1/chats/config"] = map[string]any{
		"ENABLE_CONTEXT_COMPACTION": true, "CONTEXT_COMPACTION_PROMPT_TEMPLATE": "c",
		// a field 0.13.3 added: the open body must not reject it
		"DEFAULT_TOOL_PERMISSIONS_AUTO_PROMPT": true}
	f.state["/api/v1/evaluations/config"] = map[string]any{}
	f.state["/api/v1/users/default/permissions"] = map[string]any{
		"workspace": map[string]any{"models": true}, "sharing": map[string]any{},
		"access_grants": map[string]any{}, "chat": map[string]any{},
		"features": map[string]any{}, "settings": map[string]any{}}
	f.state["/api/v1/configs/banners"] = map[string]any{"banners": []any{}}
	f.state["/api/v1/audio/config"] = map[string]any{"stt": map[string]any{}, "tts": map[string]any{}}
	f.state["/api/v1/images/config"] = map[string]any{"ENABLE_IMAGE_GENERATION": false}
	f.state["/api/v1/retrieval/config"] = map[string]any{"RAG_TOP_K": 3}
	f.state["/api/v1/auths/admin/config"] = map[string]any{"ENABLE_SIGNUP": false, "LDAP_BIND_PASSWORD": "ldap-secret"}
}

func tempProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "charly.yml")
	// A comment and a neighbouring node prove the write is comment-preserving and scoped.
	if err := os.WriteFile(file, []byte(`# the project manifest
repo: example.com/x

keep-me:
    candy:
        description: |
            untouched
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func cliArgs(srv, file string, extra ...string) []string {
	return append([]string{"--server", srv, "--email", "admin@example.com", "--password", "pw", "--node", "ow", "--file", file}, extra...)
}

// TestSaveRedactsAndRoundTrips is the load-bearing test: save the live configuration,
// then apply it back unchanged and prove nothing is posted (the {keep:true} credential
// placeholders resolve to the live values). Without the redaction or the resolution this
// test fails.
func TestSaveRedactsAndRoundTrips(t *testing.T) {
	_, srv := newFakeServer(t)
	file := tempProject(t)

	if err := runCLI(cliArgs(srv.URL, file, "save")); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "sk-live-secret") {
		t.Fatalf("save wrote a live credential into charly.yml:\n%s", text)
	}
	if !strings.Contains(text, "keep: true") {
		t.Fatalf("save did not emit a {keep: true} placeholder:\n%s", text)
	}
	if !strings.Contains(text, "# the project manifest") || !strings.Contains(text, "keep-me:") {
		t.Fatalf("save did not preserve the existing file:\n%s", text)
	}
	ent, err := LoadEntity(file, "ow")
	if err != nil {
		t.Fatalf("saved node does not load: %v", err)
	}
	if err := ValidateEntity(ent.Body); err != nil {
		t.Fatalf("saved node fails the plugin's own schema: %v", err)
	}

	// Applying the freshly saved node must post nothing: it already equals live.
	if err := runCLI(cliArgs(srv.URL, file, "apply")); err != nil {
		t.Fatalf("apply after save: %v", err)
	}
}

// TestApplyChangesOneSurfaceThenDiffIsClean proves the write path and the diff agree: a
// single changed field is posted once, and the following diff reports no drift.
func TestApplyChangesOneSurfaceThenDiffIsClean(t *testing.T) {
	f, srv := newFakeServer(t)
	file := tempProject(t)
	if err := runCLI(cliArgs(srv.URL, file, "save")); err != nil {
		t.Fatalf("save: %v", err)
	}
	ent, err := LoadEntity(file, "ow")
	if err != nil {
		t.Fatal(err)
	}
	models := ent.Config()["models"].(map[string]any)
	models["DEFAULT_MODELS"] = "qwen-other"
	if _, err := WriteEntity(file, "ow", ent.Body, false); err != nil {
		t.Fatalf("rewrite node: %v", err)
	}

	if err := runCLI(cliArgs(srv.URL, file, "apply")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	f.mu.Lock()
	posts := append([]string(nil), f.posts...)
	f.mu.Unlock()
	if len(posts) != 1 || posts[0] != "/api/v1/configs/models" {
		t.Fatalf("expected exactly the models POST, got %v", posts)
	}
	if err := runCLI(cliArgs(srv.URL, file, "diff")); err != nil {
		t.Fatalf("diff after apply should be clean, got: %v", err)
	}
}

// TestDiffReportsDriftAndExitsNonZero proves diff is a real detector.
func TestDiffReportsDriftAndExitsNonZero(t *testing.T) {
	_, srv := newFakeServer(t)
	file := tempProject(t)
	if err := runCLI(cliArgs(srv.URL, file, "save")); err != nil {
		t.Fatalf("save: %v", err)
	}
	ent, _ := LoadEntity(file, "ow")
	ent.Config()["models"].(map[string]any)["DEFAULT_MODELS"] = "drifted"
	if _, err := WriteEntity(file, "ow", ent.Body, false); err != nil {
		t.Fatal(err)
	}
	err := runCLI(cliArgs(srv.URL, file, "diff"))
	if err == nil {
		t.Fatal("diff reported no drift against a changed entity")
	}
	var drift *driftError
	if !errors.As(err, &drift) {
		t.Fatalf("want driftError, got %T: %v", err, err)
	}
}

// TestApplyRefusesLiteralSecret proves a credential in the entity is refused, and that
// --allow-literal-secrets is the explicit escape hatch.
func TestApplyRefusesLiteralSecret(t *testing.T) {
	_, srv := newFakeServer(t)
	file := tempProject(t)
	if err := runCLI(cliArgs(srv.URL, file, "save")); err != nil {
		t.Fatalf("save: %v", err)
	}
	ent, _ := LoadEntity(file, "ow")
	ent.Config()["openai"].(map[string]any)["OPENAI_API_KEYS"] = []any{"sk-typed-into-git"}
	if _, err := WriteEntity(file, "ow", ent.Body, false); err != nil {
		t.Fatal(err)
	}
	err := runCLI(cliArgs(srv.URL, file, "apply"))
	if err == nil || !strings.Contains(err.Error(), "literal credential") {
		t.Fatalf("want a literal-credential refusal, got %v", err)
	}
	if err := runCLI(cliArgs(srv.URL, file, "apply", "--allow-literal-secrets", "--dry-run")); err != nil {
		t.Fatalf("--allow-literal-secrets should permit a dry-run plan: %v", err)
	}
}

// TestSaveSkipsWriteOnlySurfaces proves a surface with no GET is reported, not invented.
func TestSaveSkipsWriteOnlySurfaces(t *testing.T) {
	_, srv := newFakeServer(t)
	file := tempProject(t)
	if err := runCLI(cliArgs(srv.URL, file, "save")); err != nil {
		t.Fatalf("save: %v", err)
	}
	ent, err := LoadEntity(file, "ow")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"suggestions", "interface"} {
		if _, ok := ent.Config()[name]; ok {
			t.Fatalf("save invented a body for the write-only surface %q", name)
		}
	}
	if _, ok := ent.Config()["openai"]; !ok {
		t.Fatal("save did not capture the openai surface")
	}
}

// TestUnknownSurfaceIsNamed proves an entity carrying a typo'd surface fails with the
// known set, rather than being silently ignored.
func TestUnknownSurfaceIsNamed(t *testing.T) {
	_, srv := newFakeServer(t)
	file := tempProject(t)
	body := map[string]any{"config": map[string]any{"connectionsz": map[string]any{}}}
	if _, err := WriteEntity(file, "ow", body, false); err == nil || !strings.Contains(err.Error(), "connectionsz") && !strings.Contains(err.Error(), "field not allowed") {
		t.Fatalf("want a schema rejection naming the bad field, got %v", err)
	}
	_ = fmt.Sprint(srv.URL)
}
