package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server is the Cursor-facing MCP server (stdio).
type Server struct {
	sdk *mcpsdk.Server
}

// NewServer creates an empty Cursor-facing MCP server.
func NewServer(name, version string) *Server {
	return &Server{
		sdk: mcpsdk.NewServer(&mcpsdk.Implementation{
			Name:    name,
			Version: version,
		}, nil),
	}
}

// RunStdio serves MCP over stdin/stdout until the client disconnects.
func (s *Server) RunStdio(ctx context.Context) error {
	if s == nil || s.sdk == nil {
		return fmt.Errorf("server not initialized")
	}
	return s.sdk.Run(ctx, &mcpsdk.StdioTransport{})
}

// SDK exposes the underlying SDK server for registration helpers.
func (s *Server) SDK() *mcpsdk.Server {
	return s.sdk
}
