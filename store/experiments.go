package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Experiment is one sandbox comparison run.
type Experiment struct {
	ID                int64
	VersionID         string
	BaselineVersionID string
	Status            string
	ScorecardJSON     string
	CreatedAt         string
	FinishedAt        string
}

// InsertExperiment creates a pending experiment.
func (db *DB) InsertExperiment(versionID, baselineVersionID string) (Experiment, error) {
	e := Experiment{
		VersionID:         versionID,
		BaselineVersionID: baselineVersionID,
		Status:            "pending",
		ScorecardJSON:     "{}",
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
	}

	var baseline any
	if baselineVersionID != "" {
		baseline = baselineVersionID
	}

	res, err := db.sql.Exec(
		`INSERT INTO experiments (version_id, baseline_version_id, status, scorecard_json, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		e.VersionID, baseline, e.Status, e.ScorecardJSON, e.CreatedAt,
	)
	if err != nil {
		return Experiment{}, fmt.Errorf("insert experiment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Experiment{}, err
	}
	e.ID = id
	return e, nil
}

// UpdateExperimentResult sets status, scorecard, and finished_at.
func (db *DB) UpdateExperimentResult(id int64, status, scorecardJSON string) error {
	finished := time.Now().UTC().Format(time.RFC3339)
	_, err := db.sql.Exec(
		`UPDATE experiments SET status = ?, scorecard_json = ?, finished_at = ? WHERE id = ?`,
		status, scorecardJSON, finished, id,
	)
	if err != nil {
		return fmt.Errorf("update experiment: %w", err)
	}
	return nil
}

// GetExperiment loads an experiment by id.
func (db *DB) GetExperiment(id int64) (Experiment, error) {
	var e Experiment
	var baseline, finished sql.NullString
	err := db.sql.QueryRow(
		`SELECT id, version_id, baseline_version_id, status, scorecard_json, created_at, finished_at
		 FROM experiments WHERE id = ?`,
		id,
	).Scan(&e.ID, &e.VersionID, &baseline, &e.Status, &e.ScorecardJSON, &e.CreatedAt, &finished)
	if err != nil {
		return Experiment{}, fmt.Errorf("get experiment: %w", err)
	}
	e.BaselineVersionID = baseline.String
	e.FinishedAt = finished.String
	return e, nil
}
