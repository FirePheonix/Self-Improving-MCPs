package agent

import (
	"encoding/json"
	"fmt"

	"self-improving-mcp/store"
)

// DetectRedundant finds consecutive identical tool+args_hash calls.
func DetectRedundant(calls []store.ToolCall) []DetectedFinding {
	var out []DetectedFinding
	for i := 1; i < len(calls); i++ {
		prev, cur := calls[i-1], calls[i]
		if prev.ToolName == cur.ToolName && prev.ArgsHash != "" && prev.ArgsHash == cur.ArgsHash {
			ev, _ := json.Marshal([]int64{prev.ID, cur.ID})
			out = append(out, DetectedFinding{
				Category:     "redundant",
				Severity:     "warn",
				Title:        fmt.Sprintf("redundant consecutive %s", cur.ToolName),
				Detail:       "same tool and args hash called back-to-back",
				EvidenceJSON: string(ev),
			})
		}
	}
	return out
}
