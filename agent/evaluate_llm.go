package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"self-improving-mcp/store"
)

// LabelFindings asks the cheap model to add short labels/categories to findings.
// Returns newly inserted findings (does not mutate originals).
func LabelFindings(ctx context.Context, client *Client, db *store.DB, findings []store.Finding) ([]store.Finding, error) {
	if client == nil || len(findings) == 0 {
		return nil, nil
	}

	type row struct {
		ID       int64  `json:"id"`
		Category string `json:"category"`
		Title    string `json:"title"`
		Detail   string `json:"detail"`
	}
	payload := make([]row, 0, len(findings))
	for _, f := range findings {
		payload = append(payload, row{ID: f.ID, Category: f.Category, Title: f.Title, Detail: f.Detail})
	}
	raw, _ := json.Marshal(payload)

	system := `You label MCP trajectory findings. Reply with JSON array only:
[{"id":1,"suggested_category":"schema|routing|cache|compose|impl","note":"short"}]
Use only those categories. Keep notes under 20 words.`
	user := string(raw)

	text, err := client.Complete(ctx, client.Label, system, user)
	if err != nil {
		return nil, err
	}
	text = extractJSONArray(text)

	var labels []struct {
		ID                int64  `json:"id"`
		SuggestedCategory string `json:"suggested_category"`
		Note              string `json:"note"`
	}
	if err := json.Unmarshal([]byte(text), &labels); err != nil {
		return nil, fmt.Errorf("parse label json: %w (body=%q)", err, text)
	}

	var out []store.Finding
	for _, lab := range labels {
		f, err := db.InsertFinding(store.Finding{
			Category:     "llm_label",
			Severity:     "info",
			Title:        fmt.Sprintf("label for finding %d → %s", lab.ID, lab.SuggestedCategory),
			Detail:       lab.Note,
			EvidenceJSON: fmt.Sprintf(`{"source_finding_id":%d,"suggested_category":%q}`, lab.ID, lab.SuggestedCategory),
		})
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func extractJSONArray(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "["); i >= 0 {
		if j := strings.LastIndex(s, "]"); j > i {
			return s[i : j+1]
		}
	}
	return s
}
