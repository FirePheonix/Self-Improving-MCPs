package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Version is a candidate or active MCP overlay.
type Version struct {
	ID          string
	ParentID    string
	Status      string
	OverlayJSON string
	Notes       string
	CreatedAt   string
	ActivatedAt string
}

// InsertVersion creates a new version row.
func (db *DB) InsertVersion(parentID, overlayJSON, notes string) (Version, error) {
	v := Version{
		ID:          uuid.NewString(),
		ParentID:    parentID,
		Status:      "candidate",
		OverlayJSON: overlayJSON,
		Notes:       notes,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	var parent any
	if parentID != "" {
		parent = parentID
	}

	_, err := db.sql.Exec(
		`INSERT INTO versions (id, parent_id, status, overlay_json, notes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		v.ID, parent, v.Status, v.OverlayJSON, v.Notes, v.CreatedAt,
	)
	if err != nil {
		return Version{}, fmt.Errorf("insert version: %w", err)
	}
	return v, nil
}

// GetVersion loads a version by id.
func (db *DB) GetVersion(id string) (Version, error) {
	var v Version
	var parentID, activatedAt sql.NullString
	err := db.sql.QueryRow(
		`SELECT id, parent_id, status, overlay_json, notes, created_at, activated_at
		 FROM versions WHERE id = ?`,
		id,
	).Scan(&v.ID, &parentID, &v.Status, &v.OverlayJSON, &v.Notes, &v.CreatedAt, &activatedAt)
	if err != nil {
		return Version{}, fmt.Errorf("get version: %w", err)
	}
	v.ParentID = parentID.String
	v.ActivatedAt = activatedAt.String
	return v, nil
}

// ActiveVersionID returns the current active version id, or empty if none.
func (db *DB) ActiveVersionID() (string, error) {
	var id sql.NullString
	err := db.sql.QueryRow(`SELECT version_id FROM active_version WHERE id = 1`).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("active version: %w", err)
	}
	return id.String, nil
}

// SetActiveVersion points the live proxy at versionID and marks it active.
func (db *DB) SetActiveVersion(versionID string) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(
		`UPDATE versions SET status = 'active', activated_at = ? WHERE id = ?`,
		now, versionID,
	); err != nil {
		return fmt.Errorf("mark version active: %w", err)
	}
	if _, err := tx.Exec(
		`UPDATE active_version SET version_id = ? WHERE id = 1`,
		versionID,
	); err != nil {
		return fmt.Errorf("set active pointer: %w", err)
	}
	return tx.Commit()
}
