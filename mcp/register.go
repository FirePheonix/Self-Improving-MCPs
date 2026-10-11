package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterUpstreamTools lists upstream tools and re-exposes them 1:1 on the proxy.
func (p *Proxy) RegisterUpstreamTools(ctx context.Context) error {
	if p == nil || p.Upstream == nil || p.Server == nil {
		return fmt.Errorf("proxy not initialized")
	}

	tools, err := p.Upstream.ListTools(ctx)
	if err != nil {
		return err
	}

	for _, tool := range tools {
		t := tool
		p.Server.SDK().AddTool(&mcpsdk.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return p.forwardCall(ctx, t.Name, req)
		})
	}
	return nil
}

func argsFromRequest(req *mcpsdk.CallToolRequest) (map[string]any, error) {
	args := map[string]any{}
	if req == nil || req.Params == nil || len(req.Params.Arguments) == 0 {
		return args, nil
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		return nil, fmt.Errorf("decode tool arguments: %w", err)
	}
	return args, nil
}

// forwardCall is stubbed until call forwarding lands.
func (p *Proxy) forwardCall(_ context.Context, name string, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	return nil, fmt.Errorf("forward not wired for tool %s", name)
}
