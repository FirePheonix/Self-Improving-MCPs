package improvementsandbox

// Overlay is a versioned set of proxy-side improvements.
type Overlay struct {
	ToolPatches   map[string]ToolPatch   `json:"tool_patches,omitempty"`
	Routing       []RouteRule            `json:"routing,omitempty"`
	Cache         []CacheRule            `json:"cache,omitempty"`
	ComposedTools map[string]ComposedTool `json:"composed_tools,omitempty"`
}

// ToolPatch changes how a tool is advertised on tools/list.
type ToolPatch struct {
	Description *string `json:"description,omitempty"`
	InputSchema any     `json:"input_schema,omitempty"`
}

// RouteRule redirects or denies a tool call pattern.
type RouteRule struct {
	FromTool string `json:"from_tool"`
	ToTool   string `json:"to_tool,omitempty"`
	Deny     bool   `json:"deny,omitempty"`
	Note     string `json:"note,omitempty"`
}

// CacheRule caches idempotent tool results by args hash.
type CacheRule struct {
	ToolName string `json:"tool_name"`
	TTLSeconds int  `json:"ttl_seconds"`
}

// ComposedTool expands to a fixed sequence of upstream calls.
type ComposedTool struct {
	Description string          `json:"description"`
	Steps       []ComposedStep  `json:"steps"`
}

// ComposedStep is one upstream call in a composed tool.
type ComposedStep struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args,omitempty"`
}
