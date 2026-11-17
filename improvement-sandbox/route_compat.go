package improvementsandbox

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// A routing rule rewrites the tool NAME of a call. It does not rewrite the arguments. So a route
// is only usable when the target tool can accept the arguments the agent built for the source.
//
// Without this check a route can look excellent on a name-matching benchmark and still turn every
// routed call into an error. Observed live: routing read_text_file (needs `path`) to
// read_multiple_files (needs `paths`) scored well on tool selection, and a real routed call failed
// with "Invalid arguments ... expected array, received undefined at paths".

// RouteProblem says why one routing rule cannot work at call time.
type RouteProblem struct {
	From, To, Reason string
}

func (p RouteProblem) String() string {
	return fmt.Sprintf("route %s -> %s cannot work: %s", p.From, p.To, p.Reason)
}

// CheckRoutes returns one problem per routing rule whose target cannot accept the source's
// arguments. schemas maps a tool name to its input JSON Schema (any JSON-serialisable value).
// Rules naming a tool with no known schema, deny rules, and routes to composed tools are skipped:
// other validation covers them and this check never guesses.
//
// A route is compatible when every argument the target requires is one the source accepts, with
// the same type, and every argument the source requires is one the target accepts. Optional
// arguments are not compared: they are rarely supplied, so flagging them would block aliases such
// as read_file -> read_text_file that genuinely work.
func CheckRoutes(o Overlay, schemas map[string]any) []RouteProblem {
	var out []RouteProblem
	for _, r := range o.Routing {
		if r.Deny || r.ToTool == "" {
			continue
		}
		if _, composed := o.ComposedTools[r.ToTool]; composed {
			continue
		}
		from, okFrom := parseSchema(schemas[r.FromTool])
		to, okTo := parseSchema(schemas[r.ToTool])
		if !okFrom || !okTo {
			continue
		}
		if reason := incompatible(from, to); reason != "" {
			out = append(out, RouteProblem{From: r.FromTool, To: r.ToTool, Reason: reason})
		}
	}
	return out
}

type paramSchema struct {
	types    map[string]string // property -> JSON type ("" if unspecified)
	required []string
}

func parseSchema(v any) (paramSchema, bool) {
	if v == nil {
		return paramSchema{}, false
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return paramSchema{}, false
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return paramSchema{}, false
	}
	ps := paramSchema{types: map[string]string{}}
	if props, ok := m["properties"].(map[string]any); ok {
		for name, p := range props {
			t := ""
			if pm, ok := p.(map[string]any); ok {
				if ts, ok := pm["type"].(string); ok {
					t = ts
				}
			}
			ps.types[name] = t
		}
	}
	if req, ok := m["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				ps.required = append(ps.required, s)
			}
		}
	}
	return ps, true
}

func incompatible(from, to paramSchema) string {
	var reasons []string
	var missing []string
	for _, name := range to.required {
		ft, ok := from.types[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if tt := to.types[name]; ft != "" && tt != "" && ft != tt {
			reasons = append(reasons, fmt.Sprintf("argument %q is %s on the source but %s on the target", name, ft, tt))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		reasons = append(reasons, fmt.Sprintf("the target requires %s, which the source tool does not take", quoteAll(missing)))
	}
	var dropped []string
	for _, name := range from.required {
		if _, ok := to.types[name]; !ok {
			dropped = append(dropped, name)
		}
	}
	if len(dropped) > 0 {
		sort.Strings(dropped)
		reasons = append(reasons, fmt.Sprintf("the source requires %s, which the target does not take, so it would be ignored", quoteAll(dropped)))
	}
	return strings.Join(reasons, "; ")
}

func quoteAll(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(q, ", ")
}
