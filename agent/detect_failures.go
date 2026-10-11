package agent

import (
	"encoding/json"
	"fmt"

	"self-improving-mcp/store"
)

// DetectRepeatedFailures finds the same tool+args_hash failing ≥3 times.
func DetectRepeatedFailures(calls []store.ToolCall) []DetectedFinding {
	type key struct{ tool, hash string }
	counts := map[key][]int64{}
	for _, c := range calls {
		if !c.IsError || c.ArgsHash == "" {
			continue
		}
		k := key{c.ToolName, c.ArgsHash}
		counts[k] = append(counts[k], c.ID)
	}
	var out []DetectedFinding
	for k, ids := range counts {
		if len(ids) < 3 {
			continue
		}
		ev, _ := json.Marshal(ids)
		out = append(out, DetectedFinding{
			Category:     "repeated_failure",
			Severity:     "error",
			Title:        fmt.Sprintf("repeated failure on %s", k.tool),
			Detail:       fmt.Sprintf("%d failures with same args hash", len(ids)),
			EvidenceJSON: string(ev),
		})
	}
	return out
}
