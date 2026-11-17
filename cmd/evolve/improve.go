package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"self-improving-mcp/agent"
	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/mcp"
	selectiongate "self-improving-mcp/selection-gate"
)

// runImprove learns description changes from the DEV split of the golden set and registers the
// best one as a candidate. It never touches the sealed split: `evolve gate` judges the candidate
// there, which is what makes the result trustworthy.
func runImprove(ctx context.Context) error {
	fs := flag.NewFlagSet("improve", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	goldenPath := fs.String("golden", "", "golden set json (default: gate.golden_path); only its dev split is used")
	proxyCfgPath := fs.String("proxy-config", envOr("PROXY_CONFIG", "config/proxy.yaml"), "proxy config for upstream")
	rounds := fs.Int("rounds", 3, "proposal rounds on the dev split")
	historyPath := fs.String("history", "", "write every round's patches and dev result (json) to this path")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	cfg, err := loadEvolveConfig(*cfgPath)
	if err != nil {
		return err
	}
	golden := *goldenPath
	if golden == "" {
		golden = cfg.Gate.GoldenPath
	}
	if golden == "" {
		return fmt.Errorf("no golden set: pass -golden or set gate.golden_path")
	}
	g, err := selectiongate.LoadGolden(golden)
	if err != nil {
		return err
	}
	dev := g.Dev()
	if len(dev) == 0 {
		return fmt.Errorf("golden set %s has no dev tasks to learn from", golden)
	}

	db, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	activeID, err := db.ActiveVersionID()
	if err != nil {
		return err
	}
	var base improvementsandbox.Overlay
	if activeID != "" {
		v, err := db.GetVersion(activeID)
		if err != nil {
			return err
		}
		if base, err = improvementsandbox.ParseOverlay(v.OverlayJSON); err != nil {
			return fmt.Errorf("parse active overlay: %w", err)
		}
	}

	proxyCfg, err := loadProxyConfig(*proxyCfgPath)
	if err != nil {
		return err
	}
	upstream, err := mcp.ConnectStdio(ctx, proxyCfg.Upstream)
	if err != nil {
		return err
	}
	defer upstream.Close()
	live, err := upstream.ListTools(ctx)
	if err != nil {
		return err
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
		return err
	}
	zero := 0.0
	client.Temperature = &zero
	sel := agent.OpenAISelector{Client: client, Model: model}

	res, err := agent.ImproveDescriptions(ctx, client, sel, live, base, dev, agent.ImproveOptions{
		Rounds: *rounds, Concurrency: cfg.Gate.Concurrency, Model: model,
	})
	if err != nil {
		return err
	}
	fmt.Printf("dev baseline: %d of %d right (%d to learn from)\n", res.BaselineCorrect, res.DevN, res.Failures)
	for i, m := range res.Mistakes {
		if i == 5 {
			fmt.Printf("  ... and %d more mistake patterns (see -history)\n", len(res.Mistakes)-i)
			break
		}
		fmt.Printf("  mistake x%d: %s\n", m.Count, m.Pattern)
	}
	for _, r := range res.Rounds {
		if r.Err != "" {
			fmt.Printf("  round %d: %s\n", r.Round, r.Err)
			continue
		}
		fmt.Printf("  round %d: %d of %d (fixed %d, broke %d), %d patch(es)\n", r.Round, r.DevCorrect, res.DevN, r.Fixed, r.Broken, len(r.Patches.ToolPatches))
		if len(r.Pruned) > 0 {
			fmt.Printf("           dropped %s (more harm than good; full proposal: %s)\n", strings.Join(r.Pruned, ", "), r.Unpruned)
		}
	}
	if *historyPath != "" {
		raw, _ := json.MarshalIndent(res, "", "  ")
		if err := os.WriteFile(*historyPath, raw, 0o644); err != nil {
			return err
		}
	}
	if res.BestRound == 0 {
		fmt.Printf("no candidate registered: %s\n", res.Reason)
		return nil
	}
	merged := agent.MergePatches(base, res.Best)
	raw, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	if _, err := improvementsandbox.ParseOverlay(string(raw)); err != nil {
		return fmt.Errorf("improver produced an invalid overlay: %w", err)
	}
	v, err := db.InsertVersion(activeID, string(raw), fmt.Sprintf("improver %s (dev only; not yet judged on sealed)", res.Reason))
	if err != nil {
		return err
	}
	fmt.Printf("candidate %s registered (%s)\nnext: evolve gate -version %s   (judges it on the sealed tasks the improver never saw)\n", v.ID, res.Reason, v.ID)
	return nil
}
