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
	// Latency/call-count differences are noise on small samples; selection quality is the gate's job.
	if cfg.Promote.RequireEfficiencyWin {
		t.Fatal("RequireEfficiencyWin = true, want false by default")
	}
	// The gate section must parse; with no golden set configured, auto-promote is refused.
	if cfg.Gate.GoldenPath != "" {
		t.Fatalf("Gate.GoldenPath = %q, want empty in the example", cfg.Gate.GoldenPath)
	}
}

func TestLoadEvolveGateSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evolve.yaml")
	body := "data_dir: d\ngate:\n  golden_path: g.json\n  model: m\n  concurrency: 4\n  min_sealed: 40\n  alpha: 0.01\n  max_broken: 1\n  max_selector_error_rate: 0.1\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadEvolve(path)
	if err != nil {
		t.Fatal(err)
	}
	g := cfg.Gate
	if g.GoldenPath != "g.json" || g.Model != "m" || g.Concurrency != 4 || g.MinSealed != 40 ||
		g.Alpha != 0.01 || g.MaxBroken != 1 || g.MaxSelectorErrorRate != 0.1 {
		t.Fatalf("gate settings not parsed: %+v", g)
	}
}

func TestApplyEnvProxyOverridesDataDir(t *testing.T) {
	t.Setenv("DATA_DIR", filepath.Join(os.TempDir(), "sim-data"))
	cfg := ApplyEnvProxy(ProxyConfig{DataDir: "data"})
	if cfg.DataDir != filepath.Join(os.TempDir(), "sim-data") {
		t.Fatalf("DataDir = %q after env overlay", cfg.DataDir)
	}
}
