package mcp

import (
	"context"
	"fmt"
)

// ListTools returns tools exposed by the upstream server.
func (u *StdioUpstream) ListTools(ctx context.Context) ([]ToolInfo, error) {
	if u == nil || u.session == nil {
		return nil, fmt.Errorf("upstream not connected")
	}

	res, err := u.session.ListTools(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("list upstream tools: %w", err)
	}

	out := make([]ToolInfo, 0, len(res.Tools))
	for _, t := range res.Tools {
		out = append(out, ToolInfo{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	return out, nil
}
