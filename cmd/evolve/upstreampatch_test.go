package main

import (
	"encoding/json"
	"strings"
	"testing"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/mcp"
	selectiongate "self-improving-mcp/selection-gate"
	"self-improving-mcp/store"
)

func exp(status string, d selectiongate.Decision) store.Experiment {
	raw, _ := json.Marshal(d)
	return store.Experiment{Status: status, ScorecardJSON: string(raw)}
}

func TestRequireGatePassUsesNewestGateDecisionOnly(t *testing.T) {
	pass := selectiongate.Decision{Verdict: selectiongate.VerdictPass, SealedSHA256: "abc"}
	fail := selectiongate.Decision{Verdict: selectiongate.VerdictFail, SealedSHA256: "abc"}
	replay := store.Experiment{Status: "passed", ScorecardJSON: `{"promote":true,"efficiency_win":true}`}

	if _, err := requireGatePass(nil); err == nil || !strings.Contains(err.Error(), "evolve gate") {
		t.Fatalf("no experiments: %v", err)
	}
	// a replay sandbox pass is not a selection pass
	if _, err := requireGatePass([]store.Experiment{replay}); err == nil {
		t.Fatal("a replay-sandbox pass must not authorise a source change")
	}
	// newest first: a later fail overrides an older pass
	if _, err := requireGatePass([]store.Experiment{exp("failed", fail), exp("passed", pass)}); err == nil || !strings.Contains(err.Error(), `"fail"`) {
		t.Fatalf("newest fail must win: %v", err)
	}
	// a replay result after the gate pass does not hide it
	d, err := requireGatePass([]store.Experiment{replay, exp("passed", pass)})
	if err != nil || d.Verdict != selectiongate.VerdictPass {
		t.Fatalf("%v %+v", err, d)
	}
	if _, err := requireGatePass([]store.Experiment{{ScorecardJSON: "not json"}}); err == nil {
		t.Fatal("garbage scorecard must not pass")
	}
}

func TestSourceTargetsPairPatchesWithLiveDescriptions(t *testing.T) {
	newText := "NEW"
	o := improvementsandbox.Overlay{
		ToolPatches: map[string]improvementsandbox.ToolPatch{
			"b_tool": {Description: &newText},
			"a_tool": {Description: &newText},
			"ghost":  {Description: &newText},
			"schema": {InputSchema: map[string]any{"type": "object"}}, // no description: nothing to carry
		},
		Routing: []improvementsandbox.RouteRule{{FromTool: "a_tool", ToTool: "b_tool"}},
	}
	live := []mcp.ToolInfo{{Name: "a_tool", Description: "old a"}, {Name: "b_tool", Description: "old b"}, {Name: "schema", Description: "s"}}
	targets, missing := sourceTargets(o, live)
	if len(targets) != 2 || targets[0].Tool != "a_tool" || targets[0].Old != "old a" || targets[1].Tool != "b_tool" {
		t.Fatalf("targets (sorted, with live Old text): %+v", targets)
	}
	if len(missing) != 1 || missing[0] != "ghost" {
		t.Fatalf("missing: %v", missing)
	}
}
