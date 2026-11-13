package openwebui

import (
	"os"
	"testing"
)

// live_test.go — the LIVE half of the schema contract (R8: a boundary that crosses a real
// service runs against that service, and skips visibly when it is absent).
//
// A fake cannot prove this: the fake answers what the test author believed the server
// answers. This test asks a real server and validates every GET body against the schema
// `save` will write it into, which is how the 0.13.3 differences this repo carries were
// found (banners' bare-list GET, and `chats`/`subagents` bodies that gained fields or null
// values).
//
// Run it against a disposable instance:
//
//	OPENWEBUI_LIVE_URL=http://127.0.0.1:8099 OPENWEBUI_EMAIL=… OPENWEBUI_PASSWORD=… \
//	  GOWORK=off go test -run TestLiveSurfaceBodiesMatchTheSchema -v ./...
func TestLiveSurfaceBodiesMatchTheSchema(t *testing.T) {
	url := os.Getenv("OPENWEBUI_LIVE_URL")
	if url == "" {
		t.Skip("OPENWEBUI_LIVE_URL not set — no live Open WebUI to validate against")
	}
	cl, err := NewClient(url, true, 0)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	email, password := os.Getenv("OPENWEBUI_EMAIL"), os.Getenv("OPENWEBUI_PASSWORD")
	if email == "" || password == "" {
		t.Skip("OPENWEBUI_EMAIL/OPENWEBUI_PASSWORD not set — cannot read admin surfaces")
	}
	if err := cl.SignIn(email, password); err != nil {
		t.Fatalf("sign in: %v", err)
	}
	for _, s := range Surfaces {
		if s.GetPath == "" {
			continue
		}
		raw, err := cl.GetJSON(s.GetPath)
		if err != nil {
			t.Errorf("%s: GET %s: %v", s.Name, s.GetPath, err)
			continue
		}
		body := redact(s.normalize(raw))
		if err := ValidateEntity(map[string]any{"config": map[string]any{s.Name: body}}); err != nil {
			t.Errorf("%s: the server's own body does not satisfy the schema `save` would apply it to: %v", s.Name, err)
		}
	}
}
