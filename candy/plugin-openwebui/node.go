package openwebui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/opencharly/sdk/kit"
	"gopkg.in/yaml.v3"
)

// node.go — reading, validating and writing the `kind: openwebui` entity in a project's
// charly.yml.
//
// The entity is a FLAT kind, so its file shape is `<name>: {openwebui: <body>}`. The
// write path is the ONE the org already sanctions for a manifest edit — a
// comment-preserving yaml.Node mutation (`kit.SetByDotPath`, the helper behind
// `charly box set` and `charly candy set`) — preceded by a rollback copy under
// `.charly/backups/` (plugin-migrate's precedent) and followed by an atomic replace, so
// a crash mid-write cannot truncate the project manifest. There is NO version guard in
// the org's yaml helpers; the backup plus a `--dry-run` is what a caller gets.

// Entity is one loaded `openwebui:` body plus where it came from.
type Entity struct {
	Name string         // the node name
	File string         // the charly.yml path
	Body map[string]any // the `openwebui:` body
}

// Endpoint returns the entity's endpoint, if authored.
func (e *Entity) Endpoint() string {
	if s, ok := e.Body["endpoint"].(string); ok {
		return s
	}
	return ""
}

// Auth returns the entity's auth block, if authored.
func (e *Entity) Auth() map[string]any {
	if m, ok := e.Body["auth"].(map[string]any); ok {
		return m
	}
	return nil
}

// Config returns the entity's config block, if authored.
func (e *Entity) Config() map[string]any {
	if m, ok := e.Body["config"].(map[string]any); ok {
		return m
	}
	return nil
}

// BodyJSON renders the body for CUE validation.
func (e *Entity) BodyJSON() ([]byte, error) { return json.Marshal(e.Body) }

// LoadEntity reads `<file>` and returns the `<node>.openwebui` body. A missing file, a
// missing node, a node whose value is not a mapping, or a node that carries several
// keys all produce a named error: a half-understood entity is never applied.
func LoadEntity(file, node string) (*Entity, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", file, err)
	}
	raw, ok := doc[node]
	if !ok {
		return nil, fmt.Errorf("%s has no node %q (create it with `charly openwebui save --node %s`)", file, node, node)
	}
	nodeMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: node %q is not a mapping", file, node)
	}
	body, ok := nodeMap["openwebui"]
	if !ok {
		keys := make([]string, 0, len(nodeMap))
		for k := range nodeMap {
			keys = append(keys, k)
		}
		return nil, fmt.Errorf("%s: node %q carries no `openwebui:` kind (it has %v); the entity is `<name>: {openwebui: {…}}`", file, node, keys)
	}
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: node %q: `openwebui:` is not a mapping", file, node)
	}
	return &Entity{Name: node, File: file, Body: bodyMap}, nil
}

// ValidateEntity unifies a body against the plugin's OWN served schema
// (#OpenwebuiInput). This is the same def the host validates at load, so a node the CLI
// is willing to write is a node `charly box validate` accepts — and a typo is caught
// before a single HTTP call.
func ValidateEntity(body map[string]any) error { return validateEntityCUE(body) }

// WriteEntity validates then writes `<node>.openwebui` into `<file>`. With dryRun it
// validates and renders but writes nothing, returning the rendered YAML.
func WriteEntity(file, node string, body map[string]any, dryRun bool) (string, error) {
	if err := ValidateEntity(body); err != nil {
		return "", err
	}
	yamlBody, err := yaml.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("render entity as YAML: %w", err)
	}
	if dryRun {
		return fmt.Sprintf("%s:\n    openwebui:\n%s", node, indent(string(yamlBody), "        ")), nil
	}
	if err := backup(file); err != nil {
		return "", err
	}
	// SetByDotPath writes in place with os.WriteFile; run it against a temp copy and
	// atomically rename, so the project manifest is never half-written.
	dir := filepath.Dir(file)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(file)+".openwebui.*")
	if err != nil {
		return "", fmt.Errorf("stage %s: %w", file, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	src, err := os.ReadFile(file)
	if err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("read %s: %w", file, err)
	}
	if _, err := tmp.Write(src); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("stage %s: %w", file, err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("stage %s: %w", file, err)
	}
	if err := kit.SetByDotPath(tmpName, node+".openwebui", string(yamlBody)); err != nil {
		return "", fmt.Errorf("set %s.openwebui: %w", node, err)
	}
	out, err := os.ReadFile(tmpName)
	if err != nil {
		return "", fmt.Errorf("read staged %s: %w", file, err)
	}
	if err := kit.AtomicWriteFile(file, out, 0o644); err != nil {
		return "", err
	}
	return string(out), nil
}

// backup copies `file` to `<root>/.charly/backups/<rel>.<unixnano>` and guarantees the
// backup tree is gitignored — the plugin-migrate precedent, so a rewrite is always
// reversible without a git operation.
func backup(file string) error {
	abs, err := filepath.Abs(file)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", file, err)
	}
	root := filepath.Dir(abs)
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		rel = filepath.Base(abs)
	}
	dir := filepath.Join(root, ".charly", "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	gitignore := filepath.Join(root, ".charly", ".gitignore")
	if _, err := os.Stat(gitignore); os.IsNotExist(err) {
		if werr := os.WriteFile(gitignore, []byte("*\n"), 0o644); werr != nil {
			return fmt.Errorf("write %s: %w", gitignore, werr)
		}
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("read %s for backup: %w", file, err)
	}
	dst := filepath.Join(dir, fmt.Sprintf("%s.%d", rel, time.Now().UnixNano()))
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("write backup %s: %w", dst, err)
	}
	return nil
}

func indent(s, prefix string) string {
	out := ""
	for _, line := range splitLines(s) {
		if line == "" {
			out += "\n"
			continue
		}
		out += prefix + line + "\n"
	}
	return out
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
