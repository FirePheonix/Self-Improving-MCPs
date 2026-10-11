package tracer

import (
	"context"
	"fmt"

	"self-improving-mcp/store"
)

// SQLiteTracer persists calls into the shared store.
type SQLiteTracer struct {
	db        *store.DB
	sessionID string
}

// NewSQLiteTracer records into an existing session.
func NewSQLiteTracer(db *store.DB, sessionID string) *SQLiteTracer {
	return &SQLiteTracer{db: db, sessionID: sessionID}
}

// RecordCall appends one tool call. Call.Seq must be set by the caller.
func (t *SQLiteTracer) RecordCall(ctx context.Context, call Call) error {
	_ = ctx
	if t == nil || t.db == nil {
		return fmt.Errorf("tracer not initialized")
	}
	if call.Seq <= 0 {
		return fmt.Errorf("call seq must be positive")
	}

	_, err := t.db.InsertToolCall(store.ToolCall{
		SessionID:     t.sessionID,
		Seq:           call.Seq,
		ToolName:      call.ToolName,
		ArgsJSON:      call.ArgsJSON,
		ArgsHash:      call.ArgsHash,
		ResultSummary: call.ResultSummary,
		IsError:       call.IsError,
		ErrorType:     call.ErrorType,
		LatencyMs:     call.LatencyMs,
	})
	if err != nil {
		return fmt.Errorf("record call: %w", err)
	}
	return nil
}
