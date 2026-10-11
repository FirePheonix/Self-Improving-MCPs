package agent

import (
	"testing"

	"self-improving-mcp/store"
)

func TestDetectRedundant(t *testing.T) {
	calls := []store.ToolCall{
		{ID: 1, ToolName: "echo", ArgsHash: "h1"},
		{ID: 2, ToolName: "echo", ArgsHash: "h1"},
		{ID: 3, ToolName: "other", ArgsHash: "h2"},
	}
	got := DetectRedundant(calls)
	if len(got) != 1 || got[0].Category != "redundant" {
		t.Fatalf("got %+v", got)
	}
}
