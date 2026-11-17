package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"self-improving-mcp/agent"
	"self-improving-mcp/config"
	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/mcp"
	selectiongate "self-improving-mcp/selection-gate"
)

// benchResult is one domain's outcome.
type benchResult struct {
	Domain             string `json:"domain"`
	Tools              int    `json:"tools"`
	Dev                int    `json:"dev"`
	Sealed             int    `json:"sealed"`
	WithEquivalents    int    `json:"tasks_with_equivalent_tools"`
	BaseExactName      int    `json:"baseline_exact_name"`      // name == label, as the benchmark scores it
	BaseEquivalentName int    `json:"baseline_equivalent_name"` // name is the label or an equivalent tool
	BaseExecutable     int    `json:"baseline_executable"`      // ... and the arguments are valid for that tool

	ImproveDevBase  int                  `json:"improve_dev_baseline"`
	ImproveDevBest  int                  `json:"improve_dev_best"`
	ImproveRounds   []agent.ImproveRound `json:"improve_rounds"`
	ImproveReason   string               `json:"improve_reason"`
	ImproveMistakes []agent.MistakeGroup `json:"improve_mistakes,omitempty"`
	Candidate       json.RawMessage      `json:"candidate"`
	Verdict         string               `json:"verdict"`
	VerdictReason   string               `json:"verdict_reason"`
	Fixed           int                  `json:"fixed"`
	Broken          int                  `json:"broken"`
	SealedBase      int                  `json:"sealed_baseline_correct"`
	SealedCand      int                  `json:"sealed_candidate_correct"`
	Unstable        int                  `json:"unstable"`
	HarmfulVerdict  string               `json:"harmful_verdict"`
	HarmfulBroken   int                  `json:"harmful_broken"`
	HarmfulExpected int                  `json:"harmful_expected"` // baseline-correct tasks the route moves to an unacceptable tool
	EmptyVerdict    string               `json:"empty_verdict"`
	// Docs is the documentation the simulated server ships: "full" (the dataset's descriptions) or "none".
	Docs          string `json:"docs"`
	Model         string `json:"model"`
	ProposerModel string `json:"proposer_model"`
	// Restore is the positive control in -docs none: an overlay putting the real descriptions back.
	// It is a known improvement, so the gate should PASS it whenever missing docs cost the agent at
	// least a few sealed tasks; without it, a gate that never says pass is indistinguishable from a
	// gate that cannot.
	RestoreVerdict string         `json:"restore_verdict,omitempty"`
	RestoreFixed   int            `json:"restore_fixed,omitempty"`
	RestoreBroken  int            `json:"restore_broken,omitempty"`
	RestoreCorrect int            `json:"restore_correct,omitempty"`
	ExecBase       int            `json:"executed_ok_baseline,omitempty"` // FS only: correct AND the real call succeeded
	ExecCand       int            `json:"executed_ok_candidate,omitempty"`
	ExecNote       string         `json:"executed_note,omitempty"`
	ExecFailures   map[string]int `json:"executed_failures,omitempty"` // baseline: why correct picks failed when run
}

// runGateBench runs the product path on any MCPToolBench++ domain, with no upstream server needed
// for selection (the dataset carries each domain's real tool list and labels):
//
//	golden set with the dataset's equivalent tools  ->  improver on DEV (evidence-driven, refines)
//	->  selection gate on SEALED (arguments validated, decisive re-votes, retention check)
//
// Two controls per domain check the gate itself: a harmful route must FAIL, an empty overlay must be
// INCONCLUSIVE. With docs "none" the server is simulated as shipping no descriptions (names and
// schemas only, as many real servers do); everything runs against that, and a third control restores
// the real descriptions and must PASS when the gap is large enough to measure. With execUpstream set (File System), sealed calls are also executed for real through
// the proxy runtime, so "correct" can be checked against "it actually ran".
// benchOpts configures one domain run.
type benchOpts struct {
	Dataset       string
	Docs          string // "full" or "none"
	Model         string // the agent under test (selector)
	ProposerModel string // the improver and retention judge; empty = Model
	Seed          int64
	TrainRatio    float64
	Limit         int  // max tasks after shuffling (0 = all)
	Rounds        int  // improver rounds
	Concurrency   int  // parallel model calls (default 8; use 1-2 for providers with tight per-minute limits)
	BaselineOnly  bool // measure the sealed baseline and headroom, then stop (a cheap probe before a full run)
	SkipControls  bool // skip the harmful and empty controls (to save calls on a tight quota; restore still runs)
	Exec          *config.UpstreamConfig
}

func runGateBench(ctx context.Context, client *agent.Client, o benchOpts) (benchResult, error) {
	model, datasetPath, docs, seed, trainRatio, limit, rounds, execUpstream := o.Model, o.Dataset, o.Docs, o.Seed, o.TrainRatio, o.Limit, o.Rounds, o.Exec
	if o.ProposerModel == "" {
		o.ProposerModel = o.Model
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 8
	}
	if docs != "full" && docs != "none" {
		return benchResult{}, fmt.Errorf("docs must be full or none, got %q", docs)
	}
	tasks, err := loadTasks(datasetPath)
	if err != nil {
		return benchResult{}, err
	}
	var labeled []task
	for _, t := range tasks {
		if len(t.Label) > 0 && len(t.Tools) > 0 {
			labeled = append(labeled, t)
		}
	}
	tasks = labeled
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(tasks), func(i, j int) { tasks[i], tasks[j] = tasks[j], tasks[i] })
	if limit > 0 && limit < len(tasks) {
		tasks = tasks[:limit]
	}
	nTrain := int(float64(len(tasks)) * trainRatio)
	train, holdout := tasks[:nTrain], tasks[nTrain:]

	toolNames := map[string]bool{}
	for _, t := range tasks[0].Tools {
		toolNames[t.Name] = true
	}
	mk := func(t task, split selectiongate.Split) selectiongate.Task {
		gt := selectiongate.Task{ID: t.UUID, Query: t.Query, ExpectedTool: t.Label[0].Name, Split: split}
		for _, s := range t.Label[0].SimilarTools {
			if s.Name != "" && s.Name != gt.ExpectedTool && toolNames[s.Name] {
				gt.AcceptableTools = append(gt.AcceptableTools, s.Name)
			}
		}
		return gt
	}
	g := selectiongate.GoldenSet{Name: "bench"}
	sealedQ := map[string]bool{}
	for _, t := range holdout {
		g.Tasks = append(g.Tasks, mk(t, selectiongate.SplitSealed))
		sealedQ[selectiongate.NormalizeQuery(t.Query)] = true
	}
	for _, t := range train {
		if !sealedQ[selectiongate.NormalizeQuery(t.Query)] {
			g.Tasks = append(g.Tasks, mk(t, selectiongate.SplitDev))
		}
	}
	if err := g.Validate(); err != nil {
		return benchResult{}, err
	}
	dev, sealed := g.Dev(), g.Sealed()

	live := make([]mcp.ToolInfo, len(tasks[0].Tools))
	realDocs := map[string]string{}
	for i, t := range tasks[0].Tools {
		live[i] = mcp.ToolInfo{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema}
		realDocs[t.Name] = t.Description
	}
	if docs == "none" {
		for i := range live {
			live[i].Description = ""
		}
	}
	toGate := func(ov improvementsandbox.Overlay) []selectiongate.Tool {
		vis := mcp.VisibleTools(live, ov)
		out := make([]selectiongate.Tool, len(vis))
		for i, t := range vis {
			out[i] = selectiongate.Tool{Name: t.Name, Description: t.Description, Parameters: t.InputSchema}
		}
		return out
	}

	zero := 0.0
	client.Temperature = &zero
	sel := agent.OpenAISelector{Client: client, Model: model}
	opt := selectiongate.EvalOptions{Concurrency: o.Concurrency}
	res := benchResult{Domain: filepath.Base(filepath.Dir(datasetPath)), Tools: len(live), Dev: len(dev), Sealed: len(sealed), Docs: docs, Model: model, ProposerModel: o.ProposerModel}
	for _, t := range sealed {
		if len(t.AcceptableTools) > 0 {
			res.WithEquivalents++
		}
	}
	empty := improvementsandbox.Overlay{}

	// ---- baseline on sealed, scored three ways
	baseRun, err := selectiongate.Evaluate(ctx, sel, toGate(empty), sealed, empty, opt)
	if err != nil {
		return res, err
	}
	for i, o := range baseRun.Outcomes {
		if o.Served == sealed[i].ExpectedTool && !o.Denied {
			res.BaseExactName++
		}
		if sealed[i].Accepts(o.Served) && !o.Denied {
			res.BaseEquivalentName++
		}
		if o.Correct {
			res.BaseExecutable++
		}
	}
	fmt.Printf("[%s] docs=%s tools=%d dev=%d sealed=%d (with equivalents %d)  baseline on sealed: exact-name %d, equivalent-name %d, executable %d\n",
		res.Domain, res.Docs, res.Tools, res.Dev, res.Sealed, res.WithEquivalents, res.BaseExactName, res.BaseEquivalentName, res.BaseExecutable)
	if head, need := res.Sealed-res.BaseExecutable, selectiongate.MinFixedToPass(0.05); head < need {
		fmt.Printf("[%s] headroom: the baseline misses %d sealed task(s); a pass needs at least %d fixed, so no candidate can pass here\n", res.Domain, head, need)
	} else {
		fmt.Printf("[%s] headroom: the baseline misses %d sealed task(s); a pass needs at least %d fixed\n", res.Domain, head, need)
	}

	if o.BaselineOnly {
		return res, nil
	}

	// ---- the improver learns on dev only
	imp, err := agent.ImproveDescriptions(ctx, client, sel, live, empty, dev, agent.ImproveOptions{Rounds: rounds, Concurrency: o.Concurrency, Model: o.ProposerModel})
	if err != nil {
		return res, err
	}
	res.ImproveDevBase, res.ImproveDevBest, res.ImproveRounds, res.ImproveReason = imp.BaselineCorrect, imp.BestCorrect, imp.Rounds, imp.Reason
	res.ImproveMistakes = imp.Mistakes
	for i, m := range imp.Mistakes {
		if i == 6 {
			fmt.Printf("[%s] dev mistake ... and %d more patterns\n", res.Domain, len(imp.Mistakes)-i)
			break
		}
		ex := ""
		if len(m.Examples) > 0 {
			ex = m.Examples[0]
			if len(ex) > 90 {
				ex = ex[:90] + "..."
			}
		}
		fmt.Printf("[%s] dev mistake x%d: %s   e.g. %q\n", res.Domain, m.Count, m.Pattern, ex)
	}
	for _, r := range imp.Rounds {
		note := r.Err
		if note == "" {
			note = fmt.Sprintf("%d/%d (fixed %d, broke %d), patches: %s", r.DevCorrect, imp.DevN, r.Fixed, r.Broken, patchNames(r.Patches))
			if len(r.Pruned) > 0 {
				note += "  [pruned; full proposal was " + r.Unpruned + "]"
			}
		}
		fmt.Printf("[%s] improve round %d: %s\n", res.Domain, r.Round, note)
	}
	fmt.Printf("[%s] improver (dev %d -> %d of %d): %s\n", res.Domain, imp.BaselineCorrect, imp.BestCorrect, imp.DevN, imp.Reason)
	candidate := imp.Best
	res.Candidate, _ = json.Marshal(candidate)

	schemas := map[string]any{}
	for _, t := range live {
		schemas[t.Name] = t.InputSchema
	}
	// ---- judge on sealed, against the one baseline run
	judge := func(name string, ov improvementsandbox.Overlay) (selectiongate.Decision, selectiongate.Run, error) {
		if problems := improvementsandbox.CheckRoutes(ov, schemas); len(problems) > 0 {
			d := selectiongate.Decision{Verdict: selectiongate.VerdictFail, Reason: "rejected without measuring: " + problems[0].String()}
			fmt.Printf("[%s] %-9s -> fail (%s)\n", res.Domain, name, d.Reason)
			return d, selectiongate.Run{}, nil
		}
		cand, err := selectiongate.Evaluate(ctx, sel, toGate(ov), sealed, ov, opt)
		if err != nil {
			return selectiongate.Decision{}, cand, err
		}
		base := selectiongate.Run{Outcomes: append([]selectiongate.Outcome(nil), baseRun.Outcomes...)}
		if _, err := selectiongate.ConfirmDisputed(ctx, sel, sealed,
			selectiongate.Arm{Tools: toGate(empty), Overlay: empty},
			selectiongate.Arm{Tools: toGate(ov), Overlay: ov}, &base, &cand, selectiongate.DefaultConfirmVotes, opt); err != nil {
			return selectiongate.Decision{}, cand, err
		}
		d, err := selectiongate.Decide(base, cand, g.SealedHash(), selectiongate.GateConfig{})
		if err != nil {
			return d, cand, err
		}
		if d.Verdict == selectiongate.VerdictPass {
			var pairs []selectiongate.DescPair
			old := map[string]string{}
			for _, t := range mcp.VisibleTools(live, empty) {
				old[t.Name] = t.Description
			}
			for _, t := range mcp.VisibleTools(live, ov) {
				if o, ok := old[t.Name]; ok && o != t.Description {
					pairs = append(pairs, selectiongate.DescPair{Tool: t.Name, Old: o, New: t.Description})
				}
			}
			judgeFn := func(ctx context.Context, system, user string) (string, error) {
				return client.CompleteJSON(ctx, o.ProposerModel, system, user)
			}
			losses, rerr := selectiongate.CheckRetention(ctx, judgeFn, pairs, 3)
			d = selectiongate.ApplyRetention(d, losses, rerr, false)
		}
		fmt.Printf("[%s] %-9s -> %-12s %d -> %d, fixed %d, broken %d, unstable %d  (%s)\n", res.Domain, name, d.Verdict,
			d.BaselineCorrect, d.CandidateCorrect, d.Fixed, d.Broken, d.UnstableTasks, d.Reason)
		return d, cand, nil
	}

	var candRun selectiongate.Run
	if len(candidate.ToolPatches) == 0 {
		res.Verdict, res.VerdictReason = "none", "the improver found nothing worth judging: "+imp.Reason
		fmt.Printf("[%s] candidate -> none (%s)\n", res.Domain, res.VerdictReason)
	} else {
		d, run, err := judge("candidate", candidate)
		if err != nil {
			return res, err
		}
		candRun = run
		res.Verdict, res.VerdictReason, res.Fixed, res.Broken = string(d.Verdict), d.Reason, d.Fixed, d.Broken
		res.SealedBase, res.SealedCand, res.Unstable = d.BaselineCorrect, d.CandidateCorrect, d.UnstableTasks
	}

	if !o.SkipControls {
		if from, to, expect := harmfulRoute(baseRun, sealed, live); from != "" {
			fmt.Printf("[%s] harmful control: route %s -> %s should break %d sealed tasks\n", res.Domain, from, to, expect)
			hd, _, err := judge("harmful", improvementsandbox.Overlay{Routing: []improvementsandbox.RouteRule{{FromTool: from, ToTool: to}}})
			if err != nil {
				return res, err
			}
			res.HarmfulVerdict, res.HarmfulBroken, res.HarmfulExpected = string(hd.Verdict), hd.Broken, expect
		}
		ed, _, err := judge("empty", empty)
		if err != nil {
			return res, err
		}
		res.EmptyVerdict = string(ed.Verdict)
	}
	if docs == "none" {
		rd, _, err := judge("restore", restoreOverlay(realDocs))
		if err != nil {
			return res, err
		}
		res.RestoreVerdict, res.RestoreFixed, res.RestoreBroken, res.RestoreCorrect = string(rd.Verdict), rd.Fixed, rd.Broken, rd.CandidateCorrect
	}

	// ---- real execution (File System): run every sealed call through the actual proxy runtime
	if execUpstream != nil {
		res.ExecFailures = map[string]int{}
		okBase, err := executeSealed(ctx, *execUpstream, empty, baseRun, res.ExecFailures)
		if err != nil {
			res.ExecNote = "execution failed: " + err.Error()
		} else {
			res.ExecBase = okBase
			if len(candRun.Outcomes) > 0 {
				if res.ExecCand, err = executeSealed(ctx, *execUpstream, candidate, candRun, nil); err != nil {
					res.ExecNote = "candidate execution failed: " + err.Error()
				}
			}
			fmt.Printf("[%s] executed for real (correct AND the call succeeded): baseline %d, candidate %d, of %d\n",
				res.Domain, res.ExecBase, res.ExecCand, len(sealed))
			for why, n := range res.ExecFailures {
				fmt.Printf("[%s]   a correct baseline pick failed when run, x%d: %s\n", res.Domain, n, why)
			}
		}
	}
	return res, nil
}

// executeSealed replays each task's call (the tool and arguments the selector chose) through a real
// proxy over the real upstream with the overlay applied, exactly as a client would, and counts the
// calls that were judged correct AND came back without an error.
func executeSealed(ctx context.Context, up config.UpstreamConfig, ov improvementsandbox.Overlay, run selectiongate.Run, failures map[string]int) (int, error) {
	// Real calls write, move and delete files. Each run gets its own fresh copy of the fixture, so
	// both arms start from identical state and the original workspace is never changed.
	if up.Dir != "" {
		fresh, err := os.MkdirTemp("", "bench-fixture-*")
		if err != nil {
			return 0, err
		}
		defer os.RemoveAll(fresh)
		if err := copyDir(up.Dir, fresh); err != nil {
			return 0, fmt.Errorf("copy fixture: %w", err)
		}
		args := make([]string, len(up.Args))
		for i, a := range up.Args {
			args[i] = strings.ReplaceAll(a, up.Dir, fresh)
		}
		up.Args, up.Dir = args, fresh
	}
	upstream, err := mcp.ConnectStdio(ctx, up)
	if err != nil {
		return 0, err
	}
	defer upstream.Close()
	p := mcp.NewProxy(upstream)
	p.Overlay = ov
	if err := p.RegisterUpstreamTools(ctx); err != nil {
		return 0, err
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ct, st := mcpsdk.NewInMemoryTransports()
	if _, err := p.Server.SDK().Connect(cctx, st, nil); err != nil {
		return 0, err
	}
	sess, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "bench-exec", Version: "1"}, nil).Connect(cctx, ct, nil)
	if err != nil {
		return 0, err
	}
	ok := 0
	for _, o := range run.Outcomes {
		if !o.Correct || o.Picked == "" {
			continue
		}
		res, err := sess.CallTool(cctx, &mcpsdk.CallToolParams{Name: o.Picked, Arguments: o.Args})
		if err == nil && res != nil && !res.IsError {
			ok++
			continue
		}
		if failures != nil {
			why := "unknown"
			if err != nil {
				why = err.Error()
			} else if res != nil && len(res.Content) > 0 {
				if tc, isText := res.Content[0].(*mcpsdk.TextContent); isText {
					why = tc.Text
				}
			}
			if i := strings.IndexAny(why, "'\""); i > 0 {
				why = why[:i] // keep the kind of error, drop the path
			}
			if len(why) > 70 {
				why = why[:70]
			}
			failures[o.Picked+": "+strings.TrimSpace(why)]++
		}
	}
	return ok, nil
}

// restoreOverlay patches every tool back to its real description.
func restoreOverlay(realDocs map[string]string) improvementsandbox.Overlay {
	ov := improvementsandbox.Overlay{ToolPatches: map[string]improvementsandbox.ToolPatch{}}
	for name, d := range realDocs {
		if strings.TrimSpace(d) == "" {
			continue
		}
		d := d
		ov.ToolPatches[name] = improvementsandbox.ToolPatch{Description: &d}
	}
	return ov
}

func patchNames(o improvementsandbox.Overlay) string {
	var names []string
	for n, p := range o.ToolPatches {
		if p.Hidden {
			names = append(names, n+" (hidden)")
		} else {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

// harmfulRoute picks the route that should break the most sealed tasks the baseline gets right: from a
// tool the agent uses correctly to a tool that is NOT acceptable for those tasks, with compatible
// arguments so the control is judged on selection rather than rejected up front. expect is how many
// baseline-correct tasks the route moves to an unacceptable tool, so a healthy gate must report broken
// tasks. Targets are scored against each task's acceptable tools: routing to an equivalent twin (the
// same action on a sibling server) is not harmful, and once equivalents count as correct the old
// "route the most-used tool anywhere compatible" choice picked exactly such a twin and broke nothing.
func harmfulRoute(base selectiongate.Run, sealed []selectiongate.Task, tools []mcp.ToolInfo) (from, to string, expect int) {
	schemas := map[string]any{}
	for _, t := range tools {
		schemas[t.Name] = t.InputSchema
	}
	for _, f := range tools {
		for _, t := range tools {
			if t.Name == f.Name {
				continue
			}
			ov := improvementsandbox.Overlay{Routing: []improvementsandbox.RouteRule{{FromTool: f.Name, ToTool: t.Name}}}
			if len(improvementsandbox.CheckRoutes(ov, schemas)) > 0 {
				continue
			}
			n := 0
			for i, o := range base.Outcomes {
				if o.Correct && o.Served == f.Name && !sealed[i].Accepts(t.Name) {
					n++
				}
			}
			// most breaks wins; ties go to the alphabetically first pair, so the choice is reproducible
			if n > expect || (n == expect && n > 0 && (f.Name < from || (f.Name == from && t.Name < to))) {
				from, to, expect = f.Name, t.Name, n
			}
		}
	}
	return from, to, expect
}

// copyDir copies a directory tree of regular files.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	})
}
