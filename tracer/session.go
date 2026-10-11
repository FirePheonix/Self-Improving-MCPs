package tracer

import (
	"context"
	"fmt"
	"sync"

	"self-improving-mcp/store"
)

// SessionTracer creates a store session and assigns sequence numbers.
type SessionTracer struct {
	inner *SQLiteTracer

	mu  sync.Mutex
	seq int
}

// StartSession opens a new trajectory session for the given upstream label.
func StartSession(db *store.DB, upstream string) (*SessionTracer, error) {
	if db == nil {
		return nil, fmt.Errorf("store db is nil")
	}
	session, err := db.CreateSession(upstream)
	if err != nil {
		return nil, err
	}
	return &SessionTracer{inner: NewSQLiteTracer(db, session.ID)}, nil
}

// SessionID returns the underlying session id.
func (t *SessionTracer) SessionID() string {
	if t == nil || t.inner == nil {
		return ""
	}
	return t.inner.sessionID
}

// RecordCall assigns the next seq and persists the call.
func (t *SessionTracer) RecordCall(ctx context.Context, call Call) error {
	if t == nil || t.inner == nil {
		return fmt.Errorf("session tracer not initialized")
	}
	t.mu.Lock()
	t.seq++
	call.Seq = t.seq
	t.mu.Unlock()
	return t.inner.RecordCall(ctx, call)
}
