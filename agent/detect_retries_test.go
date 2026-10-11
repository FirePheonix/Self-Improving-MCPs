package agent

import (
	"testing"

	"self-improving-mcp/store"
)

func TestDetectRetries(t *testing.T) {
	calls := []store.ToolCall{
		{ID: 1, ToolName: "fetch", IsError: true},
		{ID: 2, ToolName: "fetch", IsError: true},
		{ID: 3, ToolName: "fetch", IsError: true},
	}
	got := DetectRetries(calls)
	if len(got) != 1 || got[0].Category != "retry_storm" {
		t.Fatalf("got %+v", got)
	}
}
