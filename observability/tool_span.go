package observability

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "self-improving-mcp/observability"

// StartToolSpan starts an execute_tool span for a tools/call.
func StartToolSpan(ctx context.Context, toolName string) (context.Context, trace.Span) {
	tr := Tracer(instrumentationName)
	name := fmt.Sprintf("execute_tool %s", toolName)
	ctx, span := tr.Start(ctx, name)
	span.SetAttributes(
		attribute.String("gen_ai.operation.name", "execute_tool"),
		attribute.String("gen_ai.tool.name", toolName),
	)
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
