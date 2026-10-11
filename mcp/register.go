package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterUpstreamTools lists upstream tools and re-exposes them 1:1 on the proxy,
// applying description/schema patches and composed tools from the active overlay.
func (p *Proxy) RegisterUpstreamTools(ctx context.Context) error {
	if p == nil || p.Upstream == nil || p.Server == nil {
		return fmt.Errorf("proxy not initialized")
	}

	tools, err := p.Upstream.ListTools(ctx)
	if err != nil {
		return err
	}

	for _, tool := range tools {
		t := applyToolPatch(tool, p.Overlay)
		name := t.Name
		p.Server.SDK().AddTool(&mcpsdk.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return p.forwardCall(ctx, name, req)
		})
	}

	for name, ct := range p.Overlay.ComposedTools {
		n := name
		desc := ct.Description
		schema := any(map[string]any{"type": "object", "properties": map[string]any{}})
		p.Server.SDK().AddTool(&mcpsdk.Tool{
			Name:        n,
			Description: desc,
			InputSchema: schema,
		}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return p.forwardCall(ctx, n, req)
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
