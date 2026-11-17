// A tiny notes MCP server built with the simcp library.
//
//	go build -o notes.exe ./examples/notes
//	notes.exe                       # traced, overlay-aware, hot-reloading (data in ./data)
//	SIMCP_RAW=1 notes.exe           # raw handlers only (what evolve reads as the upstream)
//
// Set NOTES_STATUS_ADDR=127.0.0.1:8787 to serve /health and /overlay.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"

	"self-improving-mcp/config"
	"self-improving-mcp/simcp"
)

func main() {
	var mu sync.Mutex
	notes := map[string]string{
		"todo":    "buy milk; write report",
		"ideas":   "self-improving MCP servers",
		"meeting": "Tuesday 10:00, roadmap review",
	}
	app := simcp.New("notes", "1.0.0")

	must := func(err error) {
		if err != nil {
			log.Fatal(err)
		}
	}
	must(app.Tool(simcp.Tool{
		Name:        "read_note",
		Description: "Read one note by its id.",
		InputSchema: obj(map[string]any{"id": str("note id")}, "id"),
		Handler: func(_ context.Context, a map[string]any) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			id, _ := a["id"].(string)
			if n, ok := notes[id]; ok {
				return n, nil
			}
			return "", fmt.Errorf("no note with id %q", id)
		},
	}))
	must(app.Tool(simcp.Tool{
		Name:        "list_notes",
		Description: "List the ids of all notes.",
		Handler: func(context.Context, map[string]any) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			ids := make([]string, 0, len(notes))
			for id := range notes {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			return strings.Join(ids, "\n"), nil
		},
	}))
	must(app.Tool(simcp.Tool{
		Name:        "search_notes",
		Description: "Find notes whose text contains a keyword.",
		InputSchema: obj(map[string]any{"keyword": str("word to look for")}, "keyword"),
		Handler: func(_ context.Context, a map[string]any) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			kw, _ := a["keyword"].(string)
			var hits []string
			for id, text := range notes {
				if strings.Contains(strings.ToLower(text), strings.ToLower(kw)) {
					hits = append(hits, id)
				}
			}
			sort.Strings(hits)
			return strings.Join(hits, "\n"), nil
		},
	}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	cfg := config.ProxyConfig{DataDir: "data", RedactPayloads: true, StatusAddr: os.Getenv("NOTES_STATUS_ADDR")}
	if err := app.Serve(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func obj(props map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required}
}
