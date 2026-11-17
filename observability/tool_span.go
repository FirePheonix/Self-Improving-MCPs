package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "self-improving-mcp/observability"

// Attribute keys for fields this project defines (no OpenTelemetry standard covers them).
const (
	// AttrSessionID groups every call from one proxy session (one agent connection).
	// Spans only: a session id is unbounded, so it must never be a metric label.
	AttrSessionID = "simcp.session.id"
	// AttrOverlayVersion is the active overlay version, or "none" for the baseline upstream.
	AttrOverlayVersion = "simcp.overlay.version"
	// AttrToolRequested is the tool the client asked for, set only when routing changed it.
	AttrToolRequested = "simcp.tool.requested"
)

// ToolSpanInfo carries optional context to stamp on a tool span.
type ToolSpanInfo struct {
	SessionID      string // proxy session id; omitted when empty
	OverlayVersion string // active overlay version id; "none" when empty
	RequestedTool  string // what the client asked for; recorded only if it differs from the served tool
}

// StartToolSpan starts an execute_tool span for a tools/call.
func StartToolSpan(ctx context.Context, toolName string, info ToolSpanInfo) (context.Context, trace.Span) {
	tr := Tracer(instrumentationName)
	name := fmt.Sprintf("execute_tool %s", toolName)
	ctx, span := tr.Start(ctx, name)

	version := info.OverlayVersion
	if version == "" {
		version = "none"
	}
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", "execute_tool"),
		attribute.String("gen_ai.tool.name", toolName),
		attribute.String(AttrOverlayVersion, version),
	}
	if info.SessionID != "" {
		attrs = append(attrs, attribute.String(AttrSessionID, info.SessionID))
	}
	if info.RequestedTool != "" && info.RequestedTool != toolName {
		attrs = append(attrs, attribute.String(AttrToolRequested, info.RequestedTool))
	}
	span.SetAttributes(attrs...)
	return ctx, span
}

// EndToolSpan finishes the span with success or error details.
func EndToolSpan(span trace.Span, isError bool, errType string, err error) {
	if span == nil {
		return
	}
	if errType != "" {
		span.SetAttributes(attribute.String("error.type", errType))
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else if isError {
		span.SetStatus(codes.Error, errType)
	}
	span.End()
}
