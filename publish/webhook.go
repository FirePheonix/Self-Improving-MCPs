package publish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	selectiongate "self-improving-mcp/selection-gate"
)

// WebhookPayload is what is POSTed. "text" makes it work as a Slack incoming webhook as is;
// the structured fields serve anything else (n8n, Zapier, your own endpoint).
//
// It carries counts, the verdict and the reason only. Task queries and tool descriptions stay
// on the machine: a webhook URL is a bearer secret that often ends up in third-party logs.
type WebhookPayload struct {
	Text      string  `json:"text"`
	Verdict   string  `json:"verdict"`
	VersionID string  `json:"version_id"`
	Reason    string  `json:"reason"`
	SealedN   int     `json:"sealed_n"`
	Baseline  int     `json:"baseline_correct"`
	Candidate int     `json:"candidate_correct"`
	Fixed     int     `json:"fixed"`
	Broken    int     `json:"broken"`
	PValue    float64 `json:"p_value"`
}

// BuildWebhookPayload summarizes a gate decision for a chat or automation endpoint.
func BuildWebhookPayload(rep selectiongate.Report) WebhookPayload {
	d := rep.Decision
	id := ShortID(rep.VersionID)
	var head string
	switch d.Verdict {
	case selectiongate.VerdictPass:
		head = fmt.Sprintf("Overlay %s PASSED the selection gate and is ready for review", id)
	case selectiongate.VerdictFail:
		head = fmt.Sprintf("Overlay %s FAILED the selection gate and was not shipped", id)
	default:
		head = fmt.Sprintf("Overlay %s was inconclusive and was not shipped", id)
	}
	return WebhookPayload{
		Text: fmt.Sprintf("%s: %s (%d -> %d of %d sealed tasks, fixed %d, broke %d, p=%.4f)",
			head, d.Reason, d.BaselineCorrect, d.CandidateCorrect, d.SealedN, d.Fixed, d.Broken, d.PValue),
		Verdict: string(d.Verdict), VersionID: rep.VersionID, Reason: d.Reason,
		SealedN: d.SealedN, Baseline: d.BaselineCorrect, Candidate: d.CandidateCorrect,
		Fixed: d.Fixed, Broken: d.Broken, PValue: d.PValue,
	}
}

// SendWebhook POSTs the payload as JSON. Only http(s) URLs are accepted. Redirects are not
// followed (a redirect could send the payload somewhere the operator never configured).
// Error text never includes the URL, since the URL is the secret.
func SendWebhook(ctx context.Context, rawURL string, p WebhookPayload) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("webhook: not a valid http(s) url")
	}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: build request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: request failed (%s)", errKind(err))
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("webhook: endpoint answered HTTP %d", res.StatusCode)
	}
	return nil
}

func errKind(err error) string {
	if ue, ok := err.(*url.Error); ok && ue.Timeout() {
		return "timeout"
	}
	return "network error"
}
