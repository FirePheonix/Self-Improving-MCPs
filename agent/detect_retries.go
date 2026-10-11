package agent

import (
	"encoding/json"
	"fmt"

	"self-improving-mcp/store"
)

// DetectRetries finds bursts of errors on the same tool (≥3 in a window of 5).
func DetectRetries(calls []store.ToolCall) []DetectedFinding {
	var out []DetectedFinding
	for i := 0; i < len(calls); i++ {
		if !calls[i].IsError {
			continue
		}
		tool := calls[i].ToolName
		var ids []int64
		end := i
		for j := i; j < len(calls) && j < i+5; j++ {
			if calls[j].ToolName == tool && calls[j].IsError {
				ids = append(ids, calls[j].ID)
				end = j
			}
		}
		if len(ids) >= 3 {
			ev, _ := json.Marshal(ids)
			out = append(out, DetectedFinding{
				Category:     "retry_storm",
				Severity:     "error",
				Title:        fmt.Sprintf("retry storm on %s", tool),
				Detail:       fmt.Sprintf("%d errors within a short window", len(ids)),
				EvidenceJSON: string(ev),
			})
			i = end
		}
	}
	return out
}
