package agent

import (
	"fmt"

	"self-improving-mcp/store"
)

// DefaultDetectors is the ordered set of deterministic detectors.
func DefaultDetectors() []Detector {
	return []Detector{
		DetectRedundant,
		DetectRetries,
		DetectRepeatedFailures,
		DetectFrequentSequences,
		DetectLatencyOutliers,
	}
}

// EvaluateRules runs all detectors over every session's trajectory and persists findings.
func EvaluateRules(db *store.DB) ([]store.Finding, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	sessions, err := listSessionIDs(db)
	if err != nil {
		return nil, err
	}

	detectors := DefaultDetectors()
	var persisted []store.Finding
	for _, sid := range sessions {
		calls, err := db.ListToolCallsBySession(sid)
		if err != nil {
			return nil, err
		}
		for _, det := range detectors {
			for _, d := range det(calls) {
				f, err := db.InsertFinding(store.Finding{
					Category:     d.Category,
					Severity:     d.Severity,
					Title:        d.Title,
					Detail:       d.Detail,
					EvidenceJSON: d.EvidenceJSON,
				})
				if err != nil {
					return nil, err
				}
				persisted = append(persisted, f)
			}
		}
	}
	return persisted, nil
}

func listSessionIDs(db *store.DB) ([]string, error) {
	rows, err := db.SQL().Query(`SELECT id FROM sessions ORDER BY started_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
