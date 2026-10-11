package mcp

import (
	"errors"
	"fmt"
)

// ErrUpstreamNotConnected means the proxy has no live upstream session.
var ErrUpstreamNotConnected = errors.New("upstream not connected")

// UpstreamError wraps a failure talking to or returned by the upstream MCP.
type UpstreamError struct {
	Op   string
	Tool string
	Err  error
}

func (e *UpstreamError) Error() string {
	if e.Tool != "" {
		return fmt.Sprintf("upstream %s %s: %v", e.Op, e.Tool, e.Err)
	}
	return fmt.Sprintf("upstream %s: %v", e.Op, e.Err)
}

func (e *UpstreamError) Unwrap() error { return e.Err }

func wrapUpstream(op, tool string, err error) error {
	if err == nil {
		return nil
	}
	return &UpstreamError{Op: op, Tool: tool, Err: err}
}
