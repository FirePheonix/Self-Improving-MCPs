package mcp

import (
	"testing"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
)

func TestVisibleToolsAppliesPatchesAndAppendsComposedInOrder(t *testing.T) {
	patched := "patched description"
	overlay := improvementsandbox.Overlay{
		ToolPatches: map[string]improvementsandbox.ToolPatch{"read_text_file": {Description: &patched}},
		ComposedTools: map[string]improvementsandbox.ComposedTool{
			"zeta":  {Description: "z"},
			"alpha": {Description: "a"},
			"mid":   {Description: "m"},
		},
	}
	upstream := []ToolInfo{
		{Name: "read_text_file", Description: "original"},
		{Name: "list_directory", Description: "list"},
	}

	for i := 0; i < 20; i++ { // repeat: a map-order bug would show up as flakiness
		got := VisibleTools(upstream, overlay)
		want := []string{"read_text_file", "list_directory", "alpha", "mid", "zeta"}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d", len(got), len(want))
		}
		for j, name := range want {
			if got[j].Name != name {
				t.Fatalf("iteration %d: tool[%d] = %q, want %q", i, j, got[j].Name, name)
			}
		}
		if got[0].Description != patched {
			t.Fatalf("patch not applied: %q", got[0].Description)
		}
	}
	if upstream[0].Description != "original" {
		t.Fatal("VisibleTools mutated its input")
	}
}

func TestVisibleToolsBaselineIsUpstream(t *testing.T) {
	upstream := []ToolInfo{{Name: "a"}, {Name: "b"}}
	got := VisibleTools(upstream, improvementsandbox.Overlay{})
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("baseline should equal upstream, got %+v", got)
	}
}
