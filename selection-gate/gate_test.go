package selectiongate

import (
	"context"
	"errors"
	"fmt"
	"testing"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
)

// fakeSelector answers from a fixed map; unknown queries error.
type fakeSelector struct {
	picks map[string]string
	errOn map[string]bool
}

func (f fakeSelector) Select(_ context.Context, q string, _ []Tool) (string, error) {
	if f.errOn[q] {
		return "", errors.New("boom")
	}
	p, ok := f.picks[q]
	if !ok {
		return "", fmt.Errorf("no pick for %q", q)
	}
	return p, nil
}

func TestEvaluateAppliesRoutingToThePick(t *testing.T) {
	sel := fakeSelector{picks: map[string]string{"read both": "read_text_file"}}
	tasks := []Task{{ID: "1", Query: "read both", ExpectedTool: "read_multiple_files"}}

	base, err := Evaluate(context.Background(), sel, nil, tasks, improvementsandbox.Overlay{}, EvalOptions{})
	if err != nil || base.Outcomes[0].Correct {
		t.Fatalf("baseline should be wrong: %v %+v", err, base.Outcomes)
	}

	routed := improvementsandbox.Overlay{Routing: []improvementsandbox.RouteRule{
		{FromTool: "read_text_file", ToTool: "read_multiple_files"},
	}}
	cand, err := Evaluate(context.Background(), sel, nil, tasks, routed, EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	o := cand.Outcomes[0]
	if !o.Correct || o.Picked != "read_text_file" || o.Served != "read_multiple_files" {
		t.Fatalf("routing not applied to the pick: %+v", o)
	}
}

func TestEvaluateDeniedPickIsIncorrect(t *testing.T) {
	sel := fakeSelector{picks: map[string]string{"q": "danger"}}
	tasks := []Task{{ID: "1", Query: "q", ExpectedTool: "danger"}}
	ov := improvementsandbox.Overlay{Routing: []improvementsandbox.RouteRule{{FromTool: "danger", Deny: true}}}
	run, _ := Evaluate(context.Background(), sel, nil, tasks, ov, EvalOptions{})
	if run.Outcomes[0].Correct || !run.Outcomes[0].Denied {
		t.Fatalf("denied pick must not count as correct: %+v", run.Outcomes[0])
	}
}

func TestEvaluateSelectorErrorCountsAsWrongAndIsRecorded(t *testing.T) {
	sel := fakeSelector{errOn: map[string]bool{"q": true}}
	run, err := Evaluate(context.Background(), sel, nil, []Task{{ID: "1", Query: "q", ExpectedTool: "x"}}, improvementsandbox.Overlay{}, EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if run.Outcomes[0].Correct || run.Outcomes[0].Err == "" {
		t.Fatalf("selector error should be wrong and recorded: %+v", run.Outcomes[0])
	}
}

func TestEvaluateKeepsTaskOrderUnderConcurrency(t *testing.T) {
	const n = 200
	picks := map[string]string{}
	tasks := make([]Task, n)
	for i := range tasks {
		q := fmt.Sprintf("q%d", i)
		picks[q] = fmt.Sprintf("tool%d", i)
		tasks[i] = Task{ID: fmt.Sprintf("t%d", i), Query: q, ExpectedTool: fmt.Sprintf("tool%d", i)}
	}
	run, err := Evaluate(context.Background(), fakeSelector{picks: picks}, nil, tasks, improvementsandbox.Overlay{}, EvalOptions{Concurrency: 16})
	if err != nil {
		t.Fatal(err)
	}
	for i, o := range run.Outcomes {
		if o.TaskID != fmt.Sprintf("t%d", i) || !o.Correct {
			t.Fatalf("outcome %d out of order or wrong: %+v", i, o)
		}
	}
}

func TestEvaluateRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Evaluate(ctx, fakeSelector{}, nil, []Task{{ID: "1", Query: "q"}}, improvementsandbox.Overlay{}, EvalOptions{})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestEvaluateNilSelector(t *testing.T) {
	if _, err := Evaluate(context.Background(), nil, nil, nil, improvementsandbox.Overlay{}, EvalOptions{}); err == nil {
		t.Fatal("expected error")
	}
}
