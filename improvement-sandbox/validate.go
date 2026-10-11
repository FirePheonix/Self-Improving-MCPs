package improvementsandbox

import "fmt"

// Validate checks an overlay for basic structural issues.
func Validate(o Overlay) error {
	for name, patch := range o.ToolPatches {
		if name == "" {
			return fmt.Errorf("tool_patches has empty tool name")
		}
		if patch.Description != nil && *patch.Description == "" {
			return fmt.Errorf("tool_patches[%s] empty description", name)
		}
	}
	for i, r := range o.Routing {
		if r.FromTool == "" {
			return fmt.Errorf("routing[%d] missing from_tool", i)
		}
		if !r.Deny && r.ToTool == "" {
			return fmt.Errorf("routing[%d] needs to_tool or deny", i)
		}
	}
	for i, c := range o.Cache {
		if c.ToolName == "" {
			return fmt.Errorf("cache[%d] missing tool_name", i)
		}
		if c.TTLSeconds <= 0 {
			return fmt.Errorf("cache[%d] ttl_seconds must be > 0", i)
		}
	}
	for name, ct := range o.ComposedTools {
		if name == "" {
			return fmt.Errorf("composed_tools has empty name")
		}
		if len(ct.Steps) == 0 {
			return fmt.Errorf("composed_tools[%s] has no steps", name)
		}
		for j, step := range ct.Steps {
			if step.Tool == "" {
				return fmt.Errorf("composed_tools[%s].steps[%d] missing tool", name, j)
			}
		}
	}
	if len(o.ToolPatches) == 0 && len(o.Routing) == 0 && len(o.Cache) == 0 && len(o.ComposedTools) == 0 {
		return fmt.Errorf("overlay is empty")
	}
	return nil
}
