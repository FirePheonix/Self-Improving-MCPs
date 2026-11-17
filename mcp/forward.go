package mcp

import (
	"context"
	"fmt"
	"log"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"self-improving-mcp/observability"
	"self-improving-mcp/tracer"
)

// forwardCall sends a tools/call to upstream and maps the result back.
func (p *Proxy) forwardCall(ctx context.Context, name string, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	if p == nil || p.Upstream == nil {
		return nil, ErrUpstreamNotConnected
	}

	args, err := argsFromRequest(req)
	if err != nil {
		return nil, err
	}

	ov, version := p.snapshot()
	requested := name
	routed, denied, note := applyRouting(ov, name)
	if denied {
		return &mcpsdk.CallToolResult{
			IsError: true,
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "denied by overlay routing: " + note}},
		}, nil
	}
	name = routed

	ctx, span := observability.StartToolSpan(ctx, name, observability.ToolSpanInfo{
		SessionID:      p.sessionID(),
		OverlayVersion: version,
		RequestedTool:  requested,
	})
	start := time.Now()

	var res CallResult
	if ct, ok := ov.ComposedTools[name]; ok {
		res, err = p.runComposed(ctx, ct.Steps)
	} else if cached, ok := p.cacheGet(ov, name, args); ok {
		res, err = cached, nil
	} else {
		res, err = p.Upstream.CallTool(ctx, name, args)
		if err == nil {
			p.cachePut(ov, name, args, res)
		}
	}

	latency := time.Since(start).Milliseconds()
	isError := err != nil || res.IsError
	errType := ""
	if err != nil {
		errType = "upstream_error"
	} else if res.IsError {
		errType = "tool_error"
	}
	observability.EndToolSpan(span, isError, errType, err)
	if metrics, merr := observability.GetToolMetrics(); merr == nil {
		metrics.RecordToolCall(ctx, name, latency, isError)
	}
	p.record(ctx, name, args, res, err, latency)

	if err != nil {
		return nil, err
	}
	out := callResultToSDK(res)
	if out == nil {
		return nil, fmt.Errorf("empty tool result")
	}
	return out, nil
}

// sessionID returns the trajectory session id when the tracer exposes one, else "".
func (p *Proxy) sessionID() string {
	if s, ok := p.Tracer.(interface{ SessionID() string }); ok {
		return s.SessionID()
	}
	return ""
}

func (p *Proxy) record(ctx context.Context, name string, args map[string]any, res CallResult, callErr error, latencyMs int64) {
	if p.Tracer == nil {
		return
	}

	argsJSON, argsHash := tracer.RedactPayload(args, 0)
	texts := make([]string, 0, len(res.Content))
	for _, c := range res.Content {
		texts = append(texts, c.Text)
	}
	isError := callErr != nil || res.IsError
	errType := ""
	if callErr != nil {
		errType = "upstream_error"
	} else if res.IsError {
		errType = "tool_error"
	}
	summary := tracer.SummarizeResult(texts, isError, 0)
	if callErr != nil && summary == "error" {
		summary = callErr.Error()
	}

	if err := p.Tracer.RecordCall(ctx, tracer.Call{
		ToolName:      name,
		ArgsJSON:      argsJSON,
		ArgsHash:      argsHash,
		ResultSummary: summary,
		IsError:       isError,
		ErrorType:     errType,
		LatencyMs:     latencyMs,
	}); err != nil {
		log.Printf("tracer record: %v", err)
	}
}
