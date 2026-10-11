package store

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed all:migrations
var migrationFS embed.FS

// Migrate applies all pending SQL migrations in lexical order.
func (db *DB) Migrate() error {
	if _, err := db.sql.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
	name TEXT PRIMARY KEY,
	applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(entries)

	for _, name := range entries {
		base := strings.TrimPrefix(name, "migrations/")
		var exists int
		if err := db.sql.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE name = ?`, base).Scan(&exists); err != nil {
			return fmt.Errorf("check migration %s: %w", base, err)
		}
		if exists > 0 {
			continue
		}

		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", base, err)
		}

		tx, err := db.sql.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", base, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, base); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", base, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}
