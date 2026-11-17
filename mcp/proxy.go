package mcp

import (
	"sync"
	"time"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/tracer"
)

// Proxy is the Cursor-facing server backed by an Upstream.
type Proxy struct {
	Server   *Server
	Upstream Upstream
	Tracer   tracer.Tracer
	Overlay  improvementsandbox.Overlay
	// OverlayVersion is the active overlay version id ("" = baseline); stamped on spans.
	OverlayVersion string

	cacheMu sync.Mutex
	cache   map[string]cacheEntry

	// stateMu guards the hot-reloadable surface: the overlay snapshot, which tool names this
	// proxy has registered (so a reload can remove composed tools that disappeared), and the
	// last reload outcome shown on /overlay.
	stateMu       sync.RWMutex
	state         *overlayState
	registered    map[string]bool
	upstreamTools []ToolInfo
	reloadErr     string
}

// overlayState is one immutable overlay snapshot. A call reads it once and uses it throughout,
// so a reload mid-call can never mix the old routing with the new cache rules.
type overlayState struct {
	overlay  improvementsandbox.Overlay
	version  string
	loadedAt time.Time
}

// snapshot returns the overlay and version currently in force.
func (p *Proxy) snapshot() (improvementsandbox.Overlay, string) {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()
	if p.state != nil {
		return p.state.overlay, p.state.version
	}
	return p.Overlay, p.OverlayVersion
}

type cacheEntry struct {
	result    CallResult
	expiresAt time.Time
}

// NewProxy builds a proxy that will expose tools from upstream.
func NewProxy(upstream Upstream) *Proxy {
	return &Proxy{
		Server:   NewServer("self-improving-mcp", "0.1.0"),
		Upstream: upstream,
		cache:    map[string]cacheEntry{},

		registered: map[string]bool{},
	}
}
