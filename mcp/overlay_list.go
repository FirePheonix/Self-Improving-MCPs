package mcp

import (
	"sort"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
)

func applyToolPatch(t ToolInfo, overlay improvementsandbox.Overlay) ToolInfo {
	patch, ok := overlay.ToolPatches[t.Name]
	if !ok {
		return t
	}
	if patch.Description != nil {
		t.Description = *patch.Description
	}
	if patch.InputSchema != nil {
		t.InputSchema = patch.InputSchema
	}
	return t
}

// ApplyOverlayToTools returns a copy of tools with description/schema patches applied.
func ApplyOverlayToTools(tools []ToolInfo, overlay improvementsandbox.Overlay) []ToolInfo {
	out := make([]ToolInfo, len(tools))
	for i, t := range tools {
		out[i] = applyToolPatch(t, overlay)
	}
	return out
}

// VisibleTools returns the tool list an agent sees through the proxy for this overlay:
// upstream tools with description/schema patches, then composed tools in name order.
// It mirrors RegisterUpstreamTools so offline gates evaluate the same surface production serves.
func VisibleTools(upstream []ToolInfo, overlay improvementsandbox.Overlay) []ToolInfo {
	patched := ApplyOverlayToTools(upstream, overlay)
	out := make([]ToolInfo, 0, len(patched))
	for _, t := range patched {
		if overlay.ToolPatches[t.Name].Hidden {
			continue // hidden tools are not offered to agents
		}
		out = append(out, t)
	}

	names := make([]string, 0, len(overlay.ComposedTools))
	for name := range overlay.ComposedTools {
		names = append(names, name)
	}
	sort.Strings(names) // map order is random; the selector must see a stable list

	for _, name := range names {
		out = append(out, ToolInfo{
			Name:        name,
			Description: overlay.ComposedTools[name].Description,
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		})
	}
	return out
}
