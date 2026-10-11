package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// forwardCall sends a tools/call to upstream and maps the result back.
func (p *Proxy) forwardCall(ctx context.Context, name string, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if p == nil || p.Upstream == nil {
		return nil, ErrUpstreamNotConnected
	}

	args, err := argsFromRequest(req)
	if err != nil {
		return nil, err
	}

	res, err := p.Upstream.CallTool(ctx, name, args)
	if err != nil {
		return nil, err
	}

	out := &mcpsdk.CallToolResult{
		IsError:           res.IsError,
		StructuredContent: res.StructuredAny,
	}
	for _, c := range res.Content {
		switch c.Type {
		case "text", "":
			out.Content = append(out.Content, &mcpsdk.TextContent{Text: c.Text})
		default:
			out.Content = append(out.Content, &mcpsdk.TextContent{Text: fmt.Sprintf("[%s] %s", c.Type, c.Text)})
		}
	}
	return out, nil
}
