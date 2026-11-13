package openwebui

import (
	"encoding/json"
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// cue.go — the in-process CUE gate.
//
// The plugin embeds the schema it SERVES (schema/openwebui.cue) and compiles the very
// same bytes here, so the CLI cannot write an entity the host's load gate would reject.
// One schema, two consumers (the Describe channel and this validator) — never a copy
// that could drift.

// compileInputDef returns the #OpenwebuiInput definition from the embedded schema.
func compileInputDef() (cue.Value, *cue.Context, error) {
	src, err := schemaFS.ReadFile("schema/openwebui.cue")
	if err != nil {
		return cue.Value{}, nil, fmt.Errorf("read embedded schema: %w", err)
	}
	ctx := cuecontext.New()
	v := ctx.CompileBytes(src)
	if err := v.Err(); err != nil {
		return cue.Value{}, nil, fmt.Errorf("compile served schema: %w", err)
	}
	def := v.LookupPath(cue.ParsePath("#OpenwebuiInput"))
	if err := def.Err(); err != nil {
		return cue.Value{}, nil, fmt.Errorf("served schema has no #OpenwebuiInput: %w", err)
	}
	return def, ctx, nil
}

// validateEntityCUE unifies an entity body against #OpenwebuiInput and requires it to be
// concrete. A typo, a wrong type, or a missing required field is an error naming the
// field; nothing reaches the network or the manifest until it passes.
func validateEntityCUE(body map[string]any) error {
	def, ctx, err := compileInputDef()
	if err != nil {
		return err
	}
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal entity: %w", err)
	}
	val := ctx.CompileBytes(data)
	if err := val.Err(); err != nil {
		return fmt.Errorf("entity is not JSON-representable: %w", err)
	}
	if err := def.Unify(val).Validate(cue.Concrete(true)); err != nil {
		return fmt.Errorf("entity fails #OpenwebuiInput: %w", err)
	}
	return nil
}
