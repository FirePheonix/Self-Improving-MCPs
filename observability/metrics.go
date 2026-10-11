package observability

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ToolMetrics holds instruments for MCP tool calls.
type ToolMetrics struct {
	calls    metric.Int64Counter
	errors   metric.Int64Counter
	latency  metric.Float64Histogram
}

var (
	toolMetricsOnce sync.Once
	toolMetrics     *ToolMetrics
	toolMetricsErr  error
)

// GetToolMetrics returns process-wide tool call metrics.
func GetToolMetrics() (*ToolMetrics, error) {
	toolMetricsOnce.Do(func() {
		m := Meter(instrumentationName)
		var err error
		tm := &ToolMetrics{}
		tm.calls, err = m.Int64Counter("mcp.tool.calls",
			metric.WithDescription("Number of MCP tool calls through the proxy"))
		if err != nil {
			toolMetricsErr = err
			return
		}
		tm.errors, err = m.Int64Counter("mcp.tool.errors",
			metric.WithDescription("Number of failed MCP tool calls"))
		if err != nil {
			toolMetricsErr = err
			return
		}
		tm.latency, err = m.Float64Histogram("mcp.tool.latency_ms",
			metric.WithDescription("MCP tool call latency in milliseconds"),
			metric.WithUnit("ms"))
		if err != nil {
			toolMetricsErr = err
			return
		}
		toolMetrics = tm
	})
	if toolMetricsErr != nil {
		return nil, fmt.Errorf("tool metrics: %w", toolMetricsErr)
	}
	return toolMetrics, nil
}

// RecordToolCall updates counters and latency histogram for one call.
func (m *ToolMetrics) RecordToolCall(ctx context.Context, toolName string, latencyMs int64, isError bool) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("gen_ai.tool.name", toolName))
	m.calls.Add(ctx, 1, attrs)
	m.latency.Record(ctx, float64(latencyMs), attrs)
	if isError {
		m.errors.Add(ctx, 1, attrs)
	}
}
