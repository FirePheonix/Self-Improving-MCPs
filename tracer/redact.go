package tracer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

const defaultMaxLen = 2048

var secretKeyPattern = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|authorization|bearer)`)

// RedactPayload JSON-encodes args/results, strips likely secrets, and truncates.
func RedactPayload(v any, maxLen int) (jsonStr string, hash string) {
	if maxLen <= 0 {
		maxLen = defaultMaxLen
	}
	raw, err := json.Marshal(v)
	if err != nil {
		raw = []byte(`{"_error":"marshal_failed"}`)
	}

	var data any
	if err := json.Unmarshal(raw, &data); err == nil {
		data = redactValue(data)
		if rewritten, err := json.Marshal(data); err == nil {
			raw = rewritten
		}
	}

	sum := sha256.Sum256(raw)
	hash = hex.EncodeToString(sum[:])

	s := string(raw)
	if len(s) > maxLen {
		s = s[:maxLen] + "…(truncated)"
	}
	return s, hash
}

// SummarizeResult makes a short text summary from content strings.
func SummarizeResult(texts []string, isError bool, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 512
	}
	joined := strings.Join(texts, "\n")
	if isError && joined == "" {
		joined = "error"
	}
	if len(joined) > maxLen {
		joined = joined[:maxLen] + "…(truncated)"
	}
	return joined
}

func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if secretKeyPattern.MatchString(k) {
				out[k] = "[REDACTED]"
				continue
			}
			out[k] = redactValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = redactValue(val)
		}
		return out
	default:
		return v
	}
}
