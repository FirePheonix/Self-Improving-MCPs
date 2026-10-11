package agent

import (
	"testing"

	"self-improving-mcp/store"
)

func TestDetectFrequentSequences(t *testing.T) {
	calls := []store.ToolCall{
		{ID: 1, ToolName: "a"}, {ID: 2, ToolName: "b"},
		{ID: 3, ToolName: "a"}, {ID: 4, ToolName: "b"},
		{ID: 5, ToolName: "a"}, {ID: 6, ToolName: "b"},
	}
	got := DetectFrequentSequences(calls)
	if len(got) != 1 || got[0].Category != "frequent_sequence" {
		t.Fatalf("got %+v", got)
	}
}
