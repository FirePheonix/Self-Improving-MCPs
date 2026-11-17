package selectiongate

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// DraftTask is a machine-written candidate golden task. It is NOT usable by the gate until a
// person sets Approved after reading it: a label written by the same kind of model that will be
// judged would otherwise grade its own homework.
type DraftTask struct {
	ID           string `json:"id"`
	Query        string `json:"query"`
	ExpectedTool string `json:"expected_tool"`
	// BaselinePick is what the selector chooses today, when recorded. A mismatch with
	// ExpectedTool marks a real confusion (or a wrong label): review those first.
	BaselinePick string `json:"baseline_pick,omitempty"`
	// AcceptableTools: a reviewer adds tools that do the same job, so picking one is not a mistake.
	AcceptableTools []string `json:"acceptable_tools,omitempty"`
	Approved        bool     `json:"approved"`
}

// Draft is the reviewable file written by `evolve golden draft`.
type Draft struct {
	Name  string      `json:"name"`
	Model string      `json:"model"`
	Tasks []DraftTask `json:"tasks"`
}

// LLM is the single call the drafter needs.
type LLM func(ctx context.Context, system, user string) (string, error)

const draftSystem = `You write test queries for evaluating whether an AI agent picks the right tool.
For each tool you are given, write realistic user requests that this tool, and only this tool, is the best answer to.
Rules:
- Write requests the way a user would, in plain language. Do not mention the tool name or copy its description.
- Each request must have exactly one best tool among the tools listed. If another listed tool could do it equally well, do not write it.
- Prefer requests that are close to a neighbouring tool but still clearly resolved by the given one.
- Vary phrasing and concrete details (paths, names, sizes).
Return a JSON object: {"tasks":[{"tool":"<exact tool name>","query":"<request>"}]}`

// DraftPrompt builds the user message listing tools and how many tasks to write for each.
func DraftPrompt(tools []Tool, perTool int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Write %d requests for each of these tools.\n\n", perTool)
	for _, t := range tools {
		fmt.Fprintf(&b, "TOOL %s\n%s\n\n", t.Name, strings.TrimSpace(t.Description))
	}
	return b.String()
}

// DraftOptions controls GenerateDraft.
type DraftOptions struct {
	Name    string
	Model   string
	PerTool int // tasks requested per tool (default 4)
}

// GenerateDraft asks the model for labeled queries and returns only those that pass the
// mechanical checks in ParseDraft, plus the reasons others were dropped. Nothing is approved.
func GenerateDraft(ctx context.Context, llm LLM, tools []Tool, opt DraftOptions) (*Draft, []string, error) {
	if len(tools) == 0 {
		return nil, nil, fmt.Errorf("no tools to draft tasks for")
	}
	if opt.PerTool <= 0 {
		opt.PerTool = 4
	}
	out, err := llm(ctx, draftSystem, DraftPrompt(tools, opt.PerTool))
	if err != nil {
		return nil, nil, fmt.Errorf("draft golden tasks: %w", err)
	}
	tasks, dropped, err := ParseDraft(out, tools)
	if err != nil {
		return nil, dropped, err
	}
	return &Draft{Name: opt.Name, Model: opt.Model, Tasks: tasks}, dropped, nil
}

// ParseDraft strictly parses the model's JSON and drops tasks that cannot be trusted.
// Dropped: unknown tool names (hallucinated labels), empty queries, queries containing the
// tool's own name (the answer is handed over, so the task tests nothing), and duplicates.
// Ids are derived from the query, so they are stable across runs.
func ParseDraft(raw string, tools []Tool) ([]DraftTask, []string, error) {
	var parsed struct {
		Tasks []struct {
			Tool  string `json:"tool"`
			Query string `json:"query"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, nil, fmt.Errorf("parse drafted tasks: %w", err)
	}
	known := map[string]bool{}
	for _, t := range tools {
		known[t.Name] = true
	}
	var kept []DraftTask
	var dropped []string
	seen := map[string]bool{}
	for _, p := range parsed.Tasks {
		q := strings.TrimSpace(p.Query)
		switch {
		case q == "":
			dropped = append(dropped, fmt.Sprintf("%s: empty query", p.Tool))
			continue
		case !known[p.Tool]:
			dropped = append(dropped, fmt.Sprintf("%q: unknown tool %q", q, p.Tool))
			continue
		case leaksToolName(q, p.Tool):
			dropped = append(dropped, fmt.Sprintf("%q: query contains the tool name %q", q, p.Tool))
			continue
		}
		norm := NormalizeQuery(q)
		if seen[norm] {
			dropped = append(dropped, fmt.Sprintf("%q: duplicate query", q))
			continue
		}
		seen[norm] = true
		kept = append(kept, DraftTask{ID: taskID(norm), Query: q, ExpectedTool: p.Tool})
	}
	if len(kept) == 0 {
		return nil, dropped, fmt.Errorf("model returned no usable tasks (%d dropped)", len(dropped))
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
	return kept, dropped, nil
}

func leaksToolName(query, tool string) bool {
	q := strings.ToLower(query)
	t := strings.ToLower(tool)
	return strings.Contains(q, t) || strings.Contains(q, strings.ReplaceAll(t, "_", " "))
}

func taskID(normQuery string) string {
	sum := sha256.Sum256([]byte(normQuery))
	return fmt.Sprintf("g-%x", sum[:4])
}

// LoadDraft reads a draft file.
func LoadDraft(path string) (*Draft, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read draft: %w", err)
	}
	var d Draft
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("parse draft %s: %w", path, err)
	}
	return &d, nil
}

// Save writes the draft as indented JSON.
func (d *Draft) Save(path string) error {
	raw, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// Approve builds a GoldenSet from the tasks a person marked approved.
//
// The dev/sealed split is a function of the normalized query, not of position, so the same
// query can never land on both sides and the split does not shift when tasks are added.
// sealedFrac is the share sent to sealed (default 0.5). knownTools, when given, rejects labels
// naming a tool that no longer exists. The result is validated like any golden set.
func (d *Draft) Approve(sealedFrac float64, knownTools []string) (*GoldenSet, error) {
	if sealedFrac <= 0 || sealedFrac >= 1 {
		sealedFrac = 0.5
	}
	known := map[string]bool{}
	for _, n := range knownTools {
		known[n] = true
	}
	g := &GoldenSet{Name: d.Name}
	for _, t := range d.Tasks {
		if !t.Approved {
			continue
		}
		if len(known) > 0 && !known[t.ExpectedTool] {
			return nil, fmt.Errorf("approved task %q expects unknown tool %q", t.ID, t.ExpectedTool)
		}
		split := SplitDev
		if splitBucket(t.Query) < sealedFrac {
			split = SplitSealed
		}
		for _, a := range t.AcceptableTools {
			if len(known) > 0 && !known[a] {
				return nil, fmt.Errorf("approved task %q lists unknown acceptable tool %q", t.ID, a)
			}
		}
		g.Tasks = append(g.Tasks, Task{ID: t.ID, Query: t.Query, ExpectedTool: t.ExpectedTool, AcceptableTools: t.AcceptableTools, Split: split})
	}
	if len(g.Tasks) == 0 {
		return nil, fmt.Errorf("no approved tasks: set \"approved\": true on the tasks you have reviewed")
	}
	if err := g.Validate(); err != nil {
		return nil, err
	}
	return g, nil
}

func splitBucket(query string) float64 {
	sum := sha256.Sum256([]byte("split\x00" + NormalizeQuery(query)))
	return float64(binary.BigEndian.Uint32(sum[:4])) / float64(1<<32)
}
