package store

import (
	"fmt"
	"time"
)

// ToolCall is one recorded MCP tools/call.
type ToolCall struct {
	ID            int64
	SessionID     string
	Seq           int
	ToolName      string
	ArgsJSON      string
	ArgsHash      string
	ResultSummary string
	IsError       bool
	ErrorType     string
	LatencyMs     int64
	CreatedAt     string
}

// InsertToolCall appends a tool call to a session.
func (db *DB) InsertToolCall(tc ToolCall) (ToolCall, error) {
	if tc.CreatedAt == "" {
		tc.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	isError := 0
	if tc.IsError {
		isError = 1
	}
	res, err := db.sql.Exec(
		`INSERT INTO tool_calls (
			session_id, seq, tool_name, args_json, args_hash,
			result_summary, is_error, error_type, latency_ms, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tc.SessionID, tc.Seq, tc.ToolName, tc.ArgsJSON, tc.ArgsHash,
		tc.ResultSummary, isError, tc.ErrorType, tc.LatencyMs, tc.CreatedAt,
	)
	if err != nil {
		return ToolCall{}, fmt.Errorf("insert tool call: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ToolCall{}, err
	}
	tc.ID = id
	return tc, nil
}

// ListToolCallsBySession returns tool calls in sequence order.
func (db *DB) ListToolCallsBySession(sessionID string) ([]ToolCall, error) {
	rows, err := db.sql.Query(
		`SELECT id, session_id, seq, tool_name, args_json, args_hash,
		        result_summary, is_error, error_type, latency_ms, created_at
		 FROM tool_calls WHERE session_id = ? ORDER BY seq ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tool calls: %w", err)
	}
	defer rows.Close()

	var out []ToolCall
	for rows.Next() {
		var tc ToolCall
		var isError int
		if err := rows.Scan(
			&tc.ID, &tc.SessionID, &tc.Seq, &tc.ToolName, &tc.ArgsJSON, &tc.ArgsHash,
			&tc.ResultSummary, &isError, &tc.ErrorType, &tc.LatencyMs, &tc.CreatedAt,
		); err != nil {
			return nil, err
		}
		tc.IsError = isError != 0
		out = append(out, tc)
	}
	return out, rows.Err()
}
