package improvementsandbox

import "testing"

func TestResolveRoute(t *testing.T) {
	o := Overlay{Routing: []RouteRule{
		{FromTool: "read_text_file", ToTool: "read_multiple_files", Note: "batch"},
		{FromTool: "delete_all", Deny: true, Note: "unsafe"},
		{FromTool: "noop"}, // matches but neither denies nor routes: skipped
		{FromTool: "noop", ToTool: "real"},
	}}

	cases := []struct {
		in         string
		wantTool   string
		wantDenied bool
		wantNote   string
	}{
		{"read_text_file", "read_multiple_files", false, "batch"},
		{"delete_all", "delete_all", true, "unsafe"},
		{"noop", "real", false, ""},
		{"untouched", "untouched", false, ""},
	}
	for _, c := range cases {
		tool, denied, note := o.ResolveRoute(c.in)
		if tool != c.wantTool || denied != c.wantDenied || note != c.wantNote {
			t.Errorf("ResolveRoute(%q) = (%q,%v,%q), want (%q,%v,%q)",
				c.in, tool, denied, note, c.wantTool, c.wantDenied, c.wantNote)
		}
	}
}

func TestResolveRouteEmptyOverlay(t *testing.T) {
	tool, denied, _ := Overlay{}.ResolveRoute("anything")
	if tool != "anything" || denied {
		t.Fatalf("empty overlay should pass through, got (%q,%v)", tool, denied)
	}
}
