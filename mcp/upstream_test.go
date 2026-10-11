package mcp

import (
	"context"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestUpstreamListAndCallAgainstFixture(t *testing.T) {
	ctx := context.Background()

	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fixture", Version: "v1.0.0"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "echo",
		Description: "echo text back",
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in struct {
		Text string `json:"text"`
	}) (*mcpsdk.CallToolResult, any, error) {
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: in.Text}},
		}, nil, nil
	})

	t1, t2 := mcpsdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v1.0.0"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}

	u := &StdioUpstream{session: cs}
	t.Cleanup(func() { _ = u.Close() })

	tools, err := u.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", tools)
	}

	res, err := u.CallTool(ctx, "echo", map[string]any{"text": "ping"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError || len(res.Content) != 1 || res.Content[0].Text != "ping" {
		t.Fatalf("result = %+v", res)
	}
}
