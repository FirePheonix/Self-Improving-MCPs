package config

// UpstreamConfig describes how the proxy reaches the real MCP server.
type UpstreamConfig struct {
	// Command starts a local stdio MCP server (e.g. "npx").
	Command string `yaml:"command"`
	// Args are passed to Command.
	Args []string `yaml:"args"`
	// URL is an HTTP MCP endpoint. Used when Command is empty.
	URL string `yaml:"url"`
	// Env is extra environment for the upstream process.
	Env map[string]string `yaml:"env"`
}

// ProxyConfig is the hot-path proxy settings.
type ProxyConfig struct {
	// DataDir is where SQLite and runtime files live.
	DataDir string `yaml:"data_dir"`
	// Upstream is the real MCP server behind the proxy.
	Upstream UpstreamConfig `yaml:"upstream"`
	// RedactPayloads strips secrets and truncates stored args/results.
	RedactPayloads bool `yaml:"redact_payloads"`
	// CapturePayloadsInSpans puts args/results on OTel spans (default off).
	CapturePayloadsInSpans bool `yaml:"capture_payloads_in_spans"`
}
