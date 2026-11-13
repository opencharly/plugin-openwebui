package openwebui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// surfaces.go — the ONE table that says which Open WebUI configuration surfaces this CLI
// manages, plus the two transforms a credential leaf needs on the way out and in.
//
// A surface is a (name, GET path, POST path) triple where `name` is the key under
// `config:` in the charly.yml entity, and the GET response body is the POST request
// body for that surface. That symmetry is what lets `save` be a GET→node copy and
// `apply` a node→POST copy with no translation table in between.
//
// Every path below was read from the pinned upstream source (routers/configs.py,
// routers/openai.py, routers/ollama.py, routers/tasks.py, routers/chats.py,
// routers/evaluations.py, routers/users.py, routers/auths.py, routers/audio/__init__.py,
// routers/images.py, routers/retrieval.py) — never inferred from a CLI's shape. A route
// that does not exist fails loudly at the first live call instead of silently doing
// nothing, which is the failure mode a guessed path produces.

// Surface is one managed configuration surface.
type Surface struct {
	// Name is the key under `config:` in the openwebui entity.
	Name string
	// GetPath is the GET route, or "" when the surface is write-only (the server
	// accepts a POST but publishes no GET for it).
	GetPath string
	// PostPath is the POST route that applies the surface.
	PostPath string
	// NormalizeGet adapts a GET body into the POST body shape. Most surfaces
	// answer GET with exactly what they accept on POST; `banners` is the measured
	// exception (GET returns a bare list, POST takes {"banners": [...]}), and the
	// difference is the server's, not ours to wish away.
	NormalizeGet func(any) any
}

// normalize applies the surface's GET->POST adapter (identity when there is none).
func (s Surface) normalize(v any) any {
	if s.NormalizeGet == nil {
		return v
	}
	return s.NormalizeGet(v)
}

// wrapBanners adapts GET /api/v1/configs/banners (a bare list) to the
// SetBannersForm body POST /api/v1/configs/banners accepts.
func wrapBanners(v any) any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{"banners": v}
}

// Surfaces is the managed set, in a stable order (save writes them in this order and
// diff reports in this order, so two runs are comparable).
var Surfaces = []Surface{
	{Name: "connections", GetPath: "/api/v1/configs/connections", PostPath: "/api/v1/configs/connections"},
	{Name: "models", GetPath: "/api/v1/configs/models", PostPath: "/api/v1/configs/models"},
	{Name: "openai", GetPath: "/openai/config", PostPath: "/openai/config/update"},
	{Name: "ollama", GetPath: "/ollama/config", PostPath: "/ollama/config/update"},
	{Name: "tool_servers", GetPath: "/api/v1/configs/tool_servers", PostPath: "/api/v1/configs/tool_servers"},
	{Name: "terminal_servers", GetPath: "/api/v1/configs/terminal_servers", PostPath: "/api/v1/configs/terminal_servers"},
	{Name: "code_execution", GetPath: "/api/v1/configs/code_execution", PostPath: "/api/v1/configs/code_execution"},
	{Name: "subagents", GetPath: "/api/v1/configs/subagents", PostPath: "/api/v1/configs/subagents"},
	{Name: "tasks", GetPath: "/api/v1/tasks/config", PostPath: "/api/v1/tasks/config/update"},
	{Name: "chat", GetPath: "/api/v1/chats/config", PostPath: "/api/v1/chats/config"},
	{Name: "evaluations", GetPath: "/api/v1/evaluations/config", PostPath: "/api/v1/evaluations/config"},
	{Name: "user_permissions", GetPath: "/api/v1/users/default/permissions", PostPath: "/api/v1/users/default/permissions"},
	{Name: "banners", GetPath: "/api/v1/configs/banners", PostPath: "/api/v1/configs/banners", NormalizeGet: wrapBanners},
	{Name: "suggestions", GetPath: "", PostPath: "/api/v1/configs/suggestions"},
	{Name: "interface", GetPath: "", PostPath: "/api/v1/configs/interface/visibility"},
	{Name: "audio", GetPath: "/api/v1/audio/config", PostPath: "/api/v1/audio/config/update"},
	{Name: "images", GetPath: "/api/v1/images/config", PostPath: "/api/v1/images/config/update"},
	{Name: "retrieval", GetPath: "/api/v1/retrieval/config", PostPath: "/api/v1/retrieval/config/update"},
	{Name: "auth", GetPath: "/api/v1/auths/admin/config", PostPath: "/api/v1/auths/admin/config"},
}

// surfaceByName indexes Surfaces.
var surfaceByName = func() map[string]Surface {
	m := make(map[string]Surface, len(Surfaces))
	for _, s := range Surfaces {
		m[s.Name] = s
	}
	return m
}()

// LookupSurface resolves a `config:` key.
func LookupSurface(name string) (Surface, bool) {
	s, ok := surfaceByName[name]
	return s, ok
}

// SurfaceNames lists the managed surface keys, sorted, for error messages.
func SurfaceNames() []string {
	out := make([]string, 0, len(Surfaces))
	for _, s := range Surfaces {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

// keepMarker is the JSON shape `save` writes for a credential leaf: "preserve whatever
// the server has, do not round-trip the secret through a file". A save→apply cycle is
// therefore a no-op, which is what makes `apply` safe to run on a node produced by
// `save`.
func keepMarker() map[string]any { return map[string]any{"keep": true} }

// SecretNameRE matches a field NAME that can carry a credential. It is a name
// heuristic, and it is deliberately broad: a missed secret leaks into charly.yml (which
// is committed), while a false positive only forces the operator to write `{secret: …}`
// or re-enter a value. The README documents that a saved node is reviewed before it is
// committed.
var SecretNameRE = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd|authorization|headers|_key$|^key$)`)

// isSecretField reports whether a decoded JSON object key names a credential leaf.
func isSecretField(key string) bool { return SecretNameRE.MatchString(key) }

// redact returns a copy of v with every credential-bearing leaf replaced by
// {"keep": true}. Values that are empty ("" / nil / empty container) are left alone: an
// unset credential is not a secret, and preserving the empty value keeps the node
// readable.
func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if isSecretField(k) {
				out[k] = redactValue(val)
				continue
			}
			out[k] = redact(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redact(val)
		}
		return out
	default:
		return v
	}
}

// redactValue replaces every STRING leaf of a credential-bearing value with
// {"keep": true}, preserving the surrounding shape. Shape preservation is what lets the
// schema stay honest: `OPENAI_API_KEYS` is a LIST, so a redacted list must remain a list
// of placeholders (`[{keep: true}]`), never a bare placeholder — otherwise `save` would
// write an entity its own schema (rightly) rejects. Non-string scalars are left alone: a
// credential is text, and a bool under a token-ish name is a setting, not a secret.
func redactValue(v any) any {
	switch t := v.(type) {
	case string:
		if t == "" {
			return t
		}
		return keepMarker()
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = redactValue(x)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = redactValue(x)
		}
		return out
	default:
		return v
	}
}

// SecretNotFoundError names a credential key the apply could not resolve. It is a
// distinct type so the CLI can report every missing key at once instead of stopping at
// the first.
type SecretNotFoundError struct{ Key string }

func (e *SecretNotFoundError) Error() string {
	return fmt.Sprintf("credential %q is not set in the environment (export it, or write the value with --allow-literal-secrets for a throwaway bed)", e.Key)
}

// resolveSecrets returns a copy of desired with every credential placeholder filled in:
//   - {"keep": true}       -> the LIVE value at the same path (save's round-trip), or
//     absent when the live body has none
//   - {"secret": "<key>"}  -> lookup("<key>")
//
// A literal string in a credential-NAMED field is REJECTED unless allowLiteral, because
// a literal secret in charly.yml is a secret in git history.
func resolveSecrets(desired, live any, lookup func(string) (string, error), allowLiteral bool) (any, error) {
	return resolveAt(desired, live, lookup, allowLiteral, "")
}

func resolveAt(desired, live any, lookup func(string) (string, error), allowLiteral bool, path string) (any, error) {
	switch t := desired.(type) {
	case map[string]any:
		if len(t) == 1 {
			if _, ok := t["keep"]; ok {
				// Preserve the live value wholesale: {keep:true} may mark an object
				// (e.g. `headers`), not only a scalar.
				return deepCopy(live), nil
			}
			if raw, ok := t["secret"]; ok {
				key, _ := raw.(string)
				if strings.TrimSpace(key) == "" {
					return nil, fmt.Errorf("%s: {secret: …} needs a non-empty credential key", path)
				}
				val, err := lookup(key)
				if err != nil {
					return nil, err
				}
				return val, nil
			}
		}
		lm, _ := live.(map[string]any)
		out := make(map[string]any, len(t))
		for k, val := range t {
			var lv any
			if lm != nil {
				lv = lm[k]
			}
			rv, err := resolveAt(val, lv, lookup, allowLiteral, joinPath(path, k))
			if err != nil {
				return nil, err
			}
			out[k] = rv
		}
		return out, nil
	case []any:
		la, _ := live.([]any)
		out := make([]any, len(t))
		for i, val := range t {
			var lv any
			if i < len(la) {
				lv = la[i]
			}
			rv, err := resolveAt(val, lv, lookup, allowLiteral, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out[i] = rv
		}
		return out, nil
	case string:
		// An EMPTY credential is not a secret: a server that has no key configured
		// answers "", and round-tripping that must not demand an escape hatch.
		if t != "" && isSecretField(lastSeg(path)) && !allowLiteral {
			return nil, fmt.Errorf("%s: refusing a literal credential value in the entity (use {secret: <key>} or --allow-literal-secrets)", path)
		}
		return t, nil
	default:
		return deepCopy(desired), nil
	}
}

// deepCopy clones a decoded JSON value.
func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = deepCopy(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = deepCopy(val)
		}
		return out
	default:
		return v
	}
}

func joinPath(p, k string) string {
	if p == "" {
		return k
	}
	return p + "." + k
}

// DiffPaths lists the dotted paths at which two decoded JSON values differ, bounded so a
// large retrieval body does not print a wall of text. A credential-named leaf reports
// only that it differs — never its value.
func DiffPaths(a, b any, prefix string) []string {
	const maxPaths = 40
	var out []string
	var walk func(pa, pb string, x, y any)
	walk = func(pa, pb string, x, y any) {
		if len(out) >= maxPaths {
			return
		}
		if isSecretField(lastSeg(pa)) || isSecretField(lastSeg(pb)) {
			if !equalJSON(x, y) {
				out = append(out, pa+" (secret)")
			}
			return
		}
		xm, xok := x.(map[string]any)
		ym, yok := y.(map[string]any)
		if xok && yok {
			keys := make(map[string]struct{}, len(xm)+len(ym))
			for k := range xm {
				keys[k] = struct{}{}
			}
			for k := range ym {
				keys[k] = struct{}{}
			}
			sorted := make([]string, 0, len(keys))
			for k := range keys {
				sorted = append(sorted, k)
			}
			sort.Strings(sorted)
			for _, k := range sorted {
				walk(joinPath(pa, k), joinPath(pb, k), xm[k], ym[k])
			}
			return
		}
		if !equalJSON(x, y) {
			out = append(out, pa)
		}
	}
	walk(prefix, prefix, a, b)
	return out
}

// lastSeg returns the FIELD NAME a dotted/indexed path ends in: `a.b[0]` -> `b`, so a
// credential-named list reports under its own key (`OPENAI_API_KEYS[0]` is still
// OPENAI_API_KEYS). Without the index strip, an array element's path segment is the
// index and the credential check misses.
func lastSeg(p string) string {
	if strings.HasSuffix(p, "]") {
		if i := strings.LastIndex(p, "["); i >= 0 {
			p = p[:i]
		}
	}
	if i := strings.LastIndex(p, "."); i >= 0 {
		return p[i+1:]
	}
	return p
}

func equalJSON(a, b any) bool {
	aj, err1 := json.Marshal(a)
	bj, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(aj) == string(bj)
}
