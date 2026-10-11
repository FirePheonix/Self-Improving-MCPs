package mcp

import (
	"context"
	"fmt"
	"log"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

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

	start := time.Now()
	res, err := p.Upstream.CallTool(ctx, name, args)
	latency := time.Since(start).Milliseconds()

	p.record(ctx, name, args, res, err, latency)

	if err != nil {
		return nil, err
	}

	out := &mcpsdk.CallToolResult{
		IsError:           res.IsError,
		StructuredContent: res.StructuredAny,
	}
	for _, c := range res.Content {
		switch c.Type {
		case "text", "":
			out.Content = append(out.Content, &mcpsdk.TextContent{Text: c.Text})
		default:
			out.Content = append(out.Content, &mcpsdk.TextContent{Text: fmt.Sprintf("[%s] %s", c.Type, c.Text)})
		}
	}
	return out, nil
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
