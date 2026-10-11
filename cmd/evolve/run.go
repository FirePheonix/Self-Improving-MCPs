package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

func runLoop(ctx context.Context) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	skipLLM := fs.Bool("skip-llm", false, "skip openai labeling (propose still needs openai)")
	autoPromote := fs.Bool("auto-promote", false, "promote candidates that pass sandbox")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	fmt.Println("== evaluate ==")
	evalArgs := []string{"-config", *cfgPath}
	if *skipLLM {
		evalArgs = append(evalArgs, "-skip-llm")
	}
	os.Args = append([]string{"evolve"}, evalArgs...)
	if err := runEvaluate(ctx); err != nil {
		return fmt.Errorf("evaluate: %w", err)
	}

	fmt.Println("== propose ==")
	os.Args = []string{"evolve", "-config", *cfgPath}
	if err := runPropose(ctx); err != nil {
		return fmt.Errorf("propose: %w", err)
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

	rows, err := db.SQL().Query(
		`SELECT id FROM versions WHERE status = 'candidate' ORDER BY created_at DESC LIMIT 1`,
	)
	if err != nil {
		return err
	}
	var versionID string
	if rows.Next() {
		if err := rows.Scan(&versionID); err != nil {
			_ = rows.Close()
			return err
		}
	}
	_ = rows.Close()
	if versionID == "" {
		fmt.Println("no candidate version to sandbox")
		return nil
	}

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
	var status string
	if expRows.Next() {
		_ = expRows.Scan(&status)
	}
	_ = expRows.Close()

	if status == "passed" && *autoPromote {
		fmt.Println("== promote ==")
		os.Args = []string{"evolve", "-config", *cfgPath, "-version", versionID}
		return runPromote(ctx)
	}
	if status != "passed" {
		fmt.Println("== discard ==")
		os.Args = []string{"evolve", "-config", *cfgPath, "-version", versionID}
		return runDiscard(ctx)
	}
	fmt.Println("sandbox passed; re-run with -auto-promote to activate (then restart proxy)")
	return nil
}
