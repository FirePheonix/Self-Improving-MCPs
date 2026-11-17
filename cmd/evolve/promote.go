package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"self-improving-mcp/store"
)

func runPromote(ctx context.Context) error {
	_ = ctx
	fs := flag.NewFlagSet("promote", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	versionID := fs.String("version", "", "version id to promote")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *versionID == "" {
		return fmt.Errorf("-version is required")
	}
	return promoteVersion(*cfgPath, *versionID)
}

func runDiscard(ctx context.Context) error {
	_ = ctx
	fs := flag.NewFlagSet("discard", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	versionID := fs.String("version", "", "version id to discard")
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
	db, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := setVersionStatus(db, *versionID, "discarded"); err != nil {
		return err
	}
	fmt.Printf("discarded %s\n", *versionID)
	return nil
}

func runRollback(ctx context.Context) error {
	_ = ctx
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
	versionID := fs.String("version", "", "version id to roll back to")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *versionID == "" {
		return fmt.Errorf("-version is required (a version id, or \"none\" for the baseline upstream)")
	}
	if *versionID == "none" {
		return clearActive(*cfgPath)
	}
	return promoteVersion(*cfgPath, *versionID)
}

func clearActive(cfgPath string) error {
	cfg, err := loadEvolveConfig(cfgPath)
	if err != nil {
		return err
	}
	db, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.ClearActiveVersion(); err != nil {
		return err
	}
	fmt.Println("active version -> none (baseline upstream; a running proxy picks this up within its reload interval)")
	return nil
}

func runVersions(ctx context.Context) error {
	_ = ctx
	fs := flag.NewFlagSet("versions", flag.ContinueOnError)
	cfgPath := fs.String("config", envOr("EVOLVE_CONFIG", "config/evolve.yaml"), "path to evolve yaml")
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

	active, _ := db.ActiveVersionID()
	rows, err := db.SQL().Query(`SELECT id, status, notes, created_at FROM versions ORDER BY created_at DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, status, notes, created string
		if err := rows.Scan(&id, &status, &notes, &created); err != nil {
			return err
		}
		mark := ""
		if id == active {
			mark = " [active]"
		}
		fmt.Printf("%s  %s  %s%s\n  %s\n", id, status, created, mark, notes)
	}
	return rows.Err()
}

func promoteVersion(cfgPath, versionID string) error {
	cfg, err := loadEvolveConfig(cfgPath)
	if err != nil {
		return err
	}
	db, err := openStore(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.SetActiveVersion(versionID); err != nil {
		return err
	}
	fmt.Printf("active version -> %s (a running proxy picks this up within its reload interval)\n", versionID)
	return nil
}

func setVersionStatus(db *store.DB, id, status string) error {
	_, err := db.SQL().Exec(`UPDATE versions SET status = ? WHERE id = ?`, status, id)
	return err
}
