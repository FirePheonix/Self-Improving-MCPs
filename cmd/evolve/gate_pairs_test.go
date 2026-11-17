package main

import (
	"testing"

	"self-improving-mcp/mcp"
)

func TestDescriptionPairsOnlyChangedDescriptionsOfExistingTools(t *testing.T) {
	base := []mcp.ToolInfo{{Name: "a", Description: "same"}, {Name: "b", Description: "old b"}}
	cand := []mcp.ToolInfo{{Name: "a", Description: "same"}, {Name: "b", Description: "new b"}, {Name: "composed", Description: "brand new tool"}}
	got := descriptionPairs(base, cand)
	if len(got) != 1 || got[0].Tool != "b" || got[0].Old != "old b" || got[0].New != "new b" {
		t.Fatalf("%+v", got)
	}
}
