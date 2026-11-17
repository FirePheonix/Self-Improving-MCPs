package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/mcp"
	"self-improving-mcp/publish"
	selectiongate "self-improving-mcp/selection-gate"
	"self-improving-mcp/store"
)

// runUpstreamPatch carries a gate-approved overlay's description changes back into the upstream
// server's own source (and docs) on a local side branch. It never pushes and never opens a PR.
func runUpstreamPatch(ctx context.Context) error {
	fs := flag.NewFlagSet("upstream-patch", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	versionID := fs.String("version", "", "overlay version id (must have a recorded gate pass)")
	proxyCfgPath := fs.String("proxy-config", envOr("PROXY_CONFIG", "config/proxy.yaml"), "proxy config for the live upstream")
	repo := fs.String("repo", "", "git repository holding the upstream server's source (required)")
	reportPath := fs.String("report", "", "optional full evidence report from `evolve gate -report` (adds the flipped tasks to the PR)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *versionID == "" || *repo == "" {
		return fmt.Errorf("-version and -repo are required")
	}
	cfg, err := loadEvolveConfig(*cfgPath)
	if err != nil {
		return err
	}
	db, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	ver, err := db.GetVersion(*versionID)
	if err != nil {
		return err
	}
	exps, err := db.ExperimentsForVersion(ver.ID)
	if err != nil {
		return err
	}
	decision, err := requireGatePass(exps)
	if err != nil {
		return err
	}
	rep := selectiongate.Report{VersionID: ver.ID, Decision: decision}
	if *reportPath != "" {
		if rep, err = loadReport(*reportPath, ver.ID); err != nil {
			return err
		}
	}
	overlay, err := improvementsandbox.ParseOverlay(ver.OverlayJSON)
	if err != nil {
		return fmt.Errorf("parse overlay: %w", err)
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
	targets, missing := sourceTargets(overlay, live)
	for _, m := range missing {
		fmt.Printf("note: overlay patches %q, which the upstream does not serve; skipped\n", m)
	}
	if len(targets) == 0 {
		return fmt.Errorf("this overlay has no description patches to carry into source (routing, cache and composed tools cannot be applied to source automatically)")
	}

	res, err := publish.CreateSourceBranch(*repo, rep, ver.OverlayJSON, overlay, targets)
	for _, s := range res.Plan.Skips {
		fmt.Printf("not changed: %s: %s\n", s.Tool, s.Reason)
	}
	if errors.Is(err, publish.ErrNoSourceEdits) {
		return fmt.Errorf("%w; see the notes above, then edit by hand", err)
	}
	if err != nil {
		return fmt.Errorf("create source branch: %w", err)
	}
	for _, e := range res.Plan.Edits {
		fmt.Printf("edited %s:%d (%s) for %s\n", e.File, e.Line, e.Kind, e.Tool)
	}
	fmt.Printf("source branch created: %s (commit %s)\nnothing was pushed; review the diff and run your server's tests, then push or open a PR yourself\n",
		res.Branch, res.Commit[:8])
	return nil
}

// requireGatePass finds the newest selection-gate decision among a version's experiments and
// returns it only if it is a pass. Replay-sandbox experiments are ignored: they say whether
// calls still run, not whether the agent picks better, so they cannot authorise a source change.
func requireGatePass(exps []store.Experiment) (selectiongate.Decision, error) {
	for _, e := range exps { // newest first
		var d selectiongate.Decision
		if json.Unmarshal([]byte(e.ScorecardJSON), &d) != nil || d.SealedSHA256 == "" {
			continue
		}
		if d.Verdict != selectiongate.VerdictPass {
			return selectiongate.Decision{}, fmt.Errorf("the latest selection-gate decision for this version is %q, not pass; only a pass may change source", d.Verdict)
		}
		return d, nil
	}
	return selectiongate.Decision{}, fmt.Errorf("no selection-gate decision recorded for this version; run `evolve gate -version <id>` first")
}

// sourceTargets pairs each description patch with the description the live upstream serves today.
func sourceTargets(o improvementsandbox.Overlay, live []mcp.ToolInfo) (targets []publish.SourceTarget, missing []string) {
	have := map[string]string{}
	for _, t := range live {
		have[t.Name] = t.Description
	}
	names := make([]string, 0, len(o.ToolPatches))
	for n := range o.ToolPatches {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := o.ToolPatches[n]
		if p.Description == nil {
			continue
		}
		old, ok := have[n]
		if !ok {
			missing = append(missing, n)
			continue
		}
		targets = append(targets, publish.SourceTarget{Tool: n, Old: old, New: *p.Description})
	}
	return targets, missing
}

func loadReport(path, versionID string) (selectiongate.Report, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return selectiongate.Report{}, err
	}
	var rep selectiongate.Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return selectiongate.Report{}, fmt.Errorf("parse report %s: %w", path, err)
	}
	if rep.VersionID != versionID {
		return selectiongate.Report{}, fmt.Errorf("report %s is for version %s, not %s", path, rep.VersionID, versionID)
	}
	if rep.Decision.Verdict != selectiongate.VerdictPass {
		return selectiongate.Report{}, fmt.Errorf("report %s verdict is %q, not pass", path, rep.Decision.Verdict)
	}
	return rep, nil
}
