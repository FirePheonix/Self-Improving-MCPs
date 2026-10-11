package mcp

import "context"

// ListTools returns tools exposed by the upstream server.
func (u *StdioUpstream) ListTools(ctx context.Context) ([]ToolInfo, error) {
	if u == nil || u.session == nil {
		return nil, ErrUpstreamNotConnected
	}

	res, err := u.session.ListTools(ctx, nil)
	if err != nil {
		return nil, wrapUpstream("list_tools", "", err)
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
