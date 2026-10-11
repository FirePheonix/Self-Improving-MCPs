package agent

import (
	"encoding/json"
	"fmt"
	"sort"

	"self-improving-mcp/store"
)

// DetectFrequentSequences finds bigrams that appear ≥3 times.
func DetectFrequentSequences(calls []store.ToolCall) []DetectedFinding {
	if len(calls) < 2 {
		return nil
	}
	type bigram struct{ a, b string }
	counts := map[bigram]int{}
	examples := map[bigram][]int64{}
	for i := 1; i < len(calls); i++ {
		bg := bigram{calls[i-1].ToolName, calls[i].ToolName}
		counts[bg]++
		if len(examples[bg]) < 6 {
			examples[bg] = append(examples[bg], calls[i-1].ID, calls[i].ID)
		}
	}
	type item struct {
		bg    bigram
		count int
	}
	var items []item
	for bg, n := range counts {
		if n >= 3 {
			items = append(items, item{bg, n})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].count > items[j].count })

	var out []DetectedFinding
	for _, it := range items {
		ev, _ := json.Marshal(examples[it.bg])
		out = append(out, DetectedFinding{
			Category:     "frequent_sequence",
			Severity:     "info",
			Title:        fmt.Sprintf("frequent sequence %s → %s", it.bg.a, it.bg.b),
			Detail:       fmt.Sprintf("seen %d times; candidate for a composed tool", it.count),
			EvidenceJSON: string(ev),
		})
	}
	return out
}
