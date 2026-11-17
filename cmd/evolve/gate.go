package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"self-improving-mcp/agent"
	"self-improving-mcp/config"
	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/mcp"
	selectiongate "self-improving-mcp/selection-gate"
	"self-improving-mcp/store"
)

func runGate(ctx context.Context) error {
	fs := flag.NewFlagSet("gate", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	versionID := fs.String("version", "", "candidate version id")
	goldenPath := fs.String("golden", "", "golden set json (default: gate.golden_path from config)")
	proxyCfgPath := fs.String("proxy-config", envOr("PROXY_CONFIG", "config/proxy.yaml"), "proxy config for upstream")
	reportPath := fs.String("report", "", "write the full evidence report (json) to this path")
	exitCode := fs.Bool("exit-code", false, "exit 0=pass, 3=fail, 4=inconclusive (for CI)")
	branch := fs.Bool("branch", false, "on pass, record the overlay on a new local side branch (never pushes)")
	repo := fs.String("repo", ".", "git repository to create the side branch in")
	acceptLoss := fs.Bool("accept-info-loss", false, "ship a pass even if the new descriptions drop information (only after reading what is dropped)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *versionID == "" {
		return fmt.Errorf("-version is required")
	}

	cfg, err := loadEvolveConfig(*cfgPath)
	if err != nil {
		return err
	}
	if *acceptLoss {
		cfg.Gate.AcceptInfoLoss = true
	}
	golden := *goldenPath
	if golden == "" {
		golden = cfg.Gate.GoldenPath
	}
	if golden == "" {
		return fmt.Errorf("no golden set: pass -golden or set gate.golden_path in %s", *cfgPath)
	}

	db, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	rep, err := evaluateGate(ctx, db, cfg, *versionID, golden, *proxyCfgPath)
	if err != nil {
		return err
	}

	raw, _ := json.MarshalIndent(rep.Decision, "", "  ")
	fmt.Println(string(raw))
	if *reportPath != "" {
		full, _ := json.MarshalIndent(rep, "", "  ")
		if err := os.WriteFile(*reportPath, full, 0o644); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
		fmt.Printf("evidence report: %s\n", *reportPath)
	}
	if err := publishOutcome(db, cfg.DataDir, rep, publishOptions{Branch: *branch, Repo: *repo, WebhookURL: cfg.Notify.WebhookURL}); err != nil {
		return err
	}
	if *exitCode {
		switch rep.Decision.Verdict {
		case selectiongate.VerdictFail:
			os.Exit(3)
		case selectiongate.VerdictInconclusive:
			os.Exit(4)
		}
	}
	return nil
}

// evaluateGate judges a candidate version against the currently active overlay on the sealed
// golden tasks, records the result as an experiment, and returns the full evidence.
func evaluateGate(ctx context.Context, db *store.DB, cfg config.EvolveConfig, versionID, goldenPath, proxyCfgPath string) (selectiongate.Report, error) {
	golden, err := selectiongate.LoadGolden(goldenPath)
	if err != nil {
		return selectiongate.Report{}, err
	}
	sealed := golden.Sealed()

	ver, err := db.GetVersion(versionID)
	if err != nil {
		return selectiongate.Report{}, err
	}
	candOverlay, err := improvementsandbox.ParseOverlay(ver.OverlayJSON)
	if err != nil {
		return selectiongate.Report{}, fmt.Errorf("parse candidate overlay: %w", err)
	}

	activeID, err := db.ActiveVersionID()
	if err != nil {
		return selectiongate.Report{}, err
	}
	if activeID == versionID {
		return selectiongate.Report{}, fmt.Errorf("version %s is already active; nothing to compare against", versionID)
	}
	var baseOverlay improvementsandbox.Overlay
	if activeID != "" {
		active, err := db.GetVersion(activeID)
		if err != nil {
			return selectiongate.Report{}, err
		}
		if baseOverlay, err = improvementsandbox.ParseOverlay(active.OverlayJSON); err != nil {
			return selectiongate.Report{}, fmt.Errorf("parse active overlay: %w", err)
		}
	}

	proxyCfg, err := loadProxyConfig(proxyCfgPath)
	if err != nil {
		return selectiongate.Report{}, err
	}
	upstream, err := mcp.ConnectStdio(ctx, proxyCfg.Upstream)
	if err != nil {
		return selectiongate.Report{}, err
	}
	defer upstream.Close()
	live, err := upstream.ListTools(ctx)
	if err != nil {
		return selectiongate.Report{}, err
	}

	// A route renames the tool but not the arguments. A route whose target cannot take the source's
	// arguments breaks every routed call, however well it scores on tool selection, so it is
	// rejected here without spending a single model call.
	schemas := map[string]any{}
	for _, t := range live {
		schemas[t.Name] = t.InputSchema
	}
	if problems := improvementsandbox.CheckRoutes(candOverlay, schemas); len(problems) > 0 {
		return recordRejected(db, ver.ID, activeID, golden, sealed, problems[0].String())
	}

	model := cfg.Gate.Model
	if model == "" {
		model = cfg.Models.Reason
	}
	if model == "" {
		model = "gpt-4.1-mini"
	}
	client, err := agent.NewClient(cfg.Models.Label, model)
	if err != nil {
		return selectiongate.Report{}, err
	}
	zero := 0.0
	client.Temperature = &zero // same model, same input -> same answer; only the overlay may differ
	sel := agent.OpenAISelector{Client: client, Model: model}
	opt := selectiongate.EvalOptions{Concurrency: cfg.Gate.Concurrency}

	baseRun, err := selectiongate.Evaluate(ctx, sel, toGateTools(mcp.VisibleTools(live, baseOverlay)), sealed, baseOverlay, opt)
	if err != nil {
		return selectiongate.Report{}, fmt.Errorf("baseline run: %w", err)
	}
	candRun, err := selectiongate.Evaluate(ctx, sel, toGateTools(mcp.VisibleTools(live, candOverlay)), sealed, candOverlay, opt)
	if err != nil {
		return selectiongate.Report{}, fmt.Errorf("candidate run: %w", err)
	}

	votes := cfg.Gate.ConfirmVotes
	if votes == 0 {
		votes = selectiongate.DefaultConfirmVotes
	}
	disputed, err := selectiongate.ConfirmDisputed(ctx, sel, sealed,
		selectiongate.Arm{Tools: toGateTools(mcp.VisibleTools(live, baseOverlay)), Overlay: baseOverlay},
		selectiongate.Arm{Tools: toGateTools(mcp.VisibleTools(live, candOverlay)), Overlay: candOverlay},
		&baseRun, &candRun, votes, opt)
	if err != nil {
		return selectiongate.Report{}, err
	}
	if disputed > 0 {
		fmt.Printf("confirmed %d disputed task(s) with %d extra votes per arm\n", disputed, votes)
	}

	decision, err := selectiongate.Decide(baseRun, candRun, golden.SealedHash(), selectiongate.GateConfig{
		MinSealed:            cfg.Gate.MinSealed,
		Alpha:                cfg.Gate.Alpha,
		MaxBroken:            cfg.Gate.MaxBroken,
		MaxSelectorErrorRate: cfg.Gate.MaxSelectorErrorRate,
	})
	if err != nil {
		return selectiongate.Report{}, err
	}

	// A selection pass is not enough if the new descriptions drop what agents need after choosing.
	if decision.Verdict == selectiongate.VerdictPass && cfg.Gate.RetentionVotes >= 0 {
		pairs := descriptionPairs(mcp.VisibleTools(live, baseOverlay), mcp.VisibleTools(live, candOverlay))
		judge := func(ctx context.Context, system, user string) (string, error) {
			return client.CompleteJSON(ctx, model, system, user)
		}
		losses, rerr := selectiongate.CheckRetention(ctx, judge, pairs, cfg.Gate.RetentionVotes)
		decision = selectiongate.ApplyRetention(decision, losses, rerr, cfg.Gate.AcceptInfoLoss)
		if decision.Verdict != selectiongate.VerdictPass {
			fmt.Printf("retention check: %s\n", decision.Reason)
		}
	}

	exp, err := db.InsertExperiment(ver.ID, activeID)
	if err != nil {
		return selectiongate.Report{}, err
	}
	scorecard, _ := json.Marshal(decision)
	if err := db.UpdateExperimentResult(exp.ID, experimentStatus(decision.Verdict), string(scorecard)); err != nil {
		return selectiongate.Report{}, err
	}

	return selectiongate.Report{
		VersionID:         ver.ID,
		BaselineVersionID: activeID,
		Model:             model,
		GoldenName:        golden.Name,
		Decision:          decision,
		Changes:           selectiongate.BuildChanges(sealed, baseRun, candRun),
		BaselineOutcomes:  baseRun.Outcomes,
		CandidateOutcomes: candRun.Outcomes,
	}, nil
}

func experimentStatus(v selectiongate.Verdict) string {
	switch v {
	case selectiongate.VerdictPass:
		return "passed"
	case selectiongate.VerdictFail:
		return "failed"
	default:
		return "inconclusive"
	}
}

func toGateTools(in []mcp.ToolInfo) []selectiongate.Tool {
	out := make([]selectiongate.Tool, len(in))
	for i, t := range in {
		out[i] = selectiongate.Tool{Name: t.Name, Description: t.Description, Parameters: t.InputSchema}
	}
	return out
}

// descriptionPairs lists tools whose served description differs between the active overlay
// and the candidate. Tools only the candidate adds (composed tools) have no old text to lose.
func descriptionPairs(base, cand []mcp.ToolInfo) []selectiongate.DescPair {
	old := map[string]string{}
	for _, t := range base {
		old[t.Name] = t.Description
	}
	var out []selectiongate.DescPair
	for _, t := range cand {
		if o, ok := old[t.Name]; ok && o != t.Description {
			out = append(out, selectiongate.DescPair{Tool: t.Name, Old: o, New: t.Description})
		}
	}
	return out
}

// recordRejected stores and returns a `fail` for a candidate that is unusable before any
// selection is measured (for example a route its target cannot accept arguments for).
func recordRejected(db *store.DB, versionID, activeID string, golden *selectiongate.GoldenSet, sealed []selectiongate.Task, reason string) (selectiongate.Report, error) {
	decision := selectiongate.Decision{
		Verdict:      selectiongate.VerdictFail,
		Reason:       "rejected without measuring: " + reason,
		SealedN:      len(sealed),
		PValue:       1,
		SealedSHA256: golden.SealedHash(),
	}
	exp, err := db.InsertExperiment(versionID, activeID)
	if err != nil {
		return selectiongate.Report{}, err
	}
	scorecard, _ := json.Marshal(decision)
	if err := db.UpdateExperimentResult(exp.ID, experimentStatus(decision.Verdict), string(scorecard)); err != nil {
		return selectiongate.Report{}, err
	}
	return selectiongate.Report{VersionID: versionID, BaselineVersionID: activeID, GoldenName: golden.Name, Decision: decision}, nil
}
