package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"self-improving-mcp/config"
)

// StdioUpstream is an Upstream backed by a child-process MCP server.
type StdioUpstream struct {
	session *mcpsdk.ClientSession
}

// ConnectStdio starts the upstream command and returns a connected Upstream.
func ConnectStdio(ctx context.Context, cfg config.UpstreamConfig) (*StdioUpstream, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("upstream command is required for stdio")
	}

	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	client := mcpsdk.NewClient(&mcpsdk.Implementation{
		Name:    "self-improving-mcp-proxy",
		Version: "0.1.0",
	}, nil)

	session, err := client.Connect(ctx, &mcpsdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect upstream stdio: %w", err)
	}

	return &StdioUpstream{session: session}, nil
}
