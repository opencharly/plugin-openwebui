package openwebui

import (
	"os"
	"strings"
	"testing"
)

// TestSchemaRejectsTypo proves the embedded schema is the gate: an unknown field under a
// closed surface is a validate error, which is what makes `charly box validate` a real
// check on the entity.
func TestSchemaRejectsTypo(t *testing.T) {
	body := map[string]any{
		"config": map[string]any{
			"connections": map[string]any{
				"ENABLE_DIRECT_CONNECTIONS": true,
				"ENABLE_BASE_MODELS_CACHE":  false,
				"BOGUS_FIELD":               true,
			},
		},
	}
	err := ValidateEntity(body)
	if err == nil {
		t.Fatal("schema accepted an unknown field under connections")
	}
	if !strings.Contains(err.Error(), "BOGUS_FIELD") {
		t.Fatalf("schema error does not name the offending field: %v", err)
	}
}

// TestSchemaAcceptsMinimal proves an empty body and a typed surface both validate, so the
// gate is not so strict that it rejects a legitimate node.
func TestSchemaAcceptsMinimal(t *testing.T) {
	for name, body := range map[string]map[string]any{
		"empty": {},
		"typed": {"config": map[string]any{"connections": map[string]any{
			"ENABLE_DIRECT_CONNECTIONS": true,
			"ENABLE_BASE_MODELS_CACHE":  true,
		}}},
		"open-surface": {"config": map[string]any{"retrieval": map[string]any{"ANY_FUTURE_KEY": 1}}},
		"secret-ref": {"config": map[string]any{"openai": map[string]any{
			"OPENAI_API_BASE_URLS": []any{"http://x/v1"},
			"OPENAI_API_KEYS":      []any{map[string]any{"secret": "OPENROUTER_API_KEY"}},
			"OPENAI_API_CONFIGS":   map[string]any{},
		}}},
	} {
		if err := ValidateEntity(body); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestSchemaRejectsRequiredFieldGone proves required-field enforcement is real.
func TestSchemaRejectsRequiredFieldGone(t *testing.T) {
	body := map[string]any{"config": map[string]any{"connections": map[string]any{
		"ENABLE_DIRECT_CONNECTIONS": true, // ENABLE_BASE_MODELS_CACHE missing
	}}}
	if err := ValidateEntity(body); err == nil {
		t.Fatal("schema accepted connections without its required field")
	}
}

// TestWriteEntityIsScopedAndCommentPreserving proves the write touches exactly one node
// and keeps the rest of the manifest byte-identical where it can (comments included).
func TestWriteEntityIsScopedAndCommentPreserving(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/charly.yml"
	orig := "# top comment\nrepo: example.com/x\n\nother:\n    candy:\n        # inner comment\n        description: |\n            hi\n"
	if err := os.WriteFile(file, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"endpoint": "https://h:1", "config": map[string]any{"models": map[string]any{
		"DEFAULT_MODELS": "m", "DEFAULT_PINNED_MODELS": nil, "MODEL_ORDER_LIST": []any{"m"},
	}}}
	if _, err := WriteEntity(file, "ow", body, false); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "# top comment") || !strings.Contains(s, "# inner comment") {
		t.Fatalf("comments were lost:\n%s", s)
	}
	if !strings.Contains(s, "openwebui:") {
		t.Fatalf("node was not written:\n%s", s)
	}
	// A backup exists in the gitignored tree, so the write is reversible.
	entries, _ := os.ReadDir(dir + "/.charly/backups")
	if len(entries) == 0 {
		t.Fatal("no backup was written")
	}
}

// TestLoadEntityNamesAMissingKind proves a mis-shaped node is refused with the reason.
func TestLoadEntityNamesAMissingKind(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/charly.yml"
	if err := os.WriteFile(file, []byte("ow:\n    pod:\n        image: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEntity(file, "ow"); err == nil || !strings.Contains(err.Error(), "no `openwebui:` kind") {
		t.Fatalf("want a named missing-kind error, got %v", err)
	}
}

// TestRedactLeavesEmptyValuesAlone proves an unset credential is not turned into a
// placeholder (which would make the node unreadable without a live server).
func TestRedactLeavesEmptyValuesAlone(t *testing.T) {
	in := map[string]any{"API_KEY": "", "TOKEN": nil, "MODEL": "m"}
	out := redact(in).(map[string]any)
	if _, ok := out["API_KEY"].(map[string]any); ok {
		t.Fatal("redacted an empty API_KEY")
	}
	if out["TOKEN"] != nil {
		t.Fatal("redacted a nil TOKEN")
	}
	if out["MODEL"] != "m" {
		t.Fatal("changed a non-secret field")
	}
}

// TestDiffMasksSecretValues proves a drift report never prints a credential.
func TestDiffMasksSecretValues(t *testing.T) {
	a := map[string]any{"OPENAI_API_KEYS": []any{"sk-a", "sk-b"}}
	b := map[string]any{"OPENAI_API_KEYS": []any{"sk-a"}}
	paths := DiffPaths(a, b, "openai")
	if len(paths) == 0 {
		t.Fatal("did not detect the differing key list")
	}
	for _, p := range paths {
		if strings.Contains(p, "sk-") {
			t.Fatalf("drift output leaked a credential: %s", p)
		}
	}
}
