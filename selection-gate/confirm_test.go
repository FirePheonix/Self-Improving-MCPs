package selectiongate

import (
	"context"
	"sync"
	"testing"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
)

// flakySelector answers "wrong" on its first call for a query, then "right" afterwards.
type flakySelector struct {
	mu    *sync.Mutex
	calls map[string]int
	// stableWrong queries are always wrong.
	stableWrong map[string]bool
}

func (f flakySelector) Select(_ context.Context, q string, _ []Tool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[q]++
	if f.stableWrong[q] || f.calls[q] == 1 {
		return "wrong", nil
	}
	return "right", nil
}

func mkRun(tasks []Task, correct ...bool) *Run {
	r := &Run{}
	for i, t := range tasks {
		r.Outcomes = append(r.Outcomes, Outcome{TaskID: t.ID, Expected: t.ExpectedTool, Correct: correct[i]})
	}
	return r
}

func TestConfirmDisputedOutvotesOneOffNoise(t *testing.T) {
	tasks := []Task{{ID: "a", Query: "qa", ExpectedTool: "right"}, {ID: "b", Query: "qb", ExpectedTool: "right"}}
	sel := flakySelector{mu: &sync.Mutex{}, calls: map[string]int{}}
	// Task a: baseline right, candidate wrong -> disputed. The candidate's later votes are right,
	// so the first-call flip must be outvoted. Task b agreed, so it must not be re-asked.
	base, cand := mkRun(tasks, true, true), mkRun(tasks, false, true)
	n, err := ConfirmDisputed(context.Background(), sel, tasks, Arm{}, Arm{}, base, cand, 4, EvalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("disputed = %d, want 1", n)
	}
	if !cand.Outcomes[0].Correct {
		t.Error("one-off flip should have been outvoted")
	}
	if sel.calls["qb"] != 0 {
		t.Errorf("agreed task was re-asked %d times", sel.calls["qb"])
	}
}

func TestConfirmDisputedKeepsRealRegression(t *testing.T) {
	tasks := []Task{{ID: "a", Query: "qa", ExpectedTool: "right"}}
	sel := flakySelector{mu: &sync.Mutex{}, calls: map[string]int{}, stableWrong: map[string]bool{"qa": true}}
	// The selector is consistently wrong on this task; the candidate's earlier "wrong" is real.
	// Baseline claimed right; with the same selector it is outvoted to wrong too, which is fine:
	// what matters is that a consistent difference is not erased. Use distinct arms via overlay.
	base, cand := mkRun(tasks, true), mkRun(tasks, false)
	routed := improvementsandbox.Overlay{}
	if _, err := ConfirmDisputed(context.Background(), sel, tasks, Arm{Overlay: routed}, Arm{Overlay: routed}, base, cand, 4, EvalOptions{}); err != nil {
		t.Fatal(err)
	}
	if cand.Outcomes[0].Correct {
		t.Error("a consistently wrong candidate must stay wrong")
	}
}

func TestConfirmDisputedNoopCases(t *testing.T) {
	tasks := []Task{{ID: "a", Query: "qa"}}
	sel := flakySelector{mu: &sync.Mutex{}, calls: map[string]int{}}
	base, cand := mkRun(tasks, true), mkRun(tasks, false)
	if n, _ := ConfirmDisputed(context.Background(), sel, tasks, Arm{}, Arm{}, base, cand, 0, EvalOptions{}); n != 0 || sel.calls["qa"] != 0 {
		t.Error("extra=0 must not call the selector")
	}
	if _, err := ConfirmDisputed(context.Background(), sel, tasks, Arm{}, Arm{}, base, &Run{}, 2, EvalOptions{}); err == nil {
		t.Error("mismatched run lengths must error")
	}
}

func TestMajorityTieIsIncorrect(t *testing.T) {
	got := majority([]Outcome{{Correct: true}, {Correct: false}})
	if got.Correct {
		t.Error("tie must resolve to incorrect")
	}
}
