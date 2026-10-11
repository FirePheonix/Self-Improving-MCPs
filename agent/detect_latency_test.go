package agent

import (
	"testing"

	"self-improving-mcp/store"
)

func TestDetectLatencyOutliers(t *testing.T) {
	calls := []store.ToolCall{
		{ID: 1, ToolName: "slow", LatencyMs: 10},
		{ID: 2, ToolName: "slow", LatencyMs: 12},
		{ID: 3, ToolName: "slow", LatencyMs: 11},
		{ID: 4, ToolName: "slow", LatencyMs: 13},
		{ID: 5, ToolName: "slow", LatencyMs: 1000},
	}
	got := DetectLatencyOutliers(calls)
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
}
