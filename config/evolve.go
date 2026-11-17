package config

// EvolveConfig is offline evolution settings.
type EvolveConfig struct {
	// DataDir is where SQLite lives (usually shared with the proxy).
	DataDir string `yaml:"data_dir"`
	// Models selects OpenAI models for each role.
	Models EvolveModels `yaml:"models"`
	// Promote thresholds for accepting a candidate version.
	Promote PromoteConfig `yaml:"promote"`
	// Gate configures the selection-quality gate that decides whether a candidate may ship.
	Gate GateSettings `yaml:"gate"`
	// Notify configures where gate outcomes are announced besides the notifications folder.
	Notify NotifySettings `yaml:"notify"`
}

// NotifySettings configures outbound announcements.
type NotifySettings struct {
	// WebhookURL receives a JSON summary of every gate outcome (Slack incoming-webhook compatible).
	// The URL is a secret: prefer the EVOLVE_WEBHOOK_URL environment variable over the yaml file.
	WebhookURL string `yaml:"webhook_url"`
}

// GateSettings configures the selection gate (see selection-gate/). Zero values take safe defaults.
type GateSettings struct {
	// GoldenPath is the labeled golden set (dev + sealed). Required for the gate and for -auto-promote.
	GoldenPath string `yaml:"golden_path"`
	// Model is the chat model that plays the agent under test (default: models.reason).
	Model string `yaml:"model"`
	// Concurrency bounds parallel selector calls (default 8).
	Concurrency int `yaml:"concurrency"`
	// MinSealed is the smallest sealed set the gate will judge on (default 30).
	MinSealed int `yaml:"min_sealed"`
	// Alpha is the one-sided significance level for the improvement (default 0.05).
	Alpha float64 `yaml:"alpha"`
	// MaxBroken is how many previously-correct sealed tasks a candidate may break (default 0).
	MaxBroken int `yaml:"max_broken"`
	// MaxSelectorErrorRate is the selector-failure share beyond which a result is inconclusive (default 0.05).
	MaxSelectorErrorRate float64 `yaml:"max_selector_error_rate"`
	// ConfirmVotes is how many extra votes per arm settle tasks where baseline and candidate
	// disagree (default 8; a negative value disables confirmation). Hosted models are not fully
	// deterministic even at temperature 0, so one flip must not read as a regression.
	ConfirmVotes int `yaml:"confirm_votes"`
	// RetentionVotes is how many judge runs decide whether a new description dropped information
	// agents need after choosing the tool (default 3; negative disables the check).
	RetentionVotes int `yaml:"retention_votes"`
	// AcceptInfoLoss lets a pass ship even when the retention check reports dropped information.
	// Leave false: it exists for a person who has read the dropped sentences and agrees.
	AcceptInfoLoss bool `yaml:"accept_info_loss"`
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
