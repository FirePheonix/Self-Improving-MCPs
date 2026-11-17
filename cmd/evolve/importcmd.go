package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
)

// runImport registers an overlay file as a new candidate version. It is how a reviewed overlay
// (for example improvements/<id>/overlay.json from a side-branch PR, or one written by hand)
// enters the same lifecycle as a proposed one: gate, promote, rollback.
func runImport(ctx context.Context) error {
	_ = ctx
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	file := fs.String("file", "", "overlay json file (required)")
	notes := fs.String("notes", "", "note recorded with the version")
	parent := fs.String("parent", "", "optional parent version id")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("-file is required")
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	if _, err := improvementsandbox.ParseOverlay(string(raw)); err != nil {
		return fmt.Errorf("overlay is invalid, not imported: %w", err)
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
	if *notes == "" {
		*notes = "imported from " + *file
	}
	v, err := db.InsertVersion(*parent, string(raw), *notes)
	if err != nil {
		return err
	}
	fmt.Printf("imported as candidate %s\n", v.ID)
	return nil
}
