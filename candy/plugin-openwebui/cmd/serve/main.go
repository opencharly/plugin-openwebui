// cmd/serve is the plugin binary the host builds and either syscall.Exec's (the
// external command lane) or speaks go-plugin gRPC to (the connected lane). It is the ONE
// build target; sdk.Main wires the provider, the meta and the CLI entrypoint.
package main

import (
	openwebui "github.com/opencharly/plugin-openwebui/candy/plugin-openwebui"
	"github.com/opencharly/sdk"
)

func main() { sdk.Main(openwebui.NewProvider(), openwebui.NewMeta(), openwebui.CliMain) }
