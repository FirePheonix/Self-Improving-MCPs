package mcp

import (
	"context"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// CallTool invokes a tool on the upstream server.
func (u *StdioUpstream) CallTool(ctx context.Context, name string, args map[string]any) (CallResult, error) {
	if u == nil || u.session == nil {
		return CallResult{}, fmt.Errorf("upstream not connected")
	}
	if args == nil {
		args = map[string]any{}
	}

	res, err := u.session.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return CallResult{}, fmt.Errorf("call upstream tool %s: %w", name, err)
	}

	out := CallResult{
		IsError:       res.IsError,
		StructuredAny: res.StructuredContent,
	}
	for _, c := range res.Content {
		switch v := c.(type) {
		case *mcpsdk.TextContent:
			out.Content = append(out.Content, Content{Type: "text", Text: v.Text})
		default:
			out.Content = append(out.Content, Content{Type: "unknown", Text: fmt.Sprintf("%v", c)})
		}
	}
	return out, nil
}
