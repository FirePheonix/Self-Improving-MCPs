package agent

import (
	"testing"

	"self-improving-mcp/store"
)

func TestDetectRepeatedFailures(t *testing.T) {
	calls := []store.ToolCall{
		{ID: 1, ToolName: "read", ArgsHash: "x", IsError: true},
		{ID: 2, ToolName: "read", ArgsHash: "x", IsError: true},
		{ID: 3, ToolName: "read", ArgsHash: "x", IsError: true},
	}
	got := DetectRepeatedFailures(calls)
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
}
