package selectiongate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Split says who may see a golden task.
type Split string

const (
	// SplitDev tasks may be shown to the improvement agent (to ground proposals).
	SplitDev Split = "dev"
	// SplitSealed tasks are used only by the gate. The improver must never see them.
	SplitSealed Split = "sealed"
)

// Task is one labeled tool-selection example: given Query, an agent should pick ExpectedTool.
type Task struct {
	ID           string `json:"id"`
	Query        string `json:"query"`
	ExpectedTool string `json:"expected_tool"`
	// AcceptableTools are other tools that serve the request equally well (for example the same
	// action offered by two servers, or an alias). Picking one counts as correct. Without this,
	// the gate punishes the agent for choosing an equivalent tool and rewards overlays that merely
	// steer it to the labeler's arbitrary choice.
	AcceptableTools []string `json:"acceptable_tools,omitempty"`
	Split           Split    `json:"split"`
}

// Accepts reports whether calling tool satisfies the task.
func (t Task) Accepts(tool string) bool {
	if tool == "" {
		return false
	}
	if tool == t.ExpectedTool {
		return true
	}
	for _, a := range t.AcceptableTools {
		if a == tool {
			return true
		}
	}
	return false
}

// GoldenSet is a labeled set of selection tasks split into dev and sealed parts.
type GoldenSet struct {
	Name  string `json:"name"`
	Tasks []Task `json:"tasks"`
}

// LoadGolden reads and validates a golden set from a JSON file.
func LoadGolden(path string) (*GoldenSet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read golden set: %w", err)
	}
	var g GoldenSet
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("parse golden set %s: %w", path, err)
	}
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("golden set %s: %w", path, err)
	}
	return &g, nil
}

// Validate checks structure and guards against leakage between dev and sealed.
func (g *GoldenSet) Validate() error {
	if len(g.Tasks) == 0 {
		return fmt.Errorf("no tasks")
	}
	ids := map[string]bool{}
	queryByNorm := map[string]Split{}
	sealed := 0
	for i, t := range g.Tasks {
		if strings.TrimSpace(t.ID) == "" {
			return fmt.Errorf("task %d: empty id", i)
		}
		if ids[t.ID] {
			return fmt.Errorf("task %q: duplicate id", t.ID)
		}
		ids[t.ID] = true
		if strings.TrimSpace(t.Query) == "" {
			return fmt.Errorf("task %q: empty query", t.ID)
		}
		if strings.TrimSpace(t.ExpectedTool) == "" {
			return fmt.Errorf("task %q: empty expected_tool", t.ID)
		}
		for _, a := range t.AcceptableTools {
			if strings.TrimSpace(a) == "" {
				return fmt.Errorf("task %q: empty name in acceptable_tools", t.ID)
			}
		}
		if t.Split != SplitDev && t.Split != SplitSealed {
			return fmt.Errorf("task %q: split must be %q or %q, got %q", t.ID, SplitDev, SplitSealed, t.Split)
		}
		if t.Split == SplitSealed {
			sealed++
		}
		// Leakage guard: the same query on both sides would let the improver memorize the answer.
		norm := normalizeQuery(t.Query)
		if prev, seen := queryByNorm[norm]; seen && prev != t.Split {
			return fmt.Errorf("task %q: query also appears in the %q split (leakage)", t.ID, prev)
		}
		queryByNorm[norm] = t.Split
	}
	if sealed == 0 {
		return fmt.Errorf("no sealed tasks: the gate has nothing to judge on")
	}
	return nil
}

// Dev returns the tasks the improvement agent may see.
func (g *GoldenSet) Dev() []Task { return g.bySplit(SplitDev) }

// Sealed returns the tasks only the gate may use.
func (g *GoldenSet) Sealed() []Task { return g.bySplit(SplitSealed) }

func (g *GoldenSet) bySplit(s Split) []Task {
	var out []Task
	for _, t := range g.Tasks {
		if t.Split == s {
			out = append(out, t)
		}
	}
	return out
}

// SealedHash fingerprints the sealed set (ids, queries, labels), independent of file order.
// A gate decision records it, so a change to the sealed set after the fact is detectable.
func (g *GoldenSet) SealedHash() string {
	lines := make([]string, 0)
	for _, t := range g.Sealed() {
		line := t.ID + "\x00" + normalizeQuery(t.Query) + "\x00" + t.ExpectedTool
		if len(t.AcceptableTools) > 0 { // appended only when present, so older fingerprints are unchanged
			acc := append([]string(nil), t.AcceptableTools...)
			sort.Strings(acc)
			line += "\x00" + strings.Join(acc, ",")
		}
		lines = append(lines, line)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

// NormalizeQuery is the canonical form used to detect the same query on both sides of the split
// (case-insensitive, whitespace-collapsed). Exported so tooling that builds golden sets drops
// colliding tasks the same way Validate would reject them.
func NormalizeQuery(q string) string { return normalizeQuery(q) }

func normalizeQuery(q string) string {
	return strings.ToLower(strings.Join(strings.Fields(q), " "))
}
