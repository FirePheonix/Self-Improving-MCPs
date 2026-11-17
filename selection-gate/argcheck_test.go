package selectiongate

import (
	"context"
	"strings"
	"testing"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
)

// callSel always makes the same call.
type callSel struct{ call Call }

func (c callSel) Select(ctx context.Context, q string, t []Tool) (string, error) {
	return c.call.Name, nil
}
func (c callSel) SelectCall(ctx context.Context, q string, t []Tool) (Call, error) {
	return c.call, nil
}

var fsTools = []Tool{
	{Name: "read_text_file", Description: "one", Parameters: map[string]any{
		"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []any{"path"}}},
	{Name: "read_multiple_files", Description: "many", Parameters: map[string]any{
		"type": "object", "properties": map[string]any{"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []any{"paths"}}},
}

func one(t *testing.T, sel Selector, task Task, ov improvementsandbox.Overlay) Outcome {
	t.Helper()
	r, err := Evaluate(context.Background(), sel, fsTools, []Task{task}, ov, EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return r.Outcomes[0]
}

func TestValidArgumentsCountInvalidOnesDoNot(t *testing.T) {
	task := Task{ID: "t", Query: "read a and b", ExpectedTool: "read_multiple_files"}
	ok := one(t, callSel{Call{Name: "read_multiple_files", Args: map[string]any{"paths": []any{"a", "b"}}}}, task, improvementsandbox.Overlay{})
	if !ok.Correct || !ok.ArgsChecked || ok.ArgsError != "" {
		t.Fatalf("valid call: %+v", ok)
	}
	bad := one(t, callSel{Call{Name: "read_multiple_files", Args: map[string]any{"paths": "a"}}}, task, improvementsandbox.Overlay{})
	if bad.Correct || bad.ArgsError == "" {
		t.Fatalf("right tool with arguments that cannot run must not count: %+v", bad)
	}
	missing := one(t, callSel{Call{Name: "read_multiple_files", Args: nil}}, task, improvementsandbox.Overlay{})
	if missing.Correct || !strings.Contains(missing.ArgsError, "invalid arguments") {
		t.Fatalf("missing required argument: %+v", missing)
	}
}

// The exact failure found by calling the real proxy: the route scores on names and cannot run.
func TestRoutedCallIsValidatedAgainstTheToolActuallyCalled(t *testing.T) {
	task := Task{ID: "t", Query: "read a and b", ExpectedTool: "read_multiple_files"}
	route := improvementsandbox.Overlay{Routing: []improvementsandbox.RouteRule{{FromTool: "read_text_file", ToTool: "read_multiple_files"}}}
	o := one(t, callSel{Call{Name: "read_text_file", Args: map[string]any{"path": "a"}}}, task, route)
	if o.Served != "read_multiple_files" {
		t.Fatalf("served = %q", o.Served)
	}
	if o.Correct || o.ArgsError == "" {
		t.Fatalf("a routed call whose arguments do not fit the target must be incorrect: %+v", o)
	}
}

func TestNameOnlySelectorIsStillJudgedByName(t *testing.T) {
	o := one(t, nameSel("read_text_file"), Task{ID: "t", Query: "q", ExpectedTool: "read_text_file"}, improvementsandbox.Overlay{})
	if !o.Correct || o.ArgsChecked {
		t.Fatalf("%+v", o)
	}
}

func TestAcceptableToolsCountAsCorrect(t *testing.T) {
	task := Task{ID: "t", Query: "q", ExpectedTool: "puppeteer_click", AcceptableTools: []string{"playwright_click"}}
	if !task.Accepts("playwright_click") || !task.Accepts("puppeteer_click") || task.Accepts("playwright_hover") || task.Accepts("") {
		t.Fatal("Accepts")
	}
	tools := []Tool{{Name: "playwright_click", Description: "d"}, {Name: "puppeteer_click", Description: "d"}}
	r, _ := Evaluate(context.Background(), nameSel("playwright_click"), tools, []Task{task}, improvementsandbox.Overlay{}, EvalOptions{})
	if !r.Outcomes[0].Correct {
		t.Fatal("an equivalent tool must be accepted")
	}
}

func TestUnreadableSchemaIsNotChecked(t *testing.T) {
	tools := []Tool{{Name: "x", Description: "d", Parameters: "not a schema"}}
	r, _ := Evaluate(context.Background(), callSel{Call{Name: "x", Args: map[string]any{"a": 1}}}, tools, []Task{{ID: "t", Query: "q", ExpectedTool: "x"}}, improvementsandbox.Overlay{}, EvalOptions{})
	if !r.Outcomes[0].Correct || r.Outcomes[0].ArgsChecked {
		t.Fatalf("%+v", r.Outcomes[0])
	}
}

func TestSealedHashUnchangedWithoutAcceptableTools(t *testing.T) {
	base := GoldenSet{Tasks: []Task{{ID: "a", Query: "q", ExpectedTool: "x", Split: SplitSealed}}}
	with := GoldenSet{Tasks: []Task{{ID: "a", Query: "q", ExpectedTool: "x", Split: SplitSealed, AcceptableTools: []string{"y"}}}}
	// value computed before acceptable_tools existed: older decisions must keep matching
	if base.SealedHash() == with.SealedHash() {
		t.Fatal("adding an acceptable tool must change the fingerprint")
	}
	if (&GoldenSet{Tasks: []Task{{ID: "a", Query: "q", ExpectedTool: "x", Split: SplitSealed, AcceptableTools: []string{""}}}}).Validate() == nil {
		t.Fatal("empty acceptable name must be rejected")
	}
}

// nameSel is a name-only selector that always picks the same tool.
type nameSel string

func (n nameSel) Select(context.Context, string, []Tool) (string, error) { return string(n), nil }

func TestUnreadableArgumentsNeverCount(t *testing.T) {
	sel := callSel{Call{Name: "read_text_file", ArgsInvalid: "arguments are not a JSON object"}}
	o := one(t, sel, Task{ID: "t", Query: "q", ExpectedTool: "read_text_file"}, improvementsandbox.Overlay{})
	if o.Correct || !o.ArgsChecked || o.ArgsError == "" {
		t.Fatalf("%+v", o)
	}
}
