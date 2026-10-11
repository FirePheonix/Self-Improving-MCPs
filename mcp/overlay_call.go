package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
)

func (p *Proxy) applyRouting(name string) (newName string, denied bool, note string) {
	for _, r := range p.Overlay.Routing {
		if r.FromTool != name {
			continue
		}
		if r.Deny {
			return name, true, r.Note
		}
		if r.ToTool != "" {
			return r.ToTool, false, r.Note
		}
	}
	return name, false, ""
}

func (p *Proxy) cacheTTL(tool string) time.Duration {
	for _, c := range p.Overlay.Cache {
		if c.ToolName == tool && c.TTLSeconds > 0 {
			return time.Duration(c.TTLSeconds) * time.Second
		}
	}
	return 0
}

func cacheKey(tool string, args map[string]any) string {
	raw, _ := json.Marshal(args)
	sum := sha256.Sum256(append([]byte(tool+":"), raw...))
	return hex.EncodeToString(sum[:])
}

func (p *Proxy) cacheGet(tool string, args map[string]any) (CallResult, bool) {
	ttl := p.cacheTTL(tool)
	if ttl <= 0 {
		return CallResult{}, false
	}
	key := cacheKey(tool, args)
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	ent, ok := p.cache[key]
	if !ok || time.Now().After(ent.expiresAt) {
		return CallResult{}, false
	}
	return ent.result, true
}

func (p *Proxy) cachePut(tool string, args map[string]any, res CallResult) {
	ttl := p.cacheTTL(tool)
	if ttl <= 0 || res.IsError {
		return
	}
	key := cacheKey(tool, args)
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	p.cache[key] = cacheEntry{result: res, expiresAt: time.Now().Add(ttl)}
}

func (p *Proxy) runComposed(ctx context.Context, steps []improvementsandbox.ComposedStep) (CallResult, error) {
	var last CallResult
	for _, step := range steps {
		args := step.Args
		if args == nil {
			args = map[string]any{}
		}
		res, err := p.Upstream.CallTool(ctx, step.Tool, args)
		if err != nil {
			return CallResult{}, err
		}
		last = res
		if res.IsError {
			return last, nil
		}
	}
	return last, nil
}

func callResultToSDK(res CallResult) *mcpsdk.CallToolResult {
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
	return out
}
