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
	// Dir is the upstream process's working directory (default: the proxy's). Servers that resolve
	// relative paths, like the filesystem server, resolve them against it.
	Dir string `yaml:"dir"`
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
	// StatusAddr, when set (e.g. "127.0.0.1:8787"), serves /health and /overlay over HTTP.
	// Empty disables it. Bind to loopback unless you front it with auth.
	StatusAddr string `yaml:"status_addr"`
	// ReloadIntervalSeconds is how often the proxy checks for a newly promoted overlay and
	// swaps it in without a restart. 0 uses the default (5); negative disables hot reload.
	ReloadIntervalSeconds int `yaml:"reload_interval_seconds"`
}
