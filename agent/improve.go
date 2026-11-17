package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/mcp"
	selectiongate "self-improving-mcp/selection-gate"
)

// The improver writes description changes from evidence, not from finding summaries.
//
// It sees what the agent sees (every tool's current description) and what went wrong (dev
// requests where the agent called the wrong tool, no tool, or the right tool with arguments that
// cannot run). It proposes description patches, or hides a deprecated alias, measures them on the
// DEV split, and refines from the result. Only the best dev result is returned; the caller then
// judges it once on the SEALED split with the gate. Dev is where the improver may learn; sealed is
// where it is tested. Picking the best of several dev rounds is optimistic by construction (a
// lucky round looks good), which is exactly why the sealed gate, not the dev score, decides.
//
// It never proposes routing: a route renames a call but cannot repair arguments the agent built
// for the wrong tool, so routes do not fix wrong choices.

// ImproveOptions tunes ImproveDescriptions. Zero values take defaults.
type ImproveOptions struct {
	Rounds      int    // proposal rounds (default 3)
	Concurrency int    // parallel selector calls (default 8)
	Model       string // proposer model (default client.Reason)
	MaxExamples int    // failing requests shown per round (default 40)
	// MinGain is how many more dev tasks than the baseline a round must get right to count as an
	// improvement (default 2), so one noisy task cannot pick the winner.
	MinGain int
}

// ImproveRound is one proposal and its dev result.
type ImproveRound struct {
	Round      int                        `json:"round"`
	DevCorrect int                        `json:"dev_correct"`
	Fixed      int                        `json:"fixed"`
	Broken     int                        `json:"broken"`
	Patches    improvementsandbox.Overlay `json:"patches"`
	Rationale  string                     `json:"rationale,omitempty"`
	Rejected   []string                   `json:"rejected,omitempty"` // patches dropped by the guards, and why
	Err        string                     `json:"error,omitempty"`
	// Pruned lists tools whose patch was dropped because, on dev, it was involved in more broken
	// tasks than fixed ones; Patches and the counts above are then those of the pruned patch, and
	// Unpruned records what the full proposal scored.
	Pruned   []string `json:"pruned,omitempty"`
	Unpruned string   `json:"unpruned,omitempty"`
}

// MistakeGroup is one pattern of dev mistakes ("should be X, agent did Y") with how often it happened.
type MistakeGroup struct {
	Pattern  string   `json:"pattern"`
	Count    int      `json:"count"`
	Examples []string `json:"examples,omitempty"`
}

// ImproveResult is the outcome of ImproveDescriptions.
type ImproveResult struct {
	DevN            int                        `json:"dev_n"`
	BaselineCorrect int                        `json:"baseline_correct"`
	Failures        int                        `json:"failures"`
	Best            improvementsandbox.Overlay `json:"best"`       // tool_patches only; empty when nothing beat the baseline
	BestRound       int                        `json:"best_round"` // 0 when nothing beat the baseline
	BestCorrect     int                        `json:"best_correct"`
	Rounds          []ImproveRound             `json:"rounds"`
	Reason          string                     `json:"reason"`
	// Mistakes are the baseline's dev mistakes, grouped, most frequent first: what the improver was
	// asked to fix. Recorded so a reader can judge whether they are real mistakes or label artefacts.
	Mistakes []MistakeGroup `json:"mistakes,omitempty"`
}

const improveSystem = `You improve the descriptions of MCP tools so that an AI agent picks the right tool and calls it correctly.
You are given every tool's current description, and real requests the agent got wrong: which tool it should have called and what it did instead.
Rewrite the descriptions of the tools involved so the right choice is clear.
Rules:
- Keep every sentence that tells an agent how to use a tool after choosing it (result format, limits, side effects, failure behaviour, preconditions, parameter behaviour). Add the distinction; never shorten into a summary.
- Say plainly when to use this tool and when to use the neighbouring tool instead, naming it.
- Describe the general rule. Never copy the example requests, their file names, values or wording into a description.
- Write descriptions of a tool, not instructions to the model: no "ignore", "system prompt", "you must always".
- You may hide a tool only if its description says it is deprecated or an alias, and another listed tool does the same job.
- Change only tools involved in the mistakes.
Return JSON only: {"rationale":"one or two sentences","tool_patches":{"<exact tool name>":{"description":"<full new description>"}}}
To hide a tool use {"hidden":true} as its patch.`

// JSONCompleter is the one model call the improver needs. *Client implements it.
type JSONCompleter interface {
	CompleteJSON(ctx context.Context, model, system, user string) (string, error)
}

// ImproveDescriptions runs the dev loop described above. base is the overlay in force today; the
// returned Best contains only the new patches, to be merged onto base by the caller.
func ImproveDescriptions(ctx context.Context, client JSONCompleter, sel selectiongate.Selector, live []mcp.ToolInfo, base improvementsandbox.Overlay, dev []selectiongate.Task, opt ImproveOptions) (ImproveResult, error) {
	if client == nil {
		return ImproveResult{}, fmt.Errorf("improve: client is nil")
	}
	if len(dev) == 0 {
		return ImproveResult{}, fmt.Errorf("improve: no dev tasks to learn from")
	}
	if opt.Rounds <= 0 {
		opt.Rounds = 3
	}
	if opt.MaxExamples <= 0 {
		opt.MaxExamples = 40
	}
	if opt.MinGain <= 0 {
		opt.MinGain = 2
	}
	model := opt.Model
	if c, ok := client.(*Client); ok && model == "" {
		model = c.Reason
	}
	evalOpt := selectiongate.EvalOptions{Concurrency: opt.Concurrency}
	toolsFor := func(ov improvementsandbox.Overlay) []selectiongate.Tool {
		vis := mcp.VisibleTools(live, ov)
		out := make([]selectiongate.Tool, len(vis))
		for i, t := range vis {
			out[i] = selectiongate.Tool{Name: t.Name, Description: t.Description, Parameters: t.InputSchema}
		}
		return out
	}

	baseRun, err := selectiongate.Evaluate(ctx, sel, toolsFor(base), dev, base, evalOpt)
	if err != nil {
		return ImproveResult{}, fmt.Errorf("improve: baseline dev run: %w", err)
	}
	if n := countErrors(baseRun); n*20 > len(dev) {
		return ImproveResult{}, fmt.Errorf("improve: selector failed on %d of %d dev calls; refusing to learn from an unreliable baseline", n, len(dev))
	}
	res := ImproveResult{DevN: len(dev), BaselineCorrect: countCorrect(baseRun), Mistakes: groupMistakes(dev, baseRun)}
	res.Failures = res.DevN - res.BaselineCorrect
	if res.Failures == 0 {
		res.Reason = "the agent already handles every dev task; nothing to improve"
		return res, nil
	}

	known := map[string]bool{}
	for _, t := range live {
		known[t.Name] = true
	}
	bestCorrect := res.BaselineCorrect
	var feedback string
	for r := 1; r <= opt.Rounds; r++ {
		round := ImproveRound{Round: r}
		prompt := improvePrompt(toolsFor(base), dev, baseRun, opt.MaxExamples) + feedback
		raw, err := client.CompleteJSON(ctx, model, improveSystem, prompt)
		if err != nil {
			round.Err = err.Error()
			res.Rounds = append(res.Rounds, round)
			continue
		}
		patches, rationale, rejected := parseImprovement(raw, known, dev)
		round.Rationale, round.Rejected = rationale, rejected
		if len(patches.ToolPatches) == 0 {
			round.Err = "no usable patches"
			res.Rounds = append(res.Rounds, round)
			feedback = "\n\nYour previous answer had no usable patches. Reasons: " + strings.Join(rejected, "; ") + "\n"
			continue
		}
		measure := func(p improvementsandbox.Overlay) (selectiongate.Run, error) {
			cand := MergePatches(base, p)
			run, err := selectiongate.Evaluate(ctx, sel, toolsFor(cand), dev, cand, evalOpt)
			if err != nil {
				return run, err
			}
			if n := countErrors(run); n*20 > len(dev) {
				return run, fmt.Errorf("selector failed on %d of %d dev calls; round not counted", n, len(dev))
			}
			return run, nil
		}
		run, err := measure(patches)
		if err != nil {
			round.Err = err.Error()
			res.Rounds = append(res.Rounds, round)
			continue
		}
		// A proposal that rewrites many tools can fix some tasks and break more. Drop the patches
		// that were involved in more broken than fixed dev tasks and measure once more; keep
		// whichever scored better. Without this, refinement on Map diverged (282 baseline ->
		// 266, 256, 241 over three rounds) as each round rewrote more tools.
		if harmful := harmfulPatches(patches, dev, baseRun, run); len(harmful) > 0 && len(harmful) < len(patches.ToolPatches) {
			pruned := improvementsandbox.Overlay{ToolPatches: map[string]improvementsandbox.ToolPatch{}}
			drop := map[string]bool{}
			for _, h := range harmful {
				drop[h] = true
			}
			for k, v := range patches.ToolPatches {
				if !drop[k] {
					pruned.ToolPatches[k] = v
				}
			}
			if prunedRun, perr := measure(pruned); perr == nil && countCorrect(prunedRun) > countCorrect(run) {
				f, b := flips(baseRun, run)
				round.Unpruned = fmt.Sprintf("%d/%d (fixed %d, broke %d) before dropping %s", countCorrect(run), len(dev), f, b, strings.Join(harmful, ", "))
				round.Pruned, patches, run = harmful, pruned, prunedRun
			}
		}
		round.Patches = patches
		round.DevCorrect = countCorrect(run)
		var stillWrong, broken []string
		for i := range dev {
			b, c := baseRun.Outcomes[i].Correct, run.Outcomes[i].Correct
			switch {
			case !b && c:
				round.Fixed++
			case b && !c:
				round.Broken++
				broken = append(broken, describeMiss(dev[i], run.Outcomes[i]))
			case !b && !c:
				stillWrong = append(stillWrong, describeMiss(dev[i], run.Outcomes[i]))
			}
		}
		res.Rounds = append(res.Rounds, round)
		if round.DevCorrect >= res.BaselineCorrect+opt.MinGain && round.DevCorrect > bestCorrect {
			bestCorrect, res.Best, res.BestRound = round.DevCorrect, patches, r
		}
		pj, _ := json.Marshal(patches)
		feedback = fmt.Sprintf("\n\nYour previous patch was %s\nOn the dev requests it got %d of %d right (baseline %d): fixed %d, broke %d.\n",
			pj, round.DevCorrect, len(dev), res.BaselineCorrect, round.Fixed, round.Broken)
		if len(round.Pruned) > 0 {
			feedback += fmt.Sprintf("Your full proposal scored worse (%s); the patches for %s did more harm than good and were dropped from the patch shown above. Change fewer tools.\n",
				round.Unpruned, strings.Join(round.Pruned, ", "))
		}
		if len(broken) > 0 {
			feedback += "It BROKE these, which were right before (fix that first):\n" + bullets(broken, 10)
		}
		if len(stillWrong) > 0 {
			feedback += "Still wrong:\n" + bullets(stillWrong, 15)
		}
		feedback += "Propose a better complete patch.\n"
	}
	res.BestCorrect = bestCorrect
	switch {
	case res.BestRound == 0:
		res.Reason = fmt.Sprintf("no round beat the baseline by at least %d dev tasks", opt.MinGain)
	default:
		res.Reason = fmt.Sprintf("round %d: %d -> %d of %d dev tasks", res.BestRound, res.BaselineCorrect, bestCorrect, res.DevN)
	}
	return res, nil
}

// MergePatches returns base with patches applied on top (a patch replaces base's patch for that tool).
func MergePatches(base, patches improvementsandbox.Overlay) improvementsandbox.Overlay {
	out := base
	out.ToolPatches = map[string]improvementsandbox.ToolPatch{}
	for k, v := range base.ToolPatches {
		out.ToolPatches[k] = v
	}
	for k, v := range patches.ToolPatches {
		out.ToolPatches[k] = v
	}
	return out
}

func countErrors(r selectiongate.Run) int {
	n := 0
	for _, o := range r.Outcomes {
		if o.Err != "" {
			n++
		}
	}
	return n
}

func countCorrect(r selectiongate.Run) int {
	n := 0
	for _, o := range r.Outcomes {
		if o.Correct {
			n++
		}
	}
	return n
}

func describeMiss(t selectiongate.Task, o selectiongate.Outcome) string {
	want := t.ExpectedTool
	if len(t.AcceptableTools) > 0 {
		want += " (or " + strings.Join(t.AcceptableTools, ", ") + ")"
	}
	got := o.Served
	switch {
	case o.Err != "":
		got = "an error"
	case o.Picked == "":
		got = "no tool at all"
	case o.ArgsError != "" && t.Accepts(o.Served):
		got = o.Served + " with arguments that cannot run (" + o.ArgsError + ")"
	}
	return fmt.Sprintf("%q: should be %s, agent did %s", t.Query, want, got)
}

func bullets(lines []string, max int) string {
	var b strings.Builder
	for i, l := range lines {
		if i == max {
			fmt.Fprintf(&b, "- ... and %d more\n", len(lines)-max)
			break
		}
		b.WriteString("- " + l + "\n")
	}
	return b.String()
}

// improvePrompt lists the tools as the agent sees them and the baseline's mistakes, grouped by
// (expected, what happened) so the model sees patterns rather than single cases.
func improvePrompt(tools []selectiongate.Tool, dev []selectiongate.Task, base selectiongate.Run, maxExamples int) string {
	var b strings.Builder
	b.WriteString("TOOLS (as the agent sees them):\n\n")
	for _, t := range tools {
		desc := strings.TrimSpace(t.Description)
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "TOOL %s\n", t.Name)
		if p := paramSummary(t.Parameters); p != "" {
			fmt.Fprintf(&b, "parameters: %s\n", p)
		}
		fmt.Fprintf(&b, "%s\n\n", desc)
	}
	b.WriteString("MISTAKES on real requests (grouped):\n\n")
	shown := 0
	for _, g := range groupMistakes(dev, base) {
		if shown >= maxExamples {
			break
		}
		fmt.Fprintf(&b, "%d times: %s. Examples:\n", g.Count, g.Pattern)
		for _, ex := range g.Examples {
			if shown >= maxExamples {
				break
			}
			fmt.Fprintf(&b, "  - %q\n", ex)
			shown++
		}
	}
	return b.String()
}

// groupMistakes groups a run's wrong dev outcomes by (expected, what happened), most frequent first
// (ties keep first-seen order), with up to 3 example requests each.
func groupMistakes(dev []selectiongate.Task, run selectiongate.Run) []MistakeGroup {
	idx := map[string]int{}
	var out []MistakeGroup
	for i, t := range dev {
		o := run.Outcomes[i]
		if o.Correct {
			continue
		}
		miss := describeMiss(t, o)
		key := miss[strings.Index(miss, ": should be ")+2:]
		j, ok := idx[key]
		if !ok {
			j = len(out)
			idx[key] = j
			out = append(out, MistakeGroup{Pattern: key})
		}
		out[j].Count++
		if len(out[j].Examples) < 3 {
			out[j].Examples = append(out[j].Examples, t.Query)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Count > out[b].Count })
	return out
}

// harmfulPatches attributes each changed dev outcome to the patched tools it involves (the expected
// and acceptable tools, and the tools served before and after) and returns, sorted, the patched tools
// involved in more broken than fixed tasks. It is a heuristic: a patch can matter without being
// named, but a description mostly moves choices to or away from its own tool.
func harmfulPatches(patches improvementsandbox.Overlay, dev []selectiongate.Task, base, cand selectiongate.Run) []string {
	fixed, broken := map[string]int{}, map[string]int{}
	for i, t := range dev {
		b, c := base.Outcomes[i], cand.Outcomes[i]
		if b.Correct == c.Correct {
			continue
		}
		involved := map[string]bool{t.ExpectedTool: true, b.Served: true, c.Served: true}
		for _, a := range t.AcceptableTools {
			involved[a] = true
		}
		for tool := range involved {
			if _, patched := patches.ToolPatches[tool]; !patched {
				continue
			}
			if c.Correct {
				fixed[tool]++
			} else {
				broken[tool]++
			}
		}
	}
	var out []string
	for tool := range patches.ToolPatches {
		if broken[tool] > fixed[tool] {
			out = append(out, tool)
		}
	}
	sort.Strings(out)
	return out
}

// flips counts tasks fixed and broken from base to cand.
func flips(base, cand selectiongate.Run) (fixed, broken int) {
	for i := range base.Outcomes {
		switch b, c := base.Outcomes[i].Correct, cand.Outcomes[i].Correct; {
		case !b && c:
			fixed++
		case b && !c:
			broken++
		}
	}
	return fixed, broken
}

// paramSummary renders a tool's input schema compactly ("path (string, required), recursive (boolean)"),
// in property order sorted by name. The agent sees the schema when it picks and builds a call, so the
// improver must too: some mistakes are argument mistakes, and with a terse or missing description the
// parameters are most of what tells two tools apart. Any schema shape is accepted (map, SDK type);
// one that does not decode as a JSON object schema yields "".
func paramSummary(schema any) string {
	if schema == nil {
		return ""
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return ""
	}
	var s struct {
		Properties map[string]struct {
			Type  any `json:"type"`
			Items *struct {
				Type any `json:"type"`
			} `json:"items"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if json.Unmarshal(raw, &s) != nil || len(s.Properties) == 0 {
		return ""
	}
	req := map[string]bool{}
	for _, r := range s.Required {
		req[r] = true
	}
	typeName := func(v any) string {
		switch x := v.(type) {
		case string:
			return x
		case []any:
			parts := make([]string, 0, len(x))
			for _, p := range x {
				if ps, ok := p.(string); ok {
					parts = append(parts, ps)
				}
			}
			return strings.Join(parts, "|")
		}
		return ""
	}
	names := make([]string, 0, len(s.Properties))
	for n := range s.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		p := s.Properties[n]
		var attrs []string
		if ty := typeName(p.Type); ty != "" {
			if ty == "array" && p.Items != nil {
				if it := typeName(p.Items.Type); it != "" {
					ty = "array of " + it
				}
			}
			attrs = append(attrs, ty)
		}
		if req[n] {
			attrs = append(attrs, "required")
		}
		if len(attrs) > 0 {
			parts = append(parts, fmt.Sprintf("%s (%s)", n, strings.Join(attrs, ", ")))
		} else {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, ", ")
}

// parseImprovement keeps only patches that are safe to measure: known tool, a non-empty description
// or a hide, no copied dev request, and never hiding every tool. Everything else is reported.
func parseImprovement(raw string, known map[string]bool, dev []selectiongate.Task) (improvementsandbox.Overlay, string, []string) {
	var parsed struct {
		Rationale   string `json:"rationale"`
		ToolPatches map[string]struct {
			Description *string `json:"description"`
			Hidden      bool    `json:"hidden"`
		} `json:"tool_patches"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return improvementsandbox.Overlay{}, "", []string{"answer was not valid JSON: " + err.Error()}
	}
	out := improvementsandbox.Overlay{ToolPatches: map[string]improvementsandbox.ToolPatch{}}
	var rejected []string
	hidden := 0
	names := make([]string, 0, len(parsed.ToolPatches))
	for n := range parsed.ToolPatches {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		p := parsed.ToolPatches[name]
		switch {
		case !known[name]:
			rejected = append(rejected, fmt.Sprintf("%s: no such tool", name))
			continue
		case p.Hidden:
			out.ToolPatches[name] = improvementsandbox.ToolPatch{Hidden: true}
			hidden++
			continue
		case p.Description == nil || strings.TrimSpace(*p.Description) == "":
			rejected = append(rejected, fmt.Sprintf("%s: empty description", name))
			continue
		}
		if q := copiedQuery(*p.Description, dev); q != "" {
			rejected = append(rejected, fmt.Sprintf("%s: description copies a dev request (%q); describe the rule instead", name, q))
			continue
		}
		d := strings.TrimSpace(*p.Description)
		out.ToolPatches[name] = improvementsandbox.ToolPatch{Description: &d}
	}
	if hidden > 0 && hidden >= len(known) {
		return improvementsandbox.Overlay{}, parsed.Rationale, append(rejected, "would hide every tool")
	}
	return out, parsed.Rationale, rejected
}

// copiedQuery returns a dev request whose text (or a long run of it) appears in the description.
func copiedQuery(desc string, dev []selectiongate.Task) string {
	d := selectiongate.NormalizeQuery(desc)
	for _, t := range dev {
		q := selectiongate.NormalizeQuery(t.Query)
		words := strings.Fields(q)
		const run = 8 // eight consecutive words of a request is copying, not describing
		for i := 0; i+run <= len(words); i++ {
			if strings.Contains(d, strings.Join(words[i:i+run], " ")) {
				return t.Query
			}
		}
		if len(words) < run && len(q) >= 25 && strings.Contains(d, q) {
			return t.Query
		}
	}
	return ""
}
