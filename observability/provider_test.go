package observability

import "testing"

// Regression: pinning a semconv schema URL conflicted with resource.Default()
// and made the proxy fail to start whenever OTEL_EXPORTER_OTLP_ENDPOINT was set.
func TestNewResourceMergesWithDefault(t *testing.T) {
	res, err := newResource()
	if err != nil {
		t.Fatalf("newResource: %v", err)
	}
	if res == nil {
		t.Fatal("newResource returned nil resource")
	}
}
