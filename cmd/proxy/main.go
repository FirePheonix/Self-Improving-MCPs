package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"self-improving-mcp/config"
	"self-improving-mcp/mcp"
)

func main() {
	_ = config.LoadDotEnv(".env")
	configPath := flag.String("config", envOr("PROXY_CONFIG", "config/proxy.yaml"), "path to proxy yaml config")
	flag.Parse()

	cfg, err := config.LoadProxy(*configPath)
	if err != nil {
		// Fall back to example when user has not copied a real config yet.
		example := filepath.Join("config", "proxy.example.yaml")
		cfg, err = config.LoadProxy(example)
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		log.Printf("using %s (copy to config/proxy.yaml to customize)", example)
	}
	cfg = config.ApplyEnvProxy(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := mcp.Start(ctx, cfg); err != nil {
		log.Fatalf("proxy: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
