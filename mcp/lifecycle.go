package mcp

import (
	"context"
	"fmt"
	"log"

	"self-improving-mcp/config"
)

// Start connects upstream, registers tools, and runs the Cursor-facing server.
func Start(ctx context.Context, cfg config.ProxyConfig) error {
	upstream, err := ConnectStdio(ctx, cfg.Upstream)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := upstream.Close(); cerr != nil {
			log.Printf("upstream close: %v", cerr)
		}
	}()

	proxy := NewProxy(upstream)
	if err := proxy.RegisterUpstreamTools(ctx); err != nil {
		return fmt.Errorf("register tools: %w", err)
	}

	log.Printf("proxy ready; serving stdio")
	return proxy.Server.RunStdio(ctx)
}
