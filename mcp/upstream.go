package mcp

import "context"

// ToolInfo is one upstream tool as the proxy sees it.
type ToolInfo struct {
	Name        string
	Description string
	// InputSchema is raw JSON Schema (usually map[string]any or json.RawMessage).
	InputSchema any
}

// CallResult is the outcome of an upstream tools/call.
type CallResult struct {
	Content       []Content
	IsError       bool
	StructuredAny any
}

// Content is a simplified content block from an upstream tool.
type Content struct {
	Type string
	Text string
}

// Upstream talks to the real MCP server behind the proxy.
type Upstream interface {
	ListTools(ctx context.Context) ([]ToolInfo, error)
	CallTool(ctx context.Context, name string, args map[string]any) (CallResult, error)
	Close() error
}
