package improvementsandbox

import (
	"context"
	"encoding/json"
	"time"

	"self-improving-mcp/store"
)

// CallFunc invokes a tool by name (baseline or candidate).
type CallFunc func(ctx context.Context, tool string, args map[string]any) (ok bool, latencyMs int64, err error)

// ReplayResult is aggregate stats from replaying a trajectory.
type ReplayResult struct {
	Calls     int
	Errors    int
	TotalMs   int64
	Latencies []int64
}

// ReplayTrajectory replays stored calls through callFn.
func ReplayTrajectory(ctx context.Context, calls []store.ToolCall, callFn CallFunc) (ReplayResult, error) {
	var res ReplayResult
	for _, c := range calls {
		args := map[string]any{}
		if c.ArgsJSON != "" {
			_ = json.Unmarshal([]byte(c.ArgsJSON), &args)
		}
		start := time.Now()
		ok, latency, err := callFn(ctx, c.ToolName, args)
		if latency <= 0 {
			latency = time.Since(start).Milliseconds()
		}
		res.Calls++
		res.TotalMs += latency
		res.Latencies = append(res.Latencies, latency)
		if err != nil || !ok {
			res.Errors++
		}
	}
	return res, nil
}
