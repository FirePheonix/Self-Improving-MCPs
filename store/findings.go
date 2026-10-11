package store

import (
	"fmt"
	"time"
)

// Finding is an evaluator observation about trajectories.
type Finding struct {
	ID           int64
	Category     string
	Severity     string
	Title        string
	Detail       string
	EvidenceJSON string
	CreatedAt    string
}

// InsertFinding stores a finding.
func (db *DB) InsertFinding(f Finding) (Finding, error) {
	if f.Severity == "" {
		f.Severity = "info"
	}
	if f.EvidenceJSON == "" {
		f.EvidenceJSON = "[]"
	}
	if f.CreatedAt == "" {
		f.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	res, err := db.sql.Exec(
		`INSERT INTO findings (category, severity, title, detail, evidence_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		f.Category, f.Severity, f.Title, f.Detail, f.EvidenceJSON, f.CreatedAt,
	)
	if err != nil {
		return Finding{}, fmt.Errorf("insert finding: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Finding{}, err
	}
	f.ID = id
	return f, nil
}

// ListFindings returns findings newest first.
func (db *DB) ListFindings() ([]Finding, error) {
	rows, err := db.sql.Query(
		`SELECT id, category, severity, title, detail, evidence_json, created_at
		 FROM findings ORDER BY id DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list findings: %w", err)
	}
	defer rows.Close()

	var out []Finding
	for rows.Next() {
		var f Finding
		if err := rows.Scan(
			&f.ID, &f.Category, &f.Severity, &f.Title, &f.Detail, &f.EvidenceJSON, &f.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
