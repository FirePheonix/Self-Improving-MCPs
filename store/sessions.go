package store

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Session is one proxy runtime session that groups tool calls.
type Session struct {
	ID        string
	StartedAt string
	EndedAt   string
	Upstream  string
	Notes     string
}

// CreateSession inserts a new session and returns it.
func (db *DB) CreateSession(upstream string) (Session, error) {
	s := Session{
		ID:        uuid.NewString(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		Upstream:  upstream,
	}
	_, err := db.sql.Exec(
		`INSERT INTO sessions (id, started_at, upstream) VALUES (?, ?, ?)`,
		s.ID, s.StartedAt, s.Upstream,
	)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return s, nil
}

// GetSession loads a session by id.
func (db *DB) GetSession(id string) (Session, error) {
	var s Session
	var endedAt *string
	err := db.sql.QueryRow(
		`SELECT id, started_at, ended_at, upstream, notes FROM sessions WHERE id = ?`,
		id,
	).Scan(&s.ID, &s.StartedAt, &endedAt, &s.Upstream, &s.Notes)
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	if endedAt != nil {
		s.EndedAt = *endedAt
	}
	return s, nil
}
