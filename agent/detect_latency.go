package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"self-improving-mcp/store"
)

// DetectLatencyOutliers flags calls slower than max(avg*3, p95) per tool when n≥5.
func DetectLatencyOutliers(calls []store.ToolCall) []DetectedFinding {
	byTool := map[string][]store.ToolCall{}
	for _, c := range calls {
		byTool[c.ToolName] = append(byTool[c.ToolName], c)
	}
	var out []DetectedFinding
	for tool, group := range byTool {
		if len(group) < 5 {
			continue
		}
		lats := make([]float64, len(group))
		var sum float64
		for i, c := range group {
			lats[i] = float64(c.LatencyMs)
			sum += lats[i]
		}
		avg := sum / float64(len(lats))
		sorted := append([]float64(nil), lats...)
		sort.Float64s(sorted)
		p95 := sorted[int(math.Floor(0.95*float64(len(sorted)-1)))]
		threshold := math.Max(avg*3, p95)
		var ids []int64
		for _, c := range group {
			if float64(c.LatencyMs) > threshold && c.LatencyMs > 100 {
				ids = append(ids, c.ID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		ev, _ := json.Marshal(ids)
		out = append(out, DetectedFinding{
			Category:     "latency_outlier",
			Severity:     "warn",
			Title:        fmt.Sprintf("latency outliers on %s", tool),
			Detail:       fmt.Sprintf("threshold≈%.0fms; %d outliers", threshold, len(ids)),
			EvidenceJSON: string(ev),
		})
	}
	return out
}
