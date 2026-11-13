package openwebui

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// client.go — the ONE HTTP client the CLI uses. Every surface call goes through it, so
// authentication, TLS and error shaping live in exactly one place (R2/R3).
//
// The server speaks bearer auth: a JWT from POST /api/v1/auths/signin, or a durable API
// key from POST /api/v1/auths/api_key. An API key answers 403 for EVERY request while
// `auth.enable_api_keys` is false (upstream's default), so the CLI treats 403 on a key
// as a named condition rather than retrying.

// Client is a minimal JSON client for one Open WebUI server root.
type Client struct {
	base  string
	hc    *http.Client
	token string
}

// maxBody bounds a response read so a misbehaving server cannot exhaust memory.
const maxBody = 32 << 20

// NewClient builds a client for `server` (a full http(s) root, no trailing slash).
// `insecure` skips certificate verification for a self-signed deployment; the default is
// to verify, because a silent TLS downgrade is the kind of convenience that hides a
// real misconfiguration.
func NewClient(server string, insecure bool, timeout time.Duration) (*Client, error) {
	s := strings.TrimSuffix(strings.TrimSpace(server), "/")
	if s == "" {
		return nil, fmt.Errorf("no server: pass --server, set OPENWEBUI_URL, or add `endpoint:` to the entity")
	}
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		s = "http://" + s
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit --insecure
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{base: s, hc: &http.Client{Timeout: timeout, Transport: tr}}, nil
}

// Base is the resolved server root (used in messages).
func (c *Client) Base() string { return c.base }

// SetToken installs the bearer token used by every subsequent call.
func (c *Client) SetToken(tok string) { c.token = strings.TrimSpace(tok) }

// Token reports whether a bearer token is installed.
func (c *Client) Token() bool { return c.token != "" }

// HTTPError is a non-2xx answer, carrying the method, path, status and a bounded body
// snippet so a caller never has to re-run the request to learn why it failed.
type HTTPError struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	msg := fmt.Sprintf("%s %s: HTTP %d", e.Method, e.Path, e.Status)
	if e.Status == http.StatusUnauthorized {
		msg += " (not authenticated: pass --api-key/OPENWEBUI_API_KEY, or --email/--password with an admin account)"
	}
	if e.Status == http.StatusForbidden {
		msg += " (authenticated but not permitted: an API key needs auth.enable_api_keys=true on the server; an MFA-bearing import/settings change needs a human JWT session)"
	}
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// Status reports the HTTP status of a failed call (0 when err is not an HTTPError).
func Status(err error) int {
	if he, ok := err.(*HTTPError); ok {
		return he.Status
	}
	return 0
}

func (c *Client) do(method, path string, body any) (json.RawMessage, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode %s body: %w", path, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%s %s: read body: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 400 {
			snippet = snippet[:400] + "…"
		}
		return nil, &HTTPError{Method: method, Path: path, Status: resp.StatusCode, Body: snippet}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("null"), nil
	}
	if !json.Valid(raw) {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > 120 {
			snippet = snippet[:120] + "…"
		}
		return nil, fmt.Errorf("%s %s: response is not JSON: %s", method, path, snippet)
	}
	return json.RawMessage(raw), nil
}

// Get performs one authenticated GET.
func (c *Client) Get(path string) (json.RawMessage, error) { return c.do(http.MethodGet, path, nil) }

// Post performs one authenticated POST with a JSON body.
func (c *Client) Post(path string, body any) (json.RawMessage, error) {
	return c.do(http.MethodPost, path, body)
}

// Version reads the server's own version document.
func (c *Client) Version() (string, error) {
	raw, err := c.Get("/api/version")
	if err != nil {
		return "", err
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("decode /api/version: %w", err)
	}
	return v.Version, nil
}

// SignIn exchanges admin credentials for a JWT and installs it on the client.
func (c *Client) SignIn(email, password string) error {
	raw, err := c.Post("/api/v1/auths/signin", map[string]string{"email": email, "password": password})
	if err != nil {
		return fmt.Errorf("sign in as %s: %w", email, err)
	}
	var res struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("decode signin response: %w", err)
	}
	if res.Token == "" {
		return fmt.Errorf("sign in as %s: the server answered without a token (MFA challenge? sign in with --api-key instead)", email)
	}
	c.SetToken(res.Token)
	return nil
}

// Health probes a benign unauthenticated route ("/" would drag the whole SPA); the
// caller uses it to tell "server down" from "credentials wrong".
func (c *Client) Health() error {
	_, err := c.Get("/health")
	return err
}

// GetJSON decodes a GET into a generic JSON value (map[string]any / []any / scalar).
func (c *Client) GetJSON(path string) (any, error) {
	raw, err := c.Get(path)
	if err != nil {
		return nil, err
	}
	return decodeAny(raw, path)
}

func decodeAny(raw json.RawMessage, path string) (any, error) {
	// Deliberately NOT json.Number: a json.Number is a string type, and the yaml
	// marshaller would write it quoted ("2"), which the entity schema — correctly —
	// rejects on the next load. A plain float64 marshals back to an unquoted number in
	// both JSON and YAML, so a save/load/apply round-trip preserves the type.
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return v, nil
}
