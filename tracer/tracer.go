package tracer

import "context"

// Call is one tools/call observation to record.
type Call struct {
	Seq           int
	ToolName      string
	ArgsJSON      string
	ArgsHash      string
	ResultSummary string
	IsError       bool
	ErrorType     string
	LatencyMs     int64
}

// Tracer records tool-call trajectories.
type Tracer interface {
	RecordCall(ctx context.Context, call Call) error
}
