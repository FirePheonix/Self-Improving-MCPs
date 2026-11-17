// Package simcp lets you build an MCP server with the self-improvement loop built in.
//
// You register tools with plain Go handlers. Serve runs them behind the same runtime the
// gateway uses, so every call is traced, the overlay (descriptions, routing, cache, composed
// tools) is applied and hot-reloaded, and `evolve` can observe, propose and gate improvements
// against your server without you writing any of that.
//
//	app := simcp.New("notes", "1.0.0")
//	app.Tool(simcp.Tool{
//	    Name:        "search_notes",
//	    Description: "Search notes by keyword.",
//	    Handler: func(ctx context.Context, args map[string]any) (string, error) { ... },
//	})
//	log.Fatal(app.Serve(ctx, config.ProxyConfig{DataDir: "data"}))
//
// To let evolve inspect your server's real, unpatched tool list (for the selection gate and
// golden-task drafting), point the upstream command in proxy.yaml at your binary with
// SIMCP_RAW=1 in its env: raw mode serves the handlers with no overlay and no tracing.
package simcp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"sync"

	"self-improving-mcp/config"
	"self-improving-mcp/mcp"
)

// Handler runs one tool call. A returned error becomes an MCP tool error (isError=true) that the
// agent can read and react to; it does not crash the server. A panic is recovered the same way.
type Handler func(ctx context.Context, args map[string]any) (string, error)

// Tool describes one tool and its implementation.
type Tool struct {
	Name        string
	Description string
	// InputSchema is a JSON Schema object. Nil means an object with no properties.
	InputSchema any
	Handler     Handler
}

// RawEnv, when set to "1" in the environment, makes Serve run the handlers directly: no
// overlay, no tracing, no status server. evolve uses this to read the true tool list.
const RawEnv = "SIMCP_RAW"

// App is an MCP server under construction.
type App struct {
	name, version string

	mu    sync.RWMutex
	tools map[string]Tool
}

// New creates an empty app.
func New(name, version string) *App {
	return &App{name: name, version: version, tools: map[string]Tool{}}
}

// Tool registers a tool. The name must be non-empty and unique, and Handler must be set.
func (a *App) Tool(t Tool) error {
	if t.Name == "" {
		return errors.New("simcp: tool name is empty")
	}
	if t.Handler == nil {
		return fmt.Errorf("simcp: tool %q has no handler", t.Name)
	}
	if t.Description == "" {
		return fmt.Errorf("simcp: tool %q has no description (agents choose tools by reading it)", t.Name)
	}
	if t.InputSchema == nil {
		t.InputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, dup := a.tools[t.Name]; dup {
		return fmt.Errorf("simcp: tool %q is already registered", t.Name)
	}
	a.tools[t.Name] = t
	return nil
}

// Upstream exposes the registered tools as the runtime's Upstream interface.
func (a *App) Upstream() mcp.Upstream { return &inProcess{app: a} }

// Serve runs the server over stdio until ctx ends or the client disconnects.
func (a *App) Serve(ctx context.Context, cfg config.ProxyConfig) error {
	if os.Getenv(RawEnv) == "1" {
		return a.serveRaw(ctx)
	}
	cfg = config.ApplyEnvProxy(cfg)
	return mcp.StartWithUpstream(ctx, cfg, a.Upstream(), "simcp:"+a.name)
}

func (a *App) serveRaw(ctx context.Context) error {
	p := mcp.NewProxy(a.Upstream())
	if err := p.RegisterUpstreamTools(ctx); err != nil {
		return err
	}
	log.Printf("%s: raw mode (no overlay, no tracing)", a.name)
	return p.Server.RunStdio(ctx)
}

type inProcess struct{ app *App }

func (u *inProcess) ListTools(context.Context) ([]mcp.ToolInfo, error) {
	u.app.mu.RLock()
	defer u.app.mu.RUnlock()
	names := make([]string, 0, len(u.app.tools))
	for n := range u.app.tools {
		names = append(names, n)
	}
	sort.Strings(names) // a stable order keeps the tool list, and so selection tests, reproducible
	out := make([]mcp.ToolInfo, 0, len(names))
	for _, n := range names {
		t := u.app.tools[n]
		out = append(out, mcp.ToolInfo{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return out, nil
}

func (u *inProcess) CallTool(ctx context.Context, name string, args map[string]any) (res mcp.CallResult, err error) {
	u.app.mu.RLock()
	t, ok := u.app.tools[name]
	u.app.mu.RUnlock()
	if !ok {
		return mcp.CallResult{}, fmt.Errorf("unknown tool %q", name)
	}
	defer func() {
		if r := recover(); r != nil {
			res = mcp.CallResult{IsError: true, Content: []mcp.Content{{Type: "text", Text: fmt.Sprintf("tool %s panicked: %v", name, r)}}}
			err = nil
		}
	}()
	text, herr := t.Handler(ctx, args)
	if herr != nil {
		return mcp.CallResult{IsError: true, Content: []mcp.Content{{Type: "text", Text: herr.Error()}}}, nil
	}
	return mcp.CallResult{Content: []mcp.Content{{Type: "text", Text: text}}}, nil
}

func (u *inProcess) Close() error { return nil }
