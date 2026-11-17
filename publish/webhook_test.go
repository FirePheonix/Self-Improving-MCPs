package publish

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	selectiongate "self-improving-mcp/selection-gate"
)

func whReport(v selectiongate.Verdict) selectiongate.Report {
	return selectiongate.Report{
		VersionID: "cd4f4dc3-38c4-4115-a67b-e4195b499a9b",
		Decision: selectiongate.Decision{Verdict: v, Reason: "because", SealedN: 73,
			BaselineCorrect: 65, CandidateCorrect: 72, Fixed: 7, PValue: 0.0078},
		Changes: []selectiongate.Change{{Query: "SECRET QUERY TEXT", Expected: "a"}},
	}
}

func TestPayloadWordingPerVerdictAndNoTaskText(t *testing.T) {
	for v, want := range map[selectiongate.Verdict]string{
		selectiongate.VerdictPass: "PASSED", selectiongate.VerdictFail: "FAILED", selectiongate.VerdictInconclusive: "inconclusive",
	} {
		p := BuildWebhookPayload(whReport(v))
		if !strings.Contains(p.Text, want) || !strings.Contains(p.Text, "cd4f4dc3") {
			t.Fatalf("%s: %q", v, p.Text)
		}
		raw, _ := json.Marshal(p)
		if strings.Contains(string(raw), "SECRET QUERY TEXT") {
			t.Fatal("task queries must not leave the machine")
		}
	}
}

func TestSendWebhookPostsJSON(t *testing.T) {
	var got WebhookPayload
	var ctype string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctype = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	if err := SendWebhook(context.Background(), srv.URL, BuildWebhookPayload(whReport(selectiongate.VerdictPass))); err != nil {
		t.Fatal(err)
	}
	if ctype != "application/json" || got.Verdict != "pass" || got.Fixed != 7 || got.Text == "" {
		t.Fatalf("%s %+v", ctype, got)
	}
}

func TestSendWebhookFailuresDoNotLeakTheURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	url := srv.URL + "/services/T000/B000/SECRETTOKEN"
	err := SendWebhook(context.Background(), url, WebhookPayload{})
	if err == nil || !strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), "SECRETTOKEN") {
		t.Fatalf("err = %v", err)
	}
	srv.Close()
	err = SendWebhook(context.Background(), url, WebhookPayload{})
	if err == nil || strings.Contains(err.Error(), "SECRETTOKEN") {
		t.Fatalf("network error must not leak the url: %v", err)
	}
	for _, bad := range []string{"", "ftp://x/y", "not a url", "file:///etc/passwd", "http://"} {
		if SendWebhook(context.Background(), bad, WebhookPayload{}) == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
}

func TestSendWebhookDoesNotFollowRedirects(t *testing.T) {
	hit := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()
	if err := SendWebhook(context.Background(), srv.URL, WebhookPayload{}); err == nil {
		t.Fatal("a redirect is not a success")
	}
	if hit {
		t.Fatal("redirect was followed")
	}
}
