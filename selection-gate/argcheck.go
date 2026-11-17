package selectiongate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// compiledSchema is a tool's input schema prepared for validation.
type compiledSchema struct {
	resolved *jsonschema.Resolved
}

// compileSchemas prepares every tool's input schema once per Evaluate. A schema that cannot be
// parsed or resolved is left out: the gate then does not check that tool's arguments rather than
// failing every call to it on a schema the gate itself could not read.
func compileSchemas(tools []Tool) map[string]*compiledSchema {
	out := map[string]*compiledSchema{}
	for _, t := range tools {
		if t.Parameters == nil {
			continue
		}
		raw, err := json.Marshal(t.Parameters)
		if err != nil {
			continue
		}
		var s jsonschema.Schema
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		r, err := s.Resolve(nil)
		if err != nil {
			continue
		}
		out[t.Name] = &compiledSchema{resolved: r}
	}
	return out
}

func (c *compiledSchema) validate(args map[string]any) error {
	if args == nil {
		args = map[string]any{}
	}
	// round-trip so numbers and nested values have the JSON types the validator expects
	raw, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("arguments are not JSON: %w", err)
	}
	var inst any
	if err := json.Unmarshal(raw, &inst); err != nil {
		return fmt.Errorf("arguments are not JSON: %w", err)
	}
	if err := c.resolved.Validate(inst); err != nil {
		msg := err.Error()
		if len(msg) > 300 {
			msg = msg[:300] + "..."
		}
		return fmt.Errorf("invalid arguments: %s", strings.TrimSpace(msg))
	}
	return nil
}
