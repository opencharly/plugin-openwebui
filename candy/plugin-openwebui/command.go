package openwebui

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/alecthomas/kong"
)

// command.go — the `charly openwebui` grammar and the five verbs behind it.
//
// The command speaks plain HTTP to ONE server root and edits ONE charly.yml entity. It
// owns the whole logic; nothing here needs the host reverse channel, which is what makes
// it work identically in either placement.

// CLI is the `charly openwebui` command tree.
type CLI struct {
	Server   string        `help:"Open WebUI server root, e.g. https://host:40045 (default: the entity's endpoint:, else $OPENWEBUI_URL)."`
	APIKey   string        `env:"OPENWEBUI_API_KEY" help:"Bearer API key for the server."`
	Email    string        `env:"OPENWEBUI_EMAIL" help:"Admin email used to sign in when no API key is given."`
	Password string        `env:"OPENWEBUI_PASSWORD" help:"Admin password used to sign in when no API key is given."`
	Insecure bool          `help:"Skip TLS certificate verification (self-signed deployment)."`
	Timeout  time.Duration `help:"Per-request HTTP timeout." default:"30s"`
	Node     string        `help:"charly.yml entity name." default:"openwebui-config"`
	File     string        `help:"charly.yml path." default:"charly.yml"`

	Status StatusCmd `cmd:"" help:"Report the server version and reachability (read-only)."`
	Save   SaveCmd   `cmd:"" help:"Write the server's live configuration into the charly.yml entity."`
	Apply  ApplyCmd  `cmd:"" help:"Apply the charly.yml entity to the live server."`
	Diff   DiffCmd   `cmd:"" help:"Report drift between the entity and the live server (exit 1 on drift)."`
	Export ExportCmd `cmd:"" help:"Dump the server's raw config table. CONTAINS CREDENTIALS — backup use only."`
}

// StatusCmd reports the server's own version document.
type StatusCmd struct{}

// SaveCmd captures the live configuration into the entity.
type SaveCmd struct {
	DryRun bool `help:"Render the entity and validate it without writing charly.yml."`
}

// ApplyCmd pushes the entity to the live server.
type ApplyCmd struct {
	DryRun              bool `help:"Print the plan without posting anything."`
	AllowLiteralSecrets bool `help:"Permit literal credential values in the entity (throwaway beds only)."`
}

// DiffCmd reports drift; it exits 1 when the live server differs from the entity.
type DiffCmd struct {
	AllowLiteralSecrets bool `help:"Permit literal credential values in the entity (throwaway beds only)."`
}

// ExportCmd dumps the raw config table.
type ExportCmd struct {
	Output string `help:"Write the JSON here instead of stdout." short:"o"`
	Yes    bool   `help:"Acknowledge that the dump contains credentials." `
}

// driftError signals "the server differs from the entity" without being a failure of the
// command itself: `diff` is a question, and "yes, they differ" is its answer.
type driftError struct{ surfaces int }

func (e *driftError) Error() string {
	return fmt.Sprintf("%d surface(s) differ from the live server", e.surfaces)
}

// runCLI parses the grammar and runs the selected leaf.
func runCLI(args []string) error {
	cli := &CLI{}
	parser, err := kong.New(cli,
		kong.Name("openwebui"),
		kong.Description("Manage a deployed Open WebUI server's configuration from a charly.yml entity."),
		kong.UsageOnError(),
	)
	if err != nil {
		return err
	}
	ctx, err := parser.Parse(args)
	if err != nil {
		return err
	}
	return ctx.Run(cli)
}

// ---------------------------------------------------------------------------
// shared plumbing
// ---------------------------------------------------------------------------

// loadEntity reads the node, or returns nil when the file/node does not exist yet (save
// bootstraps it).
func (c *CLI) loadEntity() (*Entity, error) {
	ent, err := LoadEntity(c.File, c.Node)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		// A file that exists but has no such node is the bootstrap case for save, and an
		// error for every other verb; distinguish by whether the error is the "no node"
		// class.
		if strings.Contains(err.Error(), "has no node") {
			return nil, nil
		}
		return nil, err
	}
	return ent, nil
}

// serverURL resolves the server root: flag > entity endpoint > $OPENWEBUI_URL.
func (c *CLI) serverURL(ent *Entity) string {
	if strings.TrimSpace(c.Server) != "" {
		return c.Server
	}
	if ent != nil {
		if ep := strings.TrimSpace(ent.Endpoint()); ep != "" {
			return ep
		}
	}
	return os.Getenv("OPENWEBUI_URL")
}

// client builds an authenticated client. Authentication precedence:
//
//	--api-key/$OPENWEBUI_API_KEY  >  --email+--password signin  >  the entity's
//	auth.email + auth.password_secret (resolved from the environment).
func (c *CLI) client(ent *Entity) (*Client, error) { return c.clientOpt(ent, true) }

// clientOpt builds the client; with requireAuth=false a credential-less invocation is
// allowed (status reads a public route), so reachability can be checked before an
// operator has a password in hand.
func (c *CLI) clientOpt(ent *Entity, requireAuth bool) (*Client, error) {
	root := c.serverURL(ent)
	cl, err := NewClient(root, c.Insecure, c.Timeout)
	if err != nil {
		return nil, err
	}
	if key := strings.TrimSpace(c.APIKey); key != "" {
		cl.SetToken(key)
		return cl, nil
	}
	email, password := strings.TrimSpace(c.Email), c.Password
	if ent != nil {
		if auth := ent.Auth(); auth != nil {
			if email == "" {
				email, _ = auth["email"].(string)
			}
			if password == "" {
				if ref, ok := auth["password_secret"].(string); ok && ref != "" {
					pw, err := lookupSecret(ref)
					if err != nil {
						return nil, err
					}
					password = pw
				}
			}
		}
	}
	if email == "" || password == "" {
		if !requireAuth {
			return cl, nil
		}
		return nil, fmt.Errorf("no credentials: pass --api-key/$OPENWEBUI_API_KEY, or --email with --password (the entity's auth.password_secret also works)")
	}
	if err := cl.SignIn(email, password); err != nil {
		return nil, err
	}
	return cl, nil
}

// lookupSecret resolves a credential KEY from the environment. The exact key wins; a
// namespaced key (charly/secret/WEBUI_ADMIN_PASSWORD) falls back to its basename, which
// is the env name a charly deployment already uses.
func lookupSecret(key string) (string, error) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v, nil
	}
	if i := strings.LastIndex(key, "/"); i >= 0 && i+1 < len(key) {
		base := key[i+1:]
		if v, ok := os.LookupEnv(base); ok && v != "" {
			return v, nil
		}
	}
	return "", &SecretNotFoundError{Key: key}
}

// ---------------------------------------------------------------------------
// status
// ---------------------------------------------------------------------------

func (StatusCmd) Run(c *CLI) error {
	ent, err := c.loadEntity()
	if err != nil {
		return err
	}
	cl, err := c.clientOpt(ent, false)
	if err != nil {
		return err
	}
	version, err := cl.Version()
	if err != nil {
		return err
	}
	fmt.Printf("server:  %s\nversion: %s\n", cl.Base(), version)
	return nil
}

// ---------------------------------------------------------------------------
// save
// ---------------------------------------------------------------------------

func (s SaveCmd) Run(c *CLI) error {
	ent, err := c.loadEntity()
	if err != nil {
		return err
	}
	cl, err := c.client(ent)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if ent != nil {
		for k, v := range ent.Body {
			body[k] = v
		}
	}
	if ep := strings.TrimSpace(c.Server); ep != "" {
		body["endpoint"] = ep
	} else if _, ok := body["endpoint"]; !ok {
		if cur := cl.Base(); cur != "" {
			body["endpoint"] = cur
		}
	}
	// Preserve an authored auth block (a password_secret reference) and fill the email.
	auth := map[string]any{}
	if existing, ok := body["auth"].(map[string]any); ok {
		for k, v := range existing {
			auth[k] = v
		}
	}
	if e := strings.TrimSpace(c.Email); e != "" {
		auth["email"] = e
	}
	if len(auth) > 0 {
		body["auth"] = auth
	}

	config := map[string]any{}
	var skipped []string
	for _, s := range Surfaces {
		if s.GetPath == "" {
			skipped = append(skipped, s.Name+" (write-only surface, no GET)")
			continue
		}
		live, err := cl.GetJSON(s.GetPath)
		if err != nil {
			if code := Status(err); code == 404 || code == 405 {
				skipped = append(skipped, fmt.Sprintf("%s (the server does not serve %s)", s.Name, s.GetPath))
				continue
			}
			return fmt.Errorf("read surface %s: %w", s.Name, err)
		}
		config[s.Name] = redact(live)
	}
	if len(config) > 0 {
		body["config"] = config
	}

	rendered, err := WriteEntity(c.File, c.Node, body, s.DryRun)
	if err != nil {
		return err
	}
	if s.DryRun {
		fmt.Print(rendered)
		fmt.Fprintf(os.Stderr, "dry-run: %s not written\n", c.File)
		return nil
	}
	fmt.Printf("saved %d surface(s) into %s node %q\n", len(config), c.File, c.Node)
	for _, sk := range skipped {
		fmt.Printf("  skipped %s\n", sk)
	}
	fmt.Fprintln(os.Stderr, "note: credential-bearing fields are written as {keep: true}; review the node before committing it")
	return nil
}

// ---------------------------------------------------------------------------
// apply
// ---------------------------------------------------------------------------

func (a ApplyCmd) Run(c *CLI) error {
	ent, err := c.loadEntity()
	if err != nil {
		return err
	}
	if ent == nil {
		return fmt.Errorf("%s has no node %q — run `charly openwebui save --node %s` first", c.File, c.Node, c.Node)
	}
	if err := ValidateEntity(ent.Body); err != nil {
		return err
	}
	cl, err := c.client(ent)
	if err != nil {
		return err
	}
	config := ent.Config()
	if config == nil {
		return fmt.Errorf("node %q has no `config:` block", c.Node)
	}
	names := sortedKeys(config)
	for _, name := range names {
		s, ok := LookupSurface(name)
		if !ok {
			return fmt.Errorf("unknown config surface %q (known: %s)", name, strings.Join(SurfaceNames(), ", "))
		}
		desired := config[name]
		var live any
		if s.GetPath != "" {
			live, err = cl.GetJSON(s.GetPath)
			if err != nil {
				return fmt.Errorf("read surface %s: %w", name, err)
			}
		}
		resolved, err := resolveSecrets(desired, live, lookupSecret, a.AllowLiteralSecrets)
		if err != nil {
			return fmt.Errorf("surface %s: %w", name, err)
		}
		changed := s.GetPath == "" || !equalJSON(resolved, live)
		if !changed {
			fmt.Printf("%-18s unchanged\n", name)
			continue
		}
		if a.DryRun {
			fmt.Printf("%-18s would apply (%s)\n", name, joinOrNone(DiffPaths(resolved, live, "")))
			continue
		}
		if _, err := cl.Post(s.PostPath, resolved); err != nil {
			return fmt.Errorf("apply surface %s (%s): %w", name, s.PostPath, err)
		}
		fmt.Printf("%-18s applied\n", name)
	}
	if a.DryRun {
		fmt.Fprintln(os.Stderr, "dry-run: nothing was posted")
	}
	return nil
}

// ---------------------------------------------------------------------------
// diff
// ---------------------------------------------------------------------------

func (d DiffCmd) Run(c *CLI) error {
	ent, err := c.loadEntity()
	if err != nil {
		return err
	}
	if ent == nil {
		return fmt.Errorf("%s has no node %q", c.File, c.Node)
	}
	if err := ValidateEntity(ent.Body); err != nil {
		return err
	}
	cl, err := c.client(ent)
	if err != nil {
		return err
	}
	config := ent.Config()
	if config == nil {
		return fmt.Errorf("node %q has no `config:` block", c.Node)
	}
	drifted := 0
	for _, name := range sortedKeys(config) {
		s, ok := LookupSurface(name)
		if !ok {
			return fmt.Errorf("unknown config surface %q (known: %s)", name, strings.Join(SurfaceNames(), ", "))
		}
		var live any
		if s.GetPath != "" {
			live, err = cl.GetJSON(s.GetPath)
			if err != nil {
				return fmt.Errorf("read surface %s: %w", name, err)
			}
		}
		resolved, err := resolveSecrets(config[name], live, lookupSecret, d.AllowLiteralSecrets)
		if err != nil {
			return fmt.Errorf("surface %s: %w", name, err)
		}
		paths := DiffPaths(resolved, live, "")
		if len(paths) == 0 {
			fmt.Printf("%-18s in sync\n", name)
			continue
		}
		drifted++
		fmt.Printf("%-18s differs\n", name)
		for _, p := range paths {
			fmt.Printf("  %s\n", p)
		}
	}
	if drifted > 0 {
		return &driftError{surfaces: drifted}
	}
	return nil
}

// ---------------------------------------------------------------------------
// export
// ---------------------------------------------------------------------------

func (e ExportCmd) Run(c *CLI) error {
	if !e.Yes {
		return fmt.Errorf("refusing to dump without --yes: GET /api/v1/configs/export returns provider API keys and tool-server credentials in cleartext")
	}
	ent, err := c.loadEntity()
	if err != nil {
		return err
	}
	cl, err := c.client(ent)
	if err != nil {
		return err
	}
	raw, err := cl.Get("/api/v1/configs/export")
	if err != nil {
		return err
	}
	var pretty any
	if json.Unmarshal(raw, &pretty) != nil {
		pretty = string(raw)
	}
	out, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if e.Output == "" {
		fmt.Fprintf(os.Stderr, "warning: this dump contains credentials in cleartext\n")
		_, err = os.Stdout.Write(out)
		return err
	}
	if err := os.WriteFile(e.Output, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", e.Output, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (0600; contains credentials)\n", e.Output)
	return nil
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func joinOrNone(paths []string) string {
	if len(paths) == 0 {
		return "(body differs)"
	}
	return strings.Join(paths, ", ")
}
