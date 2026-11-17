package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"self-improving-mcp/store"
)

func runLoop(ctx context.Context) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	skipLLM := fs.Bool("skip-llm", false, "skip openai labeling (propose still needs openai)")
	autoPromote := fs.Bool("auto-promote", false, "promote candidates that pass the replay sandbox AND the selection gate (refused if no golden set is configured)")
	branch := fs.Bool("branch", false, "on pass, record the overlay on a new local side branch (never pushes)")
	repo := fs.String("repo", ".", "git repository to create the side branch in")
	legacy := fs.Bool("legacy-propose", false, "use the findings-only proposer even when a golden set is configured")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	cfg, err := loadEvolveConfig(*cfgPath)
	if err != nil {
		return err
	}
	// Only a version created by THIS run may be judged. Without this, a run whose proposer registers
	// nothing would pick up an older candidate and gate, or even promote, something nobody asked about.
	runStart := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	useImprover := cfg.Gate.GoldenPath != "" && !*legacy

	fmt.Println("== evaluate ==")
	evalArgs := []string{"-config", *cfgPath}
	if *skipLLM {
		evalArgs = append(evalArgs, "-skip-llm")
	}
	os.Args = append([]string{"evolve"}, evalArgs...)
	if err := runEvaluate(ctx); err != nil {
		return fmt.Errorf("evaluate: %w", err)
	}

	if useImprover {
		// evidence-driven: learns from the golden set's dev split; the gate judges on sealed
		fmt.Println("== improve (dev split) ==")
		os.Args = []string{"evolve", "-config", *cfgPath}
		if err := runImprove(ctx); err != nil {
			return fmt.Errorf("improve: %w", err)
		}
	} else {
		fmt.Println("== propose ==")
		os.Args = []string{"evolve", "-config", *cfgPath}
		if err := runPropose(ctx); err != nil {
			return fmt.Errorf("propose: %w", err)
		}
	}

	db, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	versionID, err := newestCandidateSince(db, runStart)
	if err != nil {
		return err
	}
	if versionID == "" {
		fmt.Println("no new candidate in this run; nothing to judge")
		return nil
	}

	status := "passed"
	if useImprover {
		// the improver changes only what agents read; replaying recorded calls cannot test that
		fmt.Println("replay sandbox: skipped (description-only candidate; the selection gate judges it)")
	} else {
		fmt.Printf("== sandbox %s ==\n", versionID)
		os.Args = []string{"evolve", "-config", *cfgPath, "-version", versionID}
		if err := runSandbox(ctx); err != nil {
			return fmt.Errorf("sandbox: %w", err)
		}
		expRows, err := db.SQL().Query(
			`SELECT status FROM experiments WHERE version_id = ? ORDER BY id DESC LIMIT 1`, versionID,
		)
		if err != nil {
			return err
		}
		status = ""
		if expRows.Next() {
			_ = expRows.Scan(&status)
		}
		_ = expRows.Close()
	}

	versionArgs := []string{"evolve", "-config", *cfgPath, "-version", versionID}

	// The replay sandbox only shows that routed calls still run. A failure there is final.
	// Selection quality is judged on sealed golden tasks; without a golden set nothing may be
	// promoted automatically. The branching lives in decidePreGate/decidePostGate so it is tested.
	switch decidePreGate(status == "passed", cfg.Gate.GoldenPath != "", *autoPromote) {
	case preDiscardReplayFailed:
		fmt.Println("== discard (replay sandbox failed) ==")
		os.Args = versionArgs
		return runDiscard(ctx)
	case preRefuseAutoPromote:
		fmt.Println("selection gate: skipped (no gate.golden_path configured); selection quality is NOT verified")
		fmt.Println("refusing -auto-promote without the selection gate; review the candidate and run `evolve promote` yourself")
		return nil
	case preReviewOnly:
		fmt.Println("selection gate: skipped (no gate.golden_path configured); selection quality is NOT verified")
		fmt.Println("replay sandbox passed; candidate left in place for review")
		return nil
	}

	fmt.Println("== selection gate ==")
	rep, err := evaluateGate(ctx, db, cfg, versionID, cfg.Gate.GoldenPath, envOr("PROXY_CONFIG", "config/proxy.yaml"))
	if err != nil {
		return fmt.Errorf("selection gate: %w", err)
	}
	d := rep.Decision
	fmt.Printf("verdict: %s\nreason: %s\nsealed: %d tasks, baseline %d correct, candidate %d correct (fixed %d, broken %d)\n",
		d.Verdict, d.Reason, d.SealedN, d.BaselineCorrect, d.CandidateCorrect, d.Fixed, d.Broken)

	// Publish before acting on the verdict, so a candidate that is discarded still leaves a notice.
	if err := publishOutcome(db, cfg.DataDir, rep, publishOptions{Branch: *branch, Repo: *repo, WebhookURL: cfg.Notify.WebhookURL}); err != nil {
		return err
	}

	switch decidePostGate(d.Verdict, *autoPromote) {
	case postPromote:
		fmt.Println("== promote ==")
		os.Args = versionArgs
		return runPromote(ctx)
	case postAwaitManualPromote:
		fmt.Println("gate passed; run `evolve promote -version " + versionID + "` to activate (then restart proxy)")
	case postDiscard:
		fmt.Println("== discard (selection regressed) ==")
		os.Args = versionArgs
		return runDiscard(ctx)
	default:
		fmt.Println("NOT PROMOTED: evidence is inconclusive. The candidate is left in place for review.")
	}
	return nil
}

// newestCandidateSince returns the newest candidate created at or after since (RFC3339, UTC), or "".
// Older candidates are never returned: a run judges only what it produced.
func newestCandidateSince(db *store.DB, since string) (string, error) {
	var id string
	err := db.SQL().QueryRow(
		`SELECT id FROM versions WHERE status = 'candidate' AND created_at >= ? ORDER BY created_at DESC LIMIT 1`, since,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
