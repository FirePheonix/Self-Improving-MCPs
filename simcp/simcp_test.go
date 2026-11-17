package simcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/mcp"
)

func echo(_ context.Context, a map[string]any) (string, error) {
	s, _ := a["text"].(string)
	return "echo:" + s, nil
}

func TestToolRegistrationRules(t *testing.T) {
	app := New("t", "1")
	if app.Tool(Tool{Description: "d", Handler: echo}) == nil {
		t.Fatal("empty name accepted")
	}
	if app.Tool(Tool{Name: "a", Description: "d"}) == nil {
		t.Fatal("missing handler accepted")
	}
	if app.Tool(Tool{Name: "a", Handler: echo}) == nil {
		t.Fatal("missing description accepted: agents pick tools by reading it")
	}
	if err := app.Tool(Tool{Name: "a", Description: "d", Handler: echo}); err != nil {
		t.Fatal(err)
	}
	if app.Tool(Tool{Name: "a", Description: "d2", Handler: echo}) == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestUpstreamListsSortedAndCalls(t *testing.T) {
	app := New("t", "1")
	for _, n := range []string{"zeta", "alpha", "mid"} {
		if err := app.Tool(Tool{Name: n, Description: n + " tool", Handler: echo}); err != nil {
			t.Fatal(err)
		}
	}
	up := app.Upstream()
	tools, _ := up.ListTools(context.Background())
	if len(tools) != 3 || tools[0].Name != "alpha" || tools[2].Name != "zeta" {
		t.Fatalf("order: %+v", tools)
	}
	if tools[0].InputSchema == nil {
		t.Fatal("default schema missing")
	}
	res, err := up.CallTool(context.Background(), "alpha", map[string]any{"text": "hi"})
	if err != nil || res.IsError || res.Content[0].Text != "echo:hi" {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := up.CallTool(context.Background(), "nope", nil); err == nil {
		t.Fatal("unknown tool must error")
	}
}

func TestHandlerErrorsAndPanicsBecomeToolErrors(t *testing.T) {
	app := New("t", "1")
	_ = app.Tool(Tool{Name: "fail", Description: "d", Handler: func(context.Context, map[string]any) (string, error) {
		return "", errors.New("no such note")
	}})
	_ = app.Tool(Tool{Name: "boom", Description: "d", Handler: func(context.Context, map[string]any) (string, error) {
		panic("bad index")
	}})
	up := app.Upstream()
	r, err := up.CallTool(context.Background(), "fail", nil)
	if err != nil || !r.IsError || r.Content[0].Text != "no such note" {
		t.Fatalf("%+v %v", r, err)
	}
	r, err = up.CallTool(context.Background(), "boom", nil)
	if err != nil || !r.IsError || !strings.Contains(r.Content[0].Text, "bad index") {
		t.Fatalf("panic must not escape: %+v %v", r, err)
	}
}

// The point of the library: a hand-built server gets the overlay machinery for free.
func TestLibraryServerGetsOverlayThroughTheRuntime(t *testing.T) {
	app := New("notes", "1")
	_ = app.Tool(Tool{Name: "read_note", Description: "Read a note.", Handler: echo})
	_ = app.Tool(Tool{Name: "read_notes", Description: "Read many notes.", Handler: echo})

	p := mcp.NewProxy(app.Upstream())
	desc := "Read ONE note by id. For several, use read_notes."
	p.Overlay = improvementsandbox.Overlay{
		ToolPatches: map[string]improvementsandbox.ToolPatch{"read_note": {Description: &desc}},
		Routing:     []improvementsandbox.RouteRule{{FromTool: "read_note", ToTool: "read_notes"}},
	}
	if err := p.RegisterUpstreamTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ct, st := mcpsdk.NewInMemoryTransports()
	if _, err := p.Server.SDK().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	sess, err := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "c", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, _ := sess.ListTools(ctx, nil)
	got := map[string]string{}
	for _, tool := range list.Tools {
		got[tool.Name] = tool.Description
	}
	if got["read_note"] != desc {
		t.Fatalf("patched description not served: %v", got)
	}
	res, err := sess.CallTool(ctx, &mcpsdk.CallToolParams{Name: "read_note", Arguments: map[string]any{"text": "x"}})
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
}
