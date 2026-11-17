package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/mcp"
	selectiongate "self-improving-mcp/selection-gate"
)

// descAgent is a simulated agent whose choice depends on what the descriptions say, like a real
// one: for a request about several files it picks read_multiple_files only when that tool's
// description says to use it for two or more files; otherwise it falls back to the deprecated
// read_file, unless read_file is hidden.
type descAgent struct{}

func (descAgent) Select(_ context.Context, q string, tools []selectiongate.Tool) (string, error) {
	desc := map[string]string{}
	for _, t := range tools {
		desc[t.Name] = t.Description
	}
	if strings.Contains(q, " and ") { // a multi-file request
		if strings.Contains(desc["read_multiple_files"], "two or more") {
			return "read_multiple_files", nil
		}
		if _, ok := desc["read_file"]; ok {
			return "read_file", nil
		}
		return "read_text_file", nil
	}
	return "read_text_file", nil
}

type scriptedLLM struct {
	mu      sync.Mutex
	answers []string
	prompts []string
}

func (s *scriptedLLM) CompleteJSON(_ context.Context, _, _, user string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompts = append(s.prompts, user)
	a := s.answers[min(len(s.prompts)-1, len(s.answers)-1)]
	return a, nil
}

var liveFS = []mcp.ToolInfo{
	{Name: "read_file", Description: "Read a file. DEPRECATED: use read_text_file."},
	{Name: "read_text_file", Description: "Read one file as text."},
	{Name: "read_multiple_files", Description: "Read several files."},
}

func devTasks() []selectiongate.Task {
	var out []selectiongate.Task
	for i := 0; i < 6; i++ {
		out = append(out, selectiongate.Task{ID: fmt.Sprint("m", i), Query: fmt.Sprintf("show file a%d and file b%d", i, i), ExpectedTool: "read_multiple_files"})
		out = append(out, selectiongate.Task{ID: fmt.Sprint("s", i), Query: fmt.Sprintf("show file c%d", i), ExpectedTool: "read_text_file"})
	}
	return out
}

const noEffect = `{"rationale":"r","tool_patches":{"read_text_file":{"description":"Read exactly one file as text."}}}`
const fixes = `{"rationale":"r","tool_patches":{"read_multiple_files":{"description":"Read several files. Use this whenever a request names two or more files."}}}`

func TestImproverRefinesFromFeedbackAndKeepsTheBestDevRound(t *testing.T) {
	llm := &scriptedLLM{answers: []string{noEffect, fixes, noEffect}}
	res, err := ImproveDescriptions(context.Background(), llm, descAgent{}, liveFS, improvementsandbox.Overlay{}, devTasks(), ImproveOptions{Rounds: 3, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BaselineCorrect != 6 || res.Failures != 6 {
		t.Fatalf("baseline: %+v", res)
	}
	if res.BestRound != 2 || res.BestCorrect != 12 || res.Best.ToolPatches["read_multiple_files"].Description == nil {
		t.Fatalf("best: round=%d correct=%d", res.BestRound, res.BestCorrect)
	}
	if len(res.Rounds) != 3 || res.Rounds[0].Fixed != 0 || res.Rounds[1].Fixed != 6 || res.Rounds[1].Broken != 0 {
		t.Fatalf("rounds: %+v", res.Rounds)
	}
	// round 2 was told what round 1 did and what is still wrong
	if !strings.Contains(llm.prompts[1], "Your previous patch") || !strings.Contains(llm.prompts[1], "Still wrong") {
		t.Fatalf("no feedback in round 2 prompt:\n%s", llm.prompts[1])
	}
	// the first prompt shows the tools as the agent sees them and the grouped mistakes
	for _, must := range []string{"TOOL read_multiple_files", "Read several files.", "6 times: should be read_multiple_files, agent did read_file"} {
		if !strings.Contains(llm.prompts[0], must) {
			t.Errorf("prompt missing %q", must)
		}
	}
	if len(res.Best.Routing) != 0 || len(res.Best.Cache) != 0 || len(res.Best.ComposedTools) != 0 {
		t.Fatal("the improver must only change what agents read")
	}
}

func TestHidingADeprecatedAliasIsAUsableFix(t *testing.T) {
	hide := `{"rationale":"read_file is a deprecated alias","tool_patches":{"read_file":{"hidden":true}}}`
	// with read_file hidden the agent falls back to read_text_file: still wrong for multi-file
	// requests, so hiding alone must NOT be reported as an improvement here
	res, err := ImproveDescriptions(context.Background(), &scriptedLLM{answers: []string{hide}}, descAgent{}, liveFS, improvementsandbox.Overlay{}, devTasks(), ImproveOptions{Rounds: 1, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Rounds[0].Patches.ToolPatches["read_file"].Hidden || res.BestRound != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestNothingToImproveMakesNoModelCall(t *testing.T) {
	llm := &scriptedLLM{answers: []string{fixes}}
	tasks := []selectiongate.Task{{ID: "s", Query: "show file c", ExpectedTool: "read_text_file"}}
	res, err := ImproveDescriptions(context.Background(), llm, descAgent{}, liveFS, improvementsandbox.Overlay{}, tasks, ImproveOptions{Model: "m"})
	if err != nil || len(llm.prompts) != 0 || res.Reason == "" {
		t.Fatalf("%v %+v prompts=%d", err, res, len(llm.prompts))
	}
}

func TestMinGainStopsAOneTaskWinner(t *testing.T) {
	// a dev set where the fix changes exactly one task
	tasks := []selectiongate.Task{
		{ID: "m", Query: "show file a and file b", ExpectedTool: "read_multiple_files"},
		{ID: "s", Query: "show file c", ExpectedTool: "read_text_file"},
	}
	res, err := ImproveDescriptions(context.Background(), &scriptedLLM{answers: []string{fixes}}, descAgent{}, liveFS, improvementsandbox.Overlay{}, tasks, ImproveOptions{Rounds: 1, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Rounds[0].Fixed != 1 || res.BestRound != 0 {
		t.Fatalf("a one-task gain must not be reported as an improvement: %+v", res)
	}
}

func TestGuardsRejectUnsafePatches(t *testing.T) {
	known := map[string]bool{"read_file": true, "read_text_file": true}
	dev := []selectiongate.Task{{ID: "a", Query: "please show me the contents of the quarterly budget spreadsheet for review", ExpectedTool: "read_text_file"}}
	raw := `{"rationale":"x","tool_patches":{
	  "ghost":{"description":"x"},
	  "read_text_file":{"description":"Use when asked: show me the contents of the quarterly budget spreadsheet for review."},
	  "read_file":{"description":"   "}}}`
	ov, _, rejected := parseImprovement(raw, known, dev)
	if len(ov.ToolPatches) != 0 || len(rejected) != 3 {
		t.Fatalf("patches=%v rejected=%v", ov.ToolPatches, rejected)
	}
	joined := strings.Join(rejected, " | ")
	for _, must := range []string{"ghost: no such tool", "copies a dev request", "read_file: empty description"} {
		if !strings.Contains(joined, must) {
			t.Errorf("missing %q in %s", must, joined)
		}
	}
	all := `{"tool_patches":{"read_file":{"hidden":true},"read_text_file":{"hidden":true}}}`
	if ov, _, rej := parseImprovement(all, known, dev); len(ov.ToolPatches) != 0 || !strings.Contains(strings.Join(rej, ""), "every tool") {
		t.Fatalf("hiding everything must be refused: %v %v", ov, rej)
	}
	if _, _, rej := parseImprovement("not json", known, dev); len(rej) != 1 {
		t.Fatal("bad json must be reported")
	}
}

func TestMergePatchesOverridesPerToolAndKeepsTheRest(t *testing.T) {
	a, b := "base a", "new a"
	base := improvementsandbox.Overlay{
		ToolPatches: map[string]improvementsandbox.ToolPatch{"a": {Description: &a}, "keep": {Hidden: true}},
		Cache:       []improvementsandbox.CacheRule{{ToolName: "a", TTLSeconds: 5}},
	}
	got := MergePatches(base, improvementsandbox.Overlay{ToolPatches: map[string]improvementsandbox.ToolPatch{"a": {Description: &b}}})
	if *got.ToolPatches["a"].Description != "new a" || !got.ToolPatches["keep"].Hidden || len(got.Cache) != 1 {
		t.Fatalf("%+v", got)
	}
	if *base.ToolPatches["a"].Description != "base a" {
		t.Fatal("base was mutated")
	}
}

// failingAgent errors on every call (like an API that rate-limits each request).
type failingAgent struct{}

func (failingAgent) Select(context.Context, string, []selectiongate.Tool) (string, error) {
	return "", fmt.Errorf("429 Too Many Requests")
}

func TestImproverRefusesAnUnreliableBaseline(t *testing.T) {
	llm := &scriptedLLM{answers: []string{fixes}}
	_, err := ImproveDescriptions(context.Background(), llm, failingAgent{}, liveFS, improvementsandbox.Overlay{}, devTasks(), ImproveOptions{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "unreliable baseline") {
		t.Fatalf("want refusal, got %v", err)
	}
	if len(llm.prompts) != 0 {
		t.Fatal("must not ask the proposer to learn from failures")
	}
}

// fatalAgent fails with an error that says no call can succeed.
type fatalAgent struct{}

func (fatalAgent) Select(context.Context, string, []selectiongate.Tool) (string, error) {
	return "", classify(fmt.Errorf(`429 {"code":"credit_balance_exhausted"}`))
}

func TestFatalAPIErrorStopsTheRun(t *testing.T) {
	_, err := selectiongate.Evaluate(context.Background(), fatalAgent{}, nil, devTasks(), improvementsandbox.Overlay{}, selectiongate.EvalOptions{Concurrency: 2})
	if err == nil || !strings.Contains(err.Error(), "run stopped") {
		t.Fatalf("a fatal API error must stop the run, got %v", err)
	}
}

// The improver must see the parameters the agent sees: some mistakes are argument mistakes, and for
// an undocumented tool the schema is most of what tells two tools apart.
func TestImprovePromptShowsParametersAndMissingDescriptions(t *testing.T) {
	tools := []selectiongate.Tool{
		{Name: "read_multiple_files", Description: "", Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
			"required":   []any{"paths"},
		}},
		{Name: "read_text_file", Description: "Read one file.", Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
				"head": map[string]any{"type": []any{"number", "null"}},
			},
			"required": []any{"path"},
		}},
		{Name: "no_schema", Description: "x"},
	}
	dev := []selectiongate.Task{{ID: "1", Query: "show a and b", ExpectedTool: "read_multiple_files"}}
	base := selectiongate.Run{Outcomes: []selectiongate.Outcome{{Picked: "read_text_file", Served: "read_text_file"}}}
	p := improvePrompt(tools, dev, base, 10)
	for _, want := range []string{
		"TOOL read_multiple_files\nparameters: paths (array of string, required)\n(no description)",
		"TOOL read_text_file\nparameters: head (number|null), path (string, required)\nRead one file.",
		"TOOL no_schema\nx",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt is missing %q\n---\n%s", want, p)
		}
	}
	if got := paramSummary("not a schema"); got != "" {
		t.Errorf("a non-object schema must render as nothing, got %q", got)
	}
}

// harmedAgent is descAgent, except that a read_text_file description warning against single files
// sends single-file requests to the deprecated read_file: a patch that breaks tasks.
type harmedAgent struct{}

func (harmedAgent) Select(ctx context.Context, q string, tools []selectiongate.Tool) (string, error) {
	for _, t := range tools {
		if t.Name == "read_text_file" && strings.Contains(t.Description, "NOT for single files") && !strings.Contains(q, " and ") {
			return "read_file", nil
		}
	}
	return descAgent{}.Select(ctx, q, tools)
}

// One proposal, two patches: one fixes the 6 multi-file tasks, the other breaks the 6 single-file
// tasks, so the full proposal is no better than the baseline. Pruning must drop the harmful patch,
// keep the good one, and report what it did.
func TestImproverPrunesPatchesThatDoMoreHarmThanGood(t *testing.T) {
	mixed := `{"rationale":"r","tool_patches":{
	  "read_multiple_files":{"description":"Read several files. Use this whenever a request names two or more files."},
	  "read_text_file":{"description":"Read text. NOT for single files."}}}`
	llm := &scriptedLLM{answers: []string{mixed, noEffect}}
	res, err := ImproveDescriptions(context.Background(), llm, harmedAgent{}, liveFS, improvementsandbox.Overlay{}, devTasks(), ImproveOptions{Rounds: 2, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	r := res.Rounds[0]
	if len(r.Pruned) != 1 || r.Pruned[0] != "read_text_file" || r.DevCorrect != 12 || r.Broken != 0 || !strings.Contains(r.Unpruned, "fixed 6, broke 6") {
		t.Fatalf("round 1: %+v", r)
	}
	if _, kept := r.Patches.ToolPatches["read_text_file"]; kept || res.BestRound != 1 || res.BestCorrect != 12 {
		t.Fatalf("best must be the pruned patch: round=%d correct=%d patches=%v", res.BestRound, res.BestCorrect, r.Patches.ToolPatches)
	}
	if !strings.Contains(llm.prompts[1], "did more harm than good") {
		t.Fatalf("round 2 was not told about the pruning:\n%s", llm.prompts[1])
	}
	if len(res.Mistakes) != 1 || res.Mistakes[0].Count != 6 || res.Mistakes[0].Pattern != "should be read_multiple_files, agent did read_file" || len(res.Mistakes[0].Examples) != 3 {
		t.Fatalf("mistakes: %+v", res.Mistakes)
	}
}

// A single-patch proposal is never pruned to nothing, and a patch that only helps is kept whole.
func TestPruningLeavesGoodAndSinglePatchesAlone(t *testing.T) {
	res, err := ImproveDescriptions(context.Background(), &scriptedLLM{answers: []string{fixes}}, descAgent{}, liveFS, improvementsandbox.Overlay{}, devTasks(), ImproveOptions{Rounds: 1, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rounds[0].Pruned) != 0 || res.BestCorrect != 12 {
		t.Fatalf("%+v", res.Rounds[0])
	}
}
