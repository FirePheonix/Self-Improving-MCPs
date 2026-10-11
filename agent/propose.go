package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/store"
)

// ProposeOverlays asks the reason model to draft overlays from findings and stores candidate versions.
func ProposeOverlays(ctx context.Context, client *Client, db *store.DB, findings []store.Finding) ([]store.Version, error) {
	if client == nil {
		return nil, fmt.Errorf("openai client is nil")
	}
	if len(findings) == 0 {
		return nil, nil
	}

	type row struct {
		Category string `json:"category"`
		Title    string `json:"title"`
		Detail   string `json:"detail"`
	}
	payload := make([]row, 0, len(findings))
	for _, f := range findings {
		payload = append(payload, row{Category: f.Category, Title: f.Title, Detail: f.Detail})
	}
	raw, _ := json.Marshal(payload)

	system := `You propose MCP proxy overlays from trajectory findings.
Reply with a single JSON object only matching:
{"tool_patches":{"toolName":{"description":"..."}},"routing":[{"from_tool":"a","to_tool":"b"}],"cache":[{"tool_name":"x","ttl_seconds":60}],"composed_tools":{"name":{"description":"...","steps":[{"tool":"a","args":{}}]}}}
Only include fields that help. Prefer better descriptions and composed tools for frequent sequences.`
	text, err := client.Complete(ctx, client.Reason, system, string(raw))
	if err != nil {
		return nil, err
	}
	text = extractJSONObject(text)

	var overlay improvementsandbox.Overlay
	if err := json.Unmarshal([]byte(text), &overlay); err != nil {
		return nil, fmt.Errorf("parse overlay: %w (body=%q)", err, text)
	}
	if err := improvementsandbox.Validate(overlay); err != nil {
		return nil, err
	}
	overlayJSON, err := json.Marshal(overlay)
	if err != nil {
		return nil, err
	}

	parent, _ := db.ActiveVersionID()
	v, err := db.InsertVersion(parent, string(overlayJSON), "proposed from findings")
	if err != nil {
		return nil, err
	}
	return []store.Version{v}, nil
}

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}
