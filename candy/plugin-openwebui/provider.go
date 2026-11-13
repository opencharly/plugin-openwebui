package openwebui

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opencharly/sdk"
	pb "github.com/opencharly/spec/proto"
)

// provider.go — the Provider.Invoke surface for both placements.
//
//   - sdk.OpRun  — `charly openwebui …` with the pass-through CLI args. The command is
//     SELF-CONTAINED (plain HTTP to the server plus a local charly.yml edit), so it
//     uses no reverse channel and Invoke and CliMain both run runCLI.
//   - sdk.OpLoad — the `kind: openwebui` ENTITY load. The host has already validated
//     the body against this plugin's served #OpenwebuiInput; the plugin returns that
//     body re-marshalled as canonical JSON, which the host folds into
//     uf.PluginKinds["openwebui"][name]. The body is opaque to every other provider —
//     only this plugin reads it back, from charly.yml itself.
type provider struct{ pb.UnimplementedProviderServer }

func (provider) Invoke(_ context.Context, req *pb.InvokeRequest) (*pb.InvokeReply, error) {
	switch req.GetOp() {
	case sdk.OpLoad:
		if req.GetReserved() != "openwebui" {
			return nil, fmt.Errorf("plugin-openwebui: unsupported kind word %q (want %q)", req.GetReserved(), "openwebui")
		}
		if len(req.GetParamsJson()) == 0 {
			return nil, fmt.Errorf("plugin-openwebui: kind load requires a CUE input payload")
		}
		var body any
		if err := json.Unmarshal(req.GetParamsJson(), &body); err != nil {
			return nil, fmt.Errorf("plugin-openwebui: decode entity: %w", err)
		}
		out, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("plugin-openwebui: marshal entity: %w", err)
		}
		return &pb.InvokeReply{ResultJson: out}, nil

	case sdk.OpRun:
		var in struct {
			Args []string `json:"args"`
		}
		if len(req.GetParamsJson()) > 0 {
			if err := json.Unmarshal(req.GetParamsJson(), &in); err != nil {
				return nil, fmt.Errorf("plugin-openwebui: decode args: %w", err)
			}
		}
		if err := runCLI(in.Args); err != nil {
			return nil, err
		}
		return &pb.InvokeReply{}, nil
	}
	return nil, fmt.Errorf("plugin-openwebui: unsupported op %q", req.GetOp())
}
