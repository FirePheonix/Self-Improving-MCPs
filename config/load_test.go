package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProxyExample(t *testing.T) {
	cfg, err := LoadProxy("proxy.example.yaml")
	if err != nil {
		t.Fatalf("LoadProxy: %v", err)
	}
	if cfg.DataDir != "data" {
		t.Fatalf("DataDir = %q, want data", cfg.DataDir)
	}
	if cfg.Upstream.Command != "npx" {
		t.Fatalf("Upstream.Command = %q, want npx", cfg.Upstream.Command)
	}
	if !cfg.RedactPayloads {
		t.Fatal("RedactPayloads = false, want true")
	}
	if cfg.CapturePayloadsInSpans {
		t.Fatal("CapturePayloadsInSpans = true, want false")
	}
}

func TestLoadEvolveExample(t *testing.T) {
	cfg, err := LoadEvolve("evolve.example.yaml")
	if err != nil {
		t.Fatalf("LoadEvolve: %v", err)
	}
	if cfg.Models.Label != "gpt-4.1-nano" {
		t.Fatalf("Models.Label = %q, want gpt-4.1-nano", cfg.Models.Label)
	}
	if cfg.Models.Reason != "gpt-4.1-mini" {
		t.Fatalf("Models.Reason = %q, want gpt-4.1-mini", cfg.Models.Reason)
	}
	if !cfg.Promote.RequireEfficiencyWin {
		t.Fatal("RequireEfficiencyWin = false, want true")
	}
}

func TestApplyEnvProxyOverridesDataDir(t *testing.T) {
	t.Setenv("DATA_DIR", filepath.Join(os.TempDir(), "sim-data"))
	cfg := ApplyEnvProxy(ProxyConfig{DataDir: "data"})
	if cfg.DataDir != filepath.Join(os.TempDir(), "sim-data") {
		t.Fatalf("DataDir = %q after env overlay", cfg.DataDir)
	}
}
