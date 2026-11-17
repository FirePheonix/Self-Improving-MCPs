package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
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
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	p.upstreamTools = tools
	if p.state == nil {
		p.state = &overlayState{overlay: p.Overlay, version: p.OverlayVersion, loadedAt: time.Now()}
	}
	p.syncToolsLocked()
	return nil
}

// syncToolsLocked makes the served tool list match VisibleTools(upstream, current overlay).
// AddTool replaces a tool of the same name and tells connected clients the list changed;
// composed tools that the new overlay no longer has are removed. Caller holds stateMu.
func (p *Proxy) syncToolsLocked() {
	want := map[string]bool{}
	for _, t := range VisibleTools(p.upstreamTools, p.state.overlay) {
		name := t.Name
		want[name] = true
		p.Server.SDK().AddTool(&mcpsdk.Tool{
			Name:        name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return p.forwardCall(ctx, name, req)
		})
	}
	var gone []string
	for name := range p.registered {
		if !want[name] {
			gone = append(gone, name)
		}
	}
	if len(gone) > 0 {
		p.Server.SDK().RemoveTools(gone...)
	}
	p.registered = want
}

// Reload swaps in a new overlay without restarting: the tool list is re-synced (clients are
// notified), the result cache is dropped because cache rules may have changed, and later calls
// see the new routing. Calls already running finish on the snapshot they started with.
func (p *Proxy) Reload(version string, overlay improvementsandbox.Overlay) {
	p.stateMu.Lock()
	p.state = &overlayState{overlay: overlay, version: version, loadedAt: time.Now()}
	p.reloadErr = ""
	p.syncToolsLocked()
	p.stateMu.Unlock()

	p.cacheMu.Lock()
	p.cache = map[string]cacheEntry{}
	p.cacheMu.Unlock()
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
