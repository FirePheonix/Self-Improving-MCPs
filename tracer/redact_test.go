package tracer

import (
	"strings"
	"testing"
)

func TestRedactPayloadStripsSecretsAndHashes(t *testing.T) {
	jsonStr, hash := RedactPayload(map[string]any{
		"api_key": "sk-secret",
		"path":    "/tmp/x",
	}, 2048)
	if strings.Contains(jsonStr, "sk-secret") {
		t.Fatalf("secret leaked: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, "[REDACTED]") {
		t.Fatalf("expected redaction: %s", jsonStr)
	}
	if hash == "" {
		t.Fatal("empty hash")
	}
	_, hash2 := RedactPayload(map[string]any{
		"api_key": "sk-secret",
		"path":    "/tmp/x",
	}, 2048)
	if hash != hash2 {
		t.Fatal("hash should be stable for same logical payload after redact")
	}
}

func TestRedactPayloadTruncates(t *testing.T) {
	s, _ := RedactPayload(map[string]any{"text": strings.Repeat("a", 500)}, 32)
	if !strings.Contains(s, "truncated") {
		t.Fatalf("expected truncation: %s", s)
	}
}
