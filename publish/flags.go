// Package publish turns a gate decision into something a human acts on: a side branch with
// the evidence for a winning overlay, or a notification explaining why a candidate was not shipped.
//
// Nothing here pushes, opens a pull request, or touches the caller's working tree.
package publish

import (
	"fmt"
	"sort"
	"strings"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
)

// maxDescriptionLen is the length above which a description is flagged for review.
const maxDescriptionLen = 400

// suspiciousPhrases are wordings that address the model as an authority, not the tool as a thing.
var suspiciousPhrases = []string{
	"ignore previous", "ignore all", "ignore the above", "disregard", "system prompt",
	"you must", "do not tell", "don't tell", "secret", "override", "instead of the user",
}

// ReviewFlags lists reasons a human should read the overlay's model-visible text closely.
// Descriptions are shown to agents on every request, so rewritten wording is an injection
// surface. The scan is deliberately crude: it raises a question, a person answers it.
// Output order is deterministic.
func ReviewFlags(o improvementsandbox.Overlay) []string {
	var flags []string

	patchNames := make([]string, 0, len(o.ToolPatches))
	for name := range o.ToolPatches {
		patchNames = append(patchNames, name)
	}
	sort.Strings(patchNames)
	for _, name := range patchNames {
		if d := o.ToolPatches[name].Description; d != nil {
			flags = append(flags, scanText(fmt.Sprintf("tool_patches.%s", name), *d)...)
		}
	}

	composedNames := make([]string, 0, len(o.ComposedTools))
	for name := range o.ComposedTools {
		composedNames = append(composedNames, name)
	}
	sort.Strings(composedNames)
	for _, name := range composedNames {
		flags = append(flags, scanText(fmt.Sprintf("composed_tools.%s", name), o.ComposedTools[name].Description)...)
	}
	return flags
}

func scanText(where, text string) []string {
	var out []string
	lower := strings.ToLower(text)
	if len(text) > maxDescriptionLen {
		out = append(out, fmt.Sprintf("%s: unusually long description (%d chars)", where, len(text)))
	}
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "www.") {
		out = append(out, fmt.Sprintf("%s: contains a URL", where))
	}
	for _, p := range suspiciousPhrases {
		if strings.Contains(lower, p) {
			out = append(out, fmt.Sprintf("%s: contains %q", where, p))
		}
	}
	return out
}
