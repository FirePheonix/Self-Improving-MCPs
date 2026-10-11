package main

import (
	"context"
	"flag"
	"fmt"

	"self-improving-mcp/agent"
)

func runPropose(ctx context.Context) error {
	cfgPath := configPathFlag()
	flag.Parse()

	cfg, err := loadEvolveConfig(*cfgPath)
	if err != nil {
		return err
	}
	db, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()

	findings, err := db.ListFindings()
	if err != nil {
		return err
	}
	input := findings
	if len(input) > 40 {
		input = input[:40]
	}

	client, err := agent.NewClient(cfg.Models.Label, cfg.Models.Reason)
	if err != nil {
		return err
	}
	versions, err := agent.ProposeOverlays(ctx, client, db, input)
	if err != nil {
		return err
	}
	fmt.Printf("proposed versions: %d\n", len(versions))
	for _, v := range versions {
		fmt.Printf("- %s (%s)\n", v.ID, v.Status)
	}
	return nil
}
