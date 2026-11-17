package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"self-improving-mcp/agent"
	"self-improving-mcp/config"
	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/mcp"
	selectiongate "self-improving-mcp/selection-gate"
	"self-improving-mcp/store"
)

type task struct {
	UUID  string      `json:"uuid"`
	Query string      `json:"query"`
	Tools []taskTool  `json:"tools"`
	Label []taskLabel `json:"function_call_label"`
}

type taskTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type taskLabel struct {
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
	// SimilarTools are tools the dataset itself lists as doing the same job (often the same action
	// on another server). They are acceptable answers, not mistakes.
	SimilarTools []struct {
		Name string `json:"name"`
	} `json:"similar_tools"`
}

type runStats struct {
	N         int
	Correct   int
	Calls     int
	Errors    int
	LatencyMs int64
}

func main() {
	_ = config.LoadDotEnv(".env")
	_ = config.LoadDotEnv("../.env")

	dataset := flag.String("dataset", filepath.Join("..", "evaluation", "MCPToolBenchPP", "data", "file_system", "filesystem_0723_single.json"), "filesystem dataset json")
	workspace := flag.String("workspace", filepath.Join("..", "evaluation", "workspace"), "dir passed to filesystem MCP")
	dataDir := flag.String("data-dir", "data/proof-fs", "sqlite data dir for this proof run")
	limit := flag.Int("limit", 0, "max tasks (0 = all)")
	trainRatio := flag.Float64("train-ratio", 0.7, "train fraction for observe/evolve")
	sandboxRatio := flag.Float64("sandbox-ratio", 0.25, "fraction of train held out for sandbox gate")
	seed := flag.Int64("seed", 42, "shuffle seed")
	model := flag.String("model", "gpt-4.1-mini", "openai model for tool selection")
	skipEvolve := flag.Bool("skip-evolve", false, "skip observe/evolve, only baseline holdout")
	rounds := flag.Int("rounds", 3, "gate mode: improver rounds on the dev split")
	execute := flag.Bool("execute", false, "gate mode, File System only: also execute every sealed call for real through the proxy runtime")
	docs := flag.String("docs", "full", "gate mode: documentation the simulated server ships: full (the dataset's descriptions) or none (names and schemas only; adds a restore-the-docs positive control)")
	mode := flag.String("mode", "proof", "proof: the original File System proof; gate: run the current product path (gate, confirm votes, retention) on any domain dataset, no server needed")
	exportGolden := flag.String("export-golden", "", "write a golden set json (train=dev, holdout=sealed) to this path and exit")
	flag.Parse()

	ctx := context.Background()
	if *mode == "gate" {
		client, err := agent.NewClient("gpt-4.1-nano", *model)
		if err != nil {
			log.Fatal(err)
		}
		var execUp *config.UpstreamConfig
		if *execute {
			absWS, err := filepath.Abs(*workspace)
			if err != nil {
				log.Fatal(err)
			}
			// the dataset's paths are relative ("./test_project_root/..."), so run the server in the workspace
			execUp = &config.UpstreamConfig{Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", absWS}, Dir: absWS}
		}
		res, err := runGateBench(ctx, client, *model, *dataset, *docs, *seed, *trainRatio, *limit, *rounds, execUp)
		if err != nil {
			log.Fatal(err)
		}
		raw, _ := json.Marshal(res)
		fmt.Printf("BENCH_RESULT %s\n", raw)
		return
	}
	tasks, err := loadTasks(*dataset)
	if err != nil {
		log.Fatal(err)
	}
	rng := rand.New(rand.NewSource(*seed))
	rng.Shuffle(len(tasks), func(i, j int) { tasks[i], tasks[j] = tasks[j], tasks[i] })
	if *limit > 0 && *limit < len(tasks) {
		tasks = tasks[:*limit]
	}
	nTrain := int(float64(len(tasks)) * (*trainRatio))
	if nTrain < 1 && len(tasks) > 1 {
		nTrain = len(tasks) / 2
	}
	if nTrain >= len(tasks) {
		nTrain = len(tasks) - 1
	}
	train, holdout := tasks[:nTrain], tasks[nTrain:]

	if *exportGolden != "" {
		if err := writeGolden(*exportGolden, *seed, train, holdout); err != nil {
			log.Fatal(err)
		}
		return
	}

	nSandbox := int(float64(len(train)) * (*sandboxRatio))
	if nSandbox < 1 && len(train) > 4 {
		nSandbox = len(train) / 4
	}
	if nSandbox >= len(train) {
		nSandbox = len(train) / 2
	}
	if nSandbox < 1 {
		nSandbox = 0
	}
	observe, sandboxTasks := train, []task(nil)
	if nSandbox > 0 {
		observe, sandboxTasks = train[:len(train)-nSandbox], train[len(train)-nSandbox:]
	}

	fmt.Printf("dataset tasks loaded=%d using=%d observe=%d sandbox=%d holdout=%d\n",
		countFile(*dataset), len(tasks), len(observe), len(sandboxTasks), len(holdout))

	absWS, err := filepath.Abs(*workspace)
	if err != nil {
		log.Fatal(err)
	}
	upstreamCfg := config.UpstreamConfig{
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", absWS},
	}

	db, err := store.Open(filepath.Join(*dataDir, "trajectories.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		log.Fatal(err)
	}

	client, err := agent.NewClient("gpt-4.1-nano", *model)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("== baseline holdout ==")
	base, err := evalSplit(ctx, client, upstreamCfg, db, "baseline", holdout, improvementsandbox.Overlay{}, *model, true)
	if err != nil {
		log.Fatal(err)
	}
	printStats("baseline_holdout", base)

	var overlay improvementsandbox.Overlay
	gatePassed := false
	gateReason := "skipped"
	if !*skipEvolve {
		fmt.Println("== observe train (record trajectories) ==")
		trainStats, err := evalSplit(ctx, client, upstreamCfg, db, "train-observe", observe, improvementsandbox.Overlay{}, *model, true)
		if err != nil {
			log.Fatal(err)
		}
		printStats("train_observe", trainStats)

		fmt.Println("== evolve evaluate + propose ==")
		findings, err := agent.EvaluateRules(db)
		if err != nil {
			log.Fatal(err)
		}
		confusions, allowedRoutes, err := injectConfusionFindings(db)
		if err != nil {
			log.Fatal(err)
		}
		findings = append(findings, confusions...)
		fmt.Printf("rule findings: %d (incl %d tool-confusion); allowed routes=%v\n", len(findings), len(confusions), allowedRoutes)

		versions, err := agent.ProposeOverlays(ctx, client, db, findings)
		if err != nil {
			log.Printf("propose failed (continuing with empty overlay): %v", err)
		} else if len(versions) > 0 {
			rawOverlay, err := improvementsandbox.ParseOverlay(versions[0].OverlayJSON)
			if err != nil {
				log.Fatal(err)
			}
			fmt.Println("raw proposed overlay:")
			printOverlay(rawOverlay)

			overlay = filterOverlay(rawOverlay, allowedRoutes)
			fmt.Println("filtered overlay (confusion-safe routing only):")
			printOverlay(overlay)

			fmt.Println("== sandbox gate (validate slice) ==")
			if len(sandboxTasks) == 0 {
				gatePassed = !overlayEmpty(overlay)
				gateReason = "no sandbox slice; accept filtered overlay if non-empty"
			} else {
				sbBase, err := evalSplit(ctx, client, upstreamCfg, db, "sandbox-base", sandboxTasks, improvementsandbox.Overlay{}, *model, false)
				if err != nil {
					log.Fatal(err)
				}
				sbCand, err := evalSplit(ctx, client, upstreamCfg, db, "sandbox-cand", sandboxTasks, overlay, *model, false)
				if err != nil {
					log.Fatal(err)
				}
				printStats("sandbox_baseline", sbBase)
				printStats("sandbox_candidate", sbCand)
				gatePassed, gateReason = passGate(sbBase, sbCand)

				exp, err := db.InsertExperiment(versions[0].ID, "")
				if err != nil {
					log.Fatal(err)
				}
				status := "failed"
				if gatePassed {
					status = "passed"
				}
				sc := map[string]any{
					"baseline_pass":  rate(sbBase.Correct, sbBase.N),
					"candidate_pass": rate(sbCand.Correct, sbCand.N),
					"promote":        gatePassed,
					"reason":         gateReason,
				}
				raw, _ := json.Marshal(sc)
				_ = db.UpdateExperimentResult(exp.ID, status, string(raw))
			}

			fmt.Printf("sandbox gate: passed=%v reason=%s\n", gatePassed, gateReason)
			if gatePassed {
				filteredJSON, _ := json.Marshal(overlay)
				if _, err := db.SQL().Exec(`UPDATE versions SET overlay_json = ?, status = 'active', notes = ? WHERE id = ?`,
					string(filteredJSON), "sandbox-gated filtered overlay", versions[0].ID); err != nil {
					log.Fatal(err)
				}
				if err := db.SetActiveVersion(versions[0].ID); err != nil {
					log.Fatal(err)
				}
				fmt.Printf("promoted %s\n", versions[0].ID)
			} else {
				_, _ = db.SQL().Exec(`UPDATE versions SET status = 'discarded', notes = ? WHERE id = ?`,
					"sandbox gate rejected: "+gateReason, versions[0].ID)
				overlay = improvementsandbox.Overlay{}
				fmt.Println("discarded candidate; measuring holdout as baseline (empty overlay)")
			}
		}
	}

	fmt.Println("== evolved holdout ==")
	evolved, err := evalSplit(ctx, client, upstreamCfg, db, "evolved", holdout, overlay, *model, true)
	if err != nil {
		log.Fatal(err)
	}
	printStats("evolved_holdout", evolved)

	fmt.Println("== PROOF SUMMARY ==")
	fmt.Printf("sandbox_gate  passed=%v  reason=%s\n", gatePassed, gateReason)
	fmt.Printf("holdout_pass@1  baseline=%.3f  evolved=%.3f  delta=%+.3f\n",
		rate(base.Correct, base.N), rate(evolved.Correct, evolved.N), rate(evolved.Correct, evolved.N)-rate(base.Correct, base.N))
	fmt.Printf("holdout_calls_per_task  baseline=%.2f  evolved=%.2f\n",
		per(base.Calls, base.N), per(evolved.Calls, evolved.N))
	fmt.Printf("holdout_error_rate  baseline=%.3f  evolved=%.3f\n",
		rate(base.Errors, base.Calls), rate(evolved.Errors, evolved.Calls))
	if rate(evolved.Correct, evolved.N) > rate(base.Correct, base.N) {
		fmt.Println("RESULT: evolved overlay beat baseline on holdout — observation + sandbox gate helped.")
	} else if rate(evolved.Correct, evolved.N) == rate(base.Correct, base.N) {
		fmt.Println("RESULT: no Pass@1 change on holdout (safe no-op if gate discarded a bad overlay).")
	} else {
		fmt.Println("RESULT: evolved did not beat baseline on this split.")
	}
}

func passGate(base, cand runStats) (bool, string) {
	br := rate(base.Correct, base.N)
	cr := rate(cand.Correct, cand.N)
	if cr+1e-9 < br {
		return false, fmt.Sprintf("pass@1 regression (%.3f < %.3f)", cr, br)
	}
	if cr > br+1e-9 {
		return true, fmt.Sprintf("pass@1 improved (%.3f > %.3f)", cr, br)
	}
	return false, fmt.Sprintf("no pass@1 improvement (%.3f == %.3f)", cr, br)
}

func filterOverlay(in improvementsandbox.Overlay, allowed map[string]string) improvementsandbox.Overlay {
	out := improvementsandbox.Overlay{
		ToolPatches:   in.ToolPatches,
		Cache:         in.Cache,
		ComposedTools: nil, // skip composed macros for FS proof stability
	}
	for _, r := range in.Routing {
		if r.Deny || r.FromTool == "" || r.ToTool == "" {
			continue
		}
		if want, ok := allowed[r.FromTool]; ok && want == r.ToTool {
			out.Routing = append(out.Routing, r)
		}
	}
	// If model omitted routing but we have allowed confusion routes, add them.
	if len(out.Routing) == 0 {
		for from, to := range allowed {
			out.Routing = append(out.Routing, improvementsandbox.RouteRule{FromTool: from, ToTool: to, Note: "from observed confusion"})
		}
	}
	// Keep only patches for tools involved in confusion or existing patches for those tools.
	if len(allowed) > 0 && len(out.ToolPatches) > 0 {
		filtered := map[string]improvementsandbox.ToolPatch{}
		for name, patch := range out.ToolPatches {
			if _, ok := allowed[name]; ok {
				filtered[name] = patch
				continue
			}
			for _, to := range allowed {
				if name == to {
					filtered[name] = patch
					break
				}
			}
		}
		out.ToolPatches = filtered
	}
	return out
}

func overlayEmpty(o improvementsandbox.Overlay) bool {
	return len(o.ToolPatches) == 0 && len(o.Routing) == 0 && len(o.Cache) == 0 && len(o.ComposedTools) == 0
}

func printOverlay(o improvementsandbox.Overlay) {
	raw, _ := json.MarshalIndent(o, "", "  ")
	fmt.Println(string(raw))
}

func evalSplit(ctx context.Context, client *agent.Client, upstreamCfg config.UpstreamConfig, db *store.DB, sessionLabel string, tasks []task, overlay improvementsandbox.Overlay, model string, execute bool) (runStats, error) {
	var st runStats
	st.N = len(tasks)
	if len(tasks) == 0 {
		return st, nil
	}

	up, err := mcp.ConnectStdio(ctx, upstreamCfg)
	if err != nil {
		return st, fmt.Errorf("connect filesystem mcp: %w (is node/npx installed?)", err)
	}
	defer up.Close()

	liveTools, err := up.ListTools(ctx)
	if err != nil {
		return st, err
	}
	liveTools = mcp.ApplyOverlayToTools(liveTools, overlay)
	toolSpecs := toSpecs(liveTools)
	if len(toolSpecs) == 0 && len(tasks) > 0 {
		toolSpecs = datasetSpecs(tasks[0].Tools)
	}

	session, err := tracerSession(db, sessionLabel)
	if err != nil {
		return st, err
	}

	system := `You are an MCP filesystem agent. Pick exactly one best tool for the user request.
Prefer the most specific tool. Paths are under test_project_root/. Always call a tool.`

	for i, t := range tasks {
		want := ""
		if len(t.Label) > 0 {
			want = t.Label[0].Name
		}
		start := time.Now()
		call, _, err := client.CompleteWithTools(ctx, model, system, t.Query, toolSpecs)
		latency := time.Since(start).Milliseconds()
		st.LatencyMs += latency
		st.Calls++

		got := call.Name
		routed := got
		denied := false
		for _, r := range overlay.Routing {
			if r.FromTool != got {
				continue
			}
			if r.Deny {
				denied = true
			}
			if r.ToTool != "" {
				routed = r.ToTool
			}
			break
		}
		ok := routed != "" && routed == want
		if ok {
			st.Correct++
		}
		isErr := err != nil || got == "" || denied
		if isErr {
			st.Errors++
		}

		args := map[string]any{}
		_ = json.Unmarshal([]byte(call.Arguments), &args)
		if execute && routed != "" && err == nil && !denied {
			if _, cerr := up.CallTool(ctx, routed, args); cerr != nil {
				isErr = true
				st.Errors++
			}
		}

		argsJSON, _ := json.Marshal(args)
		_, _ = db.InsertToolCall(store.ToolCall{
			SessionID:     session,
			Seq:           i + 1,
			ToolName:      got,
			ArgsJSON:      string(argsJSON),
			ArgsHash:      want,
			ResultSummary: fmt.Sprintf("want=%s got=%s ok=%v", want, got, ok),
			IsError:       isErr,
			LatencyMs:     latency,
		})

		fmt.Printf("[%s %d/%d] want=%s got=%s routed=%s ok=%v\n", sessionLabel, i+1, len(tasks), want, got, routed, ok)
	}
	return st, nil
}

func tracerSession(db *store.DB, label string) (string, error) {
	s, err := db.CreateSession(label)
	if err != nil {
		return "", err
	}
	return s.ID, nil
}

func toSpecs(tools []mcp.ToolInfo) []agent.ToolSpec {
	out := make([]agent.ToolSpec, 0, len(tools))
	for _, t := range tools {
		out = append(out, agent.ToolSpec{Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
	}
	return out
}

func datasetSpecs(tools []taskTool) []agent.ToolSpec {
	out := make([]agent.ToolSpec, 0, len(tools))
	for _, t := range tools {
		out = append(out, agent.ToolSpec{Name: t.Name, Description: t.Description, Parameters: t.InputSchema})
	}
	return out
}

// writeGolden exports the benchmark split as a golden set: train tasks are dev (the improver may
// see them), holdout tasks are sealed (only the gate may use them). The set is validated, which
// also rejects any query that appears on both sides.
func writeGolden(path string, seed int64, train, holdout []task) error {
	g := selectiongate.GoldenSet{Name: fmt.Sprintf("mcptoolbench-fs-seed%d", seed)}
	add := func(ts []task, split selectiongate.Split) {
		for _, t := range ts {
			if len(t.Label) == 0 {
				continue
			}
			gt := selectiongate.Task{ID: t.UUID, Query: t.Query, ExpectedTool: t.Label[0].Name, Split: split}
			for _, s := range t.Label[0].SimilarTools {
				if s.Name != "" && s.Name != gt.ExpectedTool {
					gt.AcceptableTools = append(gt.AcceptableTools, s.Name)
				}
			}
			g.Tasks = append(g.Tasks, gt)
		}
	}
	// Sealed first, then dev minus anything whose query also appears in sealed (the benchmark has a
	// few repeated queries). Dropping from dev keeps the sealed set identical to the proof's holdout.
	add(holdout, selectiongate.SplitSealed)
	sealedQueries := map[string]bool{}
	for _, t := range g.Tasks {
		sealedQueries[selectiongate.NormalizeQuery(t.Query)] = true
	}
	var devTasks []task
	dropped := 0
	for _, t := range train {
		if sealedQueries[selectiongate.NormalizeQuery(t.Query)] {
			dropped++
			continue
		}
		devTasks = append(devTasks, t)
	}
	add(devTasks, selectiongate.SplitDev)
	if dropped > 0 {
		fmt.Printf("dropped %d dev task(s) whose query also appears in the sealed set\n", dropped)
	}
	if err := g.Validate(); err != nil {
		return fmt.Errorf("exported golden set is invalid: %w", err)
	}
	raw, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return err
	}
	fmt.Printf("golden set %s: dev=%d sealed=%d sealed_sha256=%s\n",
		path, len(g.Dev()), len(g.Sealed()), g.SealedHash())
	return nil
}

func loadTasks(path string) ([]task, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tasks []task
	if err := json.Unmarshal(b, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func countFile(path string) int {
	tasks, err := loadTasks(path)
	if err != nil {
		return 0
	}
	return len(tasks)
}

func printStats(name string, s runStats) {
	fmt.Printf("%s: n=%d pass@1=%.3f calls=%d errors=%d avg_latency_ms=%.0f\n",
		name, s.N, rate(s.Correct, s.N), s.Calls, s.Errors, per(int(s.LatencyMs), s.N))
}

func rate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func per(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func injectConfusionFindings(db *store.DB) ([]store.Finding, map[string]string, error) {
	rows, err := db.SQL().Query(`
		SELECT tool_name, args_hash, COUNT(*) FROM tool_calls
		WHERE result_summary LIKE 'want=%'
		GROUP BY tool_name, args_hash`)
	if err != nil {
		return nil, nil, err
	}
	type key struct{ got, want string }
	counts := map[key]int{}
	for rows.Next() {
		var got, want string
		var n int
		if err := rows.Scan(&got, &want, &n); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		counts[key{got, want}] += n
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, nil, err
	}
	_ = rows.Close()

	// Candidate confusions: got != want
	type pair struct {
		got, want    string
		wrong, right int
	}
	var cands []pair
	for k, n := range counts {
		if k.got == "" || k.want == "" || k.got == k.want {
			continue
		}
		right := counts[key{got: k.got, want: k.got}]
		cands = append(cands, pair{got: k.got, want: k.want, wrong: n, right: right})
	}

	bestWrong := map[string]int{}
	allowed := map[string]string{}
	for _, p := range cands {
		// Only route got→want when picking `got` is usually wrong.
		// Requires clear majority and enough evidence.
		if p.wrong < 3 || p.wrong <= p.right*2 {
			continue
		}
		if n, ok := bestWrong[p.got]; ok && p.wrong <= n {
			continue
		}
		bestWrong[p.got] = p.wrong
		allowed[p.got] = p.want
	}

	var out []store.Finding
	for got, want := range allowed {
		n := bestWrong[got]
		right := counts[key{got: got, want: got}]
		detail := fmt.Sprintf("Agent chose %q %d times when label expected %q (and was correct with %q only %d times). Strengthen %q; ONLY safe routing: %s → %s.", got, n, want, got, right, want, got, want)
		f, err := db.InsertFinding(store.Finding{
			Category:     "poor_tool_selection",
			Severity:     "error",
			Title:        fmt.Sprintf("confused %s → should be %s", got, want),
			Detail:       detail,
			EvidenceJSON: fmt.Sprintf(`{"got":%q,"want":%q,"wrong":%d,"right":%d}`, got, want, n, right),
		})
		if err != nil {
			return nil, nil, err
		}
		out = append(out, f)
	}
	return out, allowed, nil
}
