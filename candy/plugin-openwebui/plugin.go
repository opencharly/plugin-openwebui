// Package openwebui is the charly plugin OWNING both halves of the Open WebUI
// configuration surface:
//
//   - command:openwebui — `charly openwebui status|save|apply|diff|export`, the
//     host-side management CLI that talks to a DEPLOYED Open WebUI server over its
//     own HTTP API;
//   - kind:openwebui — the flat external `openwebui:` config entity in a project's
//     `charly.yml`, whose body is validated at load against this plugin's served
//     `#OpenwebuiInput` schema (schema/openwebui.cue).
//
// The two halves are one plugin because they are one object: the command reads and
// writes exactly the entity the kind declares. The plugin owns the ENTIRE logic —
// the CLI grammar, the HTTP client, the per-surface table, the credential
// placeholders and the charly.yml read-modify-write. There is no core openwebui
// logic and no HostBuild seam.
//
// PLACEMENT: EXTERNAL and placement-agnostic. `plugin.source` names this repo's own
// candy, so nothing here is compiled into the charly binary; a project composing
// `@github.com/opencharly/plugin-openwebui/candy/plugin-openwebui:<tag>` gets the
// command word and the kind word at parse time. (Whether a given charly build is
// compiled-in is computed from `charly.yml`'s `compiled_plugins:`, never stated in
// prose.)
package openwebui

import (
	"embed"
	"errors"
	"fmt"
	"os"

	"github.com/opencharly/sdk"
	pb "github.com/opencharly/spec/proto"
)

// schemaFS carries this plugin's OWN self-contained CUE schema, served over Describe.
// There is no schema-less plugin.
//
//go:embed schema/*.cue
var schemaFS embed.FS

// calver is the plugin's identity CalVer, advertised over Describe; tag-on-merge
// advances it on every merge.
const calver = "2026.317.0001"

// NewProvider returns the openwebui provider (in-proc registration or out-of-proc
// serving — the same NewProvider/NewMeta/CliMain triple as every charly plugin).
func NewProvider() pb.ProviderServer { return &provider{} }

// NewMeta advertises the TWO capabilities this candy contributes — `command:openwebui`
// (the CLI) and `kind:openwebui` (the config entity, validated against
// #OpenwebuiInput) — together with the schema served over Describe.
func NewMeta() pb.PluginMetaServer {
	return sdk.NewMeta(calver,
		[]sdk.ProvidedCapability{
			{Class: "command", Word: "openwebui"},
			{Class: "kind", Word: "openwebui", InputDef: "#OpenwebuiInput"},
		},
		schemaFS)
}

// CliMain is the out-of-process CLI entrypoint: parse the grammar, run the leaf, and
// turn an error into exit status 1 with the message on stderr (never a panic, never a
// silent success).
func CliMain(args []string) int {
	err := runCLI(args)
	if err == nil {
		return 0
	}
	// `diff` answers a question: "they differ" is exit 1 with the drift on stderr, not an
	// error report. Match through a wrapper because kong may join the error.
	var drift *driftError
	if errors.As(err, &drift) {
		fmt.Fprintln(os.Stderr, drift.Error())
		return 1
	}
	fmt.Fprintln(os.Stderr, "charly openwebui:", err)
	return 1
}
