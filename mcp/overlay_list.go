package mcp

import improvementsandbox "self-improving-mcp/improvement-sandbox"

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
