package mcp

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"self-improving-mcp/config"
	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/observability"
	"self-improving-mcp/store"
	"self-improving-mcp/tracer"
)

// Start connects upstream, registers tools, and runs the Cursor-facing server.
func Start(ctx context.Context, cfg config.ProxyConfig) error {
	otelProviders, err := observability.SetupFromEnv(ctx)
	if err != nil {
		return fmt.Errorf("otel setup: %w", err)
	}
	defer func() {
		if serr := otelProviders.Shutdown(context.Background()); serr != nil {
			log.Printf("otel shutdown: %v", serr)
		}
	}()

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
	var overlay improvementsandbox.Overlay
	if activeID == "" {
		log.Printf("active version: none (baseline upstream)")
	} else {
		ver, err := db.GetVersion(activeID)
		if err != nil {
			return fmt.Errorf("load active version: %w", err)
		}
		overlay, err = improvementsandbox.ParseOverlay(ver.OverlayJSON)
		if err != nil {
			return fmt.Errorf("parse active overlay: %w", err)
		}
		log.Printf("active version: %s", activeID)
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
	proxy.Overlay = overlay
	if err := proxy.RegisterUpstreamTools(ctx); err != nil {
		return fmt.Errorf("register tools: %w", err)
	}

	log.Printf("proxy ready; serving stdio")
	return proxy.Server.RunStdio(ctx)
}
