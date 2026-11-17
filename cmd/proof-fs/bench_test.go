package main

import (
	"testing"

	"self-improving-mcp/mcp"
	selectiongate "self-improving-mcp/selection-gate"
)

// Regression: with equivalent tools counted as correct, routing a tool to its twin on a sibling
// server breaks nothing, so the harmful control came back inconclusive (Browser: playwright to
// puppeteer, 56 -> 56). The control must pick a target that is NOT acceptable for the tasks it moves.
func TestHarmfulRouteAvoidsEquivalentTwins(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"url": map[string]any{"type": "string"}}, "required": []any{"url"}}
	tools := []mcp.ToolInfo{
		{Name: "a_navigate", InputSchema: schema},
		{Name: "b_navigate", InputSchema: schema}, // twin of a_navigate: first alphabetically, so the old code chose it
		{Name: "z_fetch", InputSchema: schema},
	}
	var sealed []selectiongate.Task
	var base selectiongate.Run
	for i := 0; i < 4; i++ {
		sealed = append(sealed, selectiongate.Task{ID: string(rune('0' + i)), Query: "open page", ExpectedTool: "a_navigate", AcceptableTools: []string{"b_navigate"}})
		base.Outcomes = append(base.Outcomes, selectiongate.Outcome{Picked: "a_navigate", Served: "a_navigate", Correct: true})
	}
	from, to, expect := harmfulRoute(base, sealed, tools)
	if from != "a_navigate" || to != "z_fetch" || expect != 4 {
		t.Fatalf("got %s -> %s (expect %d), want a_navigate -> z_fetch breaking 4", from, to, expect)
	}
}

func TestHarmfulRouteNeedsSomethingToBreak(t *testing.T) {
	schema := map[string]any{"type": "object"}
	tools := []mcp.ToolInfo{{Name: "a", InputSchema: schema}, {Name: "b", InputSchema: schema}}
	sealed := []selectiongate.Task{{ID: "1", Query: "q", ExpectedTool: "a", AcceptableTools: []string{"b"}}}
	base := selectiongate.Run{Outcomes: []selectiongate.Outcome{{Picked: "a", Served: "a", Correct: true}}}
	if from, _, _ := harmfulRoute(base, sealed, tools); from != "" {
		t.Fatalf("every route is harmless here, want no control, got from %q", from)
	}
}

func TestRestoreOverlayPutsBackEveryRealDescription(t *testing.T) {
	ov := restoreOverlay(map[string]string{"a": "Read a file.", "b": "  "})
	if p, ok := ov.ToolPatches["a"]; !ok || p.Description == nil || *p.Description != "Read a file." {
		t.Fatalf("a not restored: %+v", ov.ToolPatches)
	}
	if _, ok := ov.ToolPatches["b"]; ok {
		t.Fatal("a tool with no real description must not get an empty patch")
	}
}
