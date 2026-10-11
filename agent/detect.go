package agent

import "self-improving-mcp/store"

// DetectedFinding is a rule-based observation before persistence.
type DetectedFinding struct {
	Category     string
	Severity     string
	Title        string
	Detail       string
	EvidenceJSON string
}

// Detector analyzes a trajectory (ordered tool calls).
type Detector func(calls []store.ToolCall) []DetectedFinding
