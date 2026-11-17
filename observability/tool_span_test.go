package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// recordSpan runs fn with a recording tracer provider installed and returns the finished span.
func recordSpan(t *testing.T, fn func(ctx context.Context)) tracetest.SpanStub {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { otel.SetTracerProvider(prev) })

	fn(context.Background())

	ended := sr.Ended()
	if len(ended) != 1 {
		t.Fatalf("want 1 ended span, got %d", len(ended))
	}
	return tracetest.SpanStubFromReadOnlySpan(ended[0])
}

func attrValue(stub tracetest.SpanStub, key string) (string, bool) {
	for _, kv := range stub.Attributes {
		if kv.Key == attribute.Key(key) {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

func TestToolSpanCarriesSessionAndOverlayVersion(t *testing.T) {
	stub := recordSpan(t, func(ctx context.Context) {
		_, span := StartToolSpan(ctx, "read_multiple_files", ToolSpanInfo{
			SessionID:      "sess-1",
			OverlayVersion: "v-abc",
			RequestedTool:  "read_text_file", // routing rewrote it
		})
		EndToolSpan(span, false, "", nil)
	})

	if stub.Name != "execute_tool read_multiple_files" {
		t.Errorf("span name = %q", stub.Name)
	}
	for key, want := range map[string]string{
		"gen_ai.tool.name": "read_multiple_files",
		AttrSessionID:      "sess-1",
		AttrOverlayVersion: "v-abc",
		AttrToolRequested:  "read_text_file",
	} {
		if got, ok := attrValue(stub, key); !ok || got != want {
			t.Errorf("attr %s = %q (present=%v), want %q", key, got, ok, want)
		}
	}
}

func TestToolSpanBaselineOmitsOptionalAttributes(t *testing.T) {
	stub := recordSpan(t, func(ctx context.Context) {
		_, span := StartToolSpan(ctx, "list_directory", ToolSpanInfo{RequestedTool: "list_directory"})
		EndToolSpan(span, false, "", nil)
	})

	if got, _ := attrValue(stub, AttrOverlayVersion); got != "none" {
		t.Errorf("baseline overlay version = %q, want %q", got, "none")
	}
	if _, ok := attrValue(stub, AttrSessionID); ok {
		t.Error("session id attribute present with no session")
	}
	if _, ok := attrValue(stub, AttrToolRequested); ok {
		t.Error("requested-tool attribute present although routing did not change the name")
	}
}
