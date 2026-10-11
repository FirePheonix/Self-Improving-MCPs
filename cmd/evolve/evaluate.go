package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"self-improving-mcp/agent"
	"self-improving-mcp/config"
	"self-improving-mcp/store"
)

func runEvaluate(ctx context.Context) error {
	fs := flag.NewFlagSet("evaluate", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	skipLLM := fs.Bool("skip-llm", false, "only run deterministic detectors")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
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

	findings, err := agent.EvaluateRules(db)
	if err != nil {
		return err
	}
	fmt.Printf("rule findings: %d\n", len(findings))
	for _, f := range findings {
		fmt.Printf("- [%s/%s] %s\n", f.Severity, f.Category, f.Title)
	}

	if *skipLLM {
		return nil
	}
	client, err := agent.NewClient(cfg.Models.Label, cfg.Models.Reason)
	if err != nil {
		log.Printf("skipping llm labels: %v", err)
		return nil
	}
	labeled, err := agent.LabelFindings(ctx, client, db, findings)
	if err != nil {
		return err
	}
	fmt.Printf("llm labels: %d\n", len(labeled))
	return nil
}

func loadEvolveConfig(path string) (config.EvolveConfig, error) {
	cfg, err := config.LoadEvolve(path)
	if err != nil {
		cfg, err = config.LoadEvolve(filepath.Join("config", "evolve.example.yaml"))
		if err != nil {
			return config.EvolveConfig{}, err
		}
	}
	return config.ApplyEnvEvolve(cfg), nil
}

func openStore(dataDir string) (*store.DB, error) {
	if dataDir == "" {
		dataDir = "data"
	}
	db, err := store.Open(filepath.Join(dataDir, "trajectories.db"))
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}
