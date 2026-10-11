package config

// EvolveConfig is offline evolution settings.
type EvolveConfig struct {
	// DataDir is where SQLite lives (usually shared with the proxy).
	DataDir string `yaml:"data_dir"`
	// Models selects OpenAI models for each role.
	Models EvolveModels `yaml:"models"`
	// Promote thresholds for accepting a candidate version.
	Promote PromoteConfig `yaml:"promote"`
}

// EvolveModels names OpenAI models used offline.
type EvolveModels struct {
	// Label is the cheap model for bulk trajectory tags (e.g. gpt-4.1-nano).
	Label string `yaml:"label"`
	// Reason is used for evaluate/propose/sandbox agent tasks (e.g. gpt-4.1-mini).
	Reason string `yaml:"reason"`
}

// PromoteConfig is the simple stats gate before flipping active_version.
type PromoteConfig struct {
	// MaxErrorRateIncrease is the largest allowed error-rate regression.
	MaxErrorRateIncrease float64 `yaml:"max_error_rate_increase"`
	// MaxLatencyP95Increase is the largest allowed p95 latency regression (ratio, e.g. 0.1 = 10%).
	MaxLatencyP95Increase float64 `yaml:"max_latency_p95_increase"`
	// RequireEfficiencyWin requires at least one efficiency metric to improve.
	RequireEfficiencyWin bool `yaml:"require_efficiency_win"`
}
