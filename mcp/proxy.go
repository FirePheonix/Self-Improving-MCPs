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

	cacheMu sync.Mutex
	cache   map[string]cacheEntry
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
	}
}
