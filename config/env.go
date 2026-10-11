package config

import "os"

// ApplyEnvProxy overlays well-known environment variables onto a ProxyConfig.
func ApplyEnvProxy(cfg ProxyConfig) ProxyConfig {
	if v := os.Getenv("DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	return cfg
}

// ApplyEnvEvolve overlays well-known environment variables onto an EvolveConfig.
func ApplyEnvEvolve(cfg EvolveConfig) EvolveConfig {
	if v := os.Getenv("DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	return cfg
}
