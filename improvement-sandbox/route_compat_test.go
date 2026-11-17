package improvementsandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func schema(required []string, props map[string]string) map[string]any {
	p := map[string]any{}
	for n, t := range props {
		pm := map[string]any{}
		if t != "" {
			pm["type"] = t
		}
		p[n] = pm
	}
	req := make([]any, len(required))
	for i, r := range required {
		req[i] = r
	}
	return map[string]any{"type": "object", "properties": p, "required": req}
}

func route(from, to string) Overlay {
	return Overlay{Routing: []RouteRule{{FromTool: from, ToTool: to}}}
}

func TestTheRealFailureIsCaught(t *testing.T) {
	schemas := map[string]any{
		"read_text_file":      schema([]string{"path"}, map[string]string{"path": "string", "head": "number", "tail": "number"}),
		"read_multiple_files": schema([]string{"paths"}, map[string]string{"paths": "array"}),
	}
	got := CheckRoutes(route("read_text_file", "read_multiple_files"), schemas)
	if len(got) != 1 {
		t.Fatalf("want 1 problem, got %v", got)
	}
	s := got[0].String()
	for _, must := range []string{"read_text_file -> read_multiple_files", `requires "paths"`, `source requires "path"`} {
		if !strings.Contains(s, must) {
			t.Errorf("message %q missing %q", s, must)
		}
	}
}

func TestAliasesAndSkippedCasesAreNotFlagged(t *testing.T) {
	same := map[string]any{
		"read_file":      schema([]string{"path"}, map[string]string{"path": "string"}),
		"read_text_file": schema([]string{"path"}, map[string]string{"path": "string", "head": "number"}),
	}
	if p := CheckRoutes(route("read_file", "read_text_file"), same); len(p) != 0 {
		t.Fatalf("a genuine alias must be allowed: %v", p)
	}
	if p := CheckRoutes(route("a", "b"), map[string]any{}); len(p) != 0 {
		t.Fatalf("unknown schemas must not be guessed at: %v", p)
	}
	if p := CheckRoutes(Overlay{Routing: []RouteRule{{FromTool: "a", Deny: true}}}, same); len(p) != 0 {
		t.Fatalf("deny rules are not routes: %v", p)
	}
	composed := Overlay{
		Routing:       []RouteRule{{FromTool: "read_file", ToTool: "both"}},
		ComposedTools: map[string]ComposedTool{"both": {Description: "d", Steps: []ComposedStep{{Tool: "read_file"}}}},
	}
	if p := CheckRoutes(composed, same); len(p) != 0 {
		t.Fatalf("routes to composed tools are out of scope: %v", p)
	}
}

func TestTypeMismatchOnSharedRequiredArgument(t *testing.T) {
	schemas := map[string]any{
		"a": schema([]string{"id"}, map[string]string{"id": "string"}),
		"b": schema([]string{"id"}, map[string]string{"id": "number"}),
	}
	p := CheckRoutes(route("a", "b"), schemas)
	if len(p) != 1 || !strings.Contains(p[0].Reason, `"id" is string on the source but number on the target`) {
		t.Fatalf("%v", p)
	}
}

func TestSchemasMayBeRawJSONOrAnyMarshalableValue(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
	other := json.RawMessage(`{"type":"object","properties":{"paths":{"type":"array"}},"required":["paths"]}`)
	if p := CheckRoutes(route("a", "b"), map[string]any{"a": raw, "b": other}); len(p) != 1 {
		t.Fatalf("raw json schemas must be understood: %v", p)
	}
	if p := CheckRoutes(route("a", "b"), map[string]any{"a": "not a schema", "b": other}); len(p) != 0 {
		t.Fatalf("an unparsable schema must be skipped, not flagged: %v", p)
	}
}
