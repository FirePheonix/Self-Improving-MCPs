package mcp

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"self-improving-mcp/config"
	"self-improving-mcp/store"
	"self-improving-mcp/tracer"
)

// Start connects upstream, registers tools, and runs the Cursor-facing server.
func Start(ctx context.Context, cfg config.ProxyConfig) error {
	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = "data"
	}
	db, err := store.Open(filepath.Join(dataDir, "trajectories.db"))
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		return fmt.Errorf("migrate store: %w", err)
	}

	activeID, err := db.ActiveVersionID()
	if err != nil {
		return fmt.Errorf("read active version: %w", err)
	}
	if activeID == "" {
		log.Printf("active version: none (baseline upstream)")
	} else {
		log.Printf("active version: %s (overlay apply not wired yet)", activeID)
	}

	upstream, err := ConnectStdio(ctx, cfg.Upstream)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := upstream.Close(); cerr != nil {
			log.Printf("upstream close: %v", cerr)
		}
	}()

	upstreamLabel := cfg.Upstream.Command
	if upstreamLabel == "" {
		upstreamLabel = cfg.Upstream.URL
	}
	sessionTracer, err := tracer.StartSession(db, upstreamLabel)
	if err != nil {
		return fmt.Errorf("start tracer session: %w", err)
	}
	log.Printf("trajectory session: %s", sessionTracer.SessionID())

	proxy := NewProxy(upstream)
	proxy.Tracer = sessionTracer
	if err := proxy.RegisterUpstreamTools(ctx); err != nil {
		return fmt.Errorf("register tools: %w", err)
	}

	log.Printf("proxy ready; serving stdio")
	return proxy.Server.RunStdio(ctx)
}
