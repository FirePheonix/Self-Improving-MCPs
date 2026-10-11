package mcp

// Proxy is the Cursor-facing server backed by an Upstream.
type Proxy struct {
	Server   *Server
	Upstream Upstream
}

// NewProxy builds a proxy that will expose tools from upstream.
func NewProxy(upstream Upstream) *Proxy {
	return &Proxy{
		Server:   NewServer("self-improving-mcp", "0.1.0"),
		Upstream: upstream,
	}
}
