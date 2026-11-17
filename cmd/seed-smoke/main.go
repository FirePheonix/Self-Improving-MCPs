package main

import (
	"fmt"
	"path/filepath"

	"self-improving-mcp/store"
)

// Seeds a synthetic bad trajectory so evaluate has something to detect.
func main() {
	db, err := store.Open(filepath.Join("data", "trajectories.db"))
	if err != nil {
		panic(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		panic(err)
	}
	s, err := db.CreateSession("smoke-fixture")
	if err != nil {
		panic(err)
	}
	calls := []store.ToolCall{
		{SessionID: s.ID, Seq: 1, ToolName: "fetch", ArgsJSON: `{"url":"a"}`, ArgsHash: "h1", LatencyMs: 10, IsError: true, ErrorType: "tool_error"},
		{SessionID: s.ID, Seq: 2, ToolName: "fetch", ArgsJSON: `{"url":"a"}`, ArgsHash: "h1", LatencyMs: 12, IsError: true, ErrorType: "tool_error"},
		{SessionID: s.ID, Seq: 3, ToolName: "fetch", ArgsJSON: `{"url":"a"}`, ArgsHash: "h1", LatencyMs: 11, IsError: true, ErrorType: "tool_error"},
		{SessionID: s.ID, Seq: 4, ToolName: "fetch", ArgsJSON: `{"url":"a"}`, ArgsHash: "h1", LatencyMs: 13, IsError: true, ErrorType: "tool_error"},
		{SessionID: s.ID, Seq: 5, ToolName: "read", ArgsJSON: `{"path":"x"}`, ArgsHash: "r1", LatencyMs: 10},
		{SessionID: s.ID, Seq: 6, ToolName: "read", ArgsJSON: `{"path":"x"}`, ArgsHash: "r1", LatencyMs: 10},
		{SessionID: s.ID, Seq: 7, ToolName: "a", ArgsHash: "x", LatencyMs: 5},
		{SessionID: s.ID, Seq: 8, ToolName: "b", ArgsHash: "y", LatencyMs: 5},
		{SessionID: s.ID, Seq: 9, ToolName: "a", ArgsHash: "x", LatencyMs: 5},
		{SessionID: s.ID, Seq: 10, ToolName: "b", ArgsHash: "y", LatencyMs: 5},
		{SessionID: s.ID, Seq: 11, ToolName: "a", ArgsHash: "x", LatencyMs: 5},
		{SessionID: s.ID, Seq: 12, ToolName: "b", ArgsHash: "y", LatencyMs: 5},
	}
	for _, c := range calls {
		if _, err := db.InsertToolCall(c); err != nil {
			panic(err)
		}
	}
	fmt.Println("seeded session", s.ID, "with", len(calls), "calls")
}
