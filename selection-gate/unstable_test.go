package selectiongate

import (
	"context"
	"sync"
	"testing"
)

// coinSelector answers right and wrong alternately for a query: a task the model gets right half the time.
type coinSelector struct {
	mu    *sync.Mutex
	calls map[string]int
}

func (c coinSelector) Select(_ context.Context, q string, _ []Tool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[q]++
	if c.calls[q]%2 == 0 {
		return "right", nil
	}
	return "wrong", nil
}

// armSelector is decided by which arm's tool list it is shown: deterministic, so any difference is real.
type armSelector struct{ rightFor map[string]bool }

func (a armSelector) Select(_ context.Context, _ string, tools []Tool) (string, error) {
	if a.rightFor[tools[0].Name] {
		return "right", nil
	}
	return "wrong", nil
}

func TestCoinFlipTaskUnderIdenticalArmsIsUnstableNotARegression(t *testing.T) {
	tasks := []Task{{ID: "a", Query: "qa", ExpectedTool: "right"}}
	sel := coinSelector{mu: &sync.Mutex{}, calls: map[string]int{}}
	// the same overlay on both sides, but the two first answers happened to differ
	base, cand := mkRun(tasks, true), mkRun(tasks, false)
	if _, err := ConfirmDisputed(context.Background(), sel, tasks, Arm{}, Arm{}, base, cand, DefaultConfirmVotes, EvalOptions{}); err != nil {
		t.Fatal(err)
	}
	if base.Outcomes[0].Correct != cand.Outcomes[0].Correct {
		t.Fatal("a coin-flip task must not count as fixed or broken")
	}
	if !base.Outcomes[0].Unstable || !cand.Outcomes[0].Unstable {
		t.Fatal("it must be marked unstable in both runs so the evidence shows it")
	}
	d, err := Decide(*base, *cand, "sha", GateConfig{MinSealed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if d.Verdict == VerdictFail || d.Broken != 0 || d.Fixed != 0 || d.UnstableTasks != 1 {
		t.Fatalf("an identical overlay must not fail: %+v", d)
	}
}

func TestDecisiveRegressionAndFixSurviveConfirmation(t *testing.T) {
	tasks := []Task{{ID: "a", Query: "qa", ExpectedTool: "right"}}
	baseArm := Arm{Tools: []Tool{{Name: "base"}}}
	candArm := Arm{Tools: []Tool{{Name: "cand"}}}

	// regression: always right under base, always wrong under cand
	sel := armSelector{rightFor: map[string]bool{"base": true}}
	base, cand := mkRun(tasks, true), mkRun(tasks, false)
	if _, err := ConfirmDisputed(context.Background(), sel, tasks, baseArm, candArm, base, cand, DefaultConfirmVotes, EvalOptions{}); err != nil {
		t.Fatal(err)
	}
	if !base.Outcomes[0].Correct || cand.Outcomes[0].Correct || base.Outcomes[0].Unstable {
		t.Fatalf("a consistent regression must stay a regression: %+v / %+v", base.Outcomes[0], cand.Outcomes[0])
	}

	// fix: mirror image
	sel = armSelector{rightFor: map[string]bool{"cand": true}}
	base, cand = mkRun(tasks, false), mkRun(tasks, true)
	if _, err := ConfirmDisputed(context.Background(), sel, tasks, baseArm, candArm, base, cand, DefaultConfirmVotes, EvalOptions{}); err != nil {
		t.Fatal(err)
	}
	if base.Outcomes[0].Correct || !cand.Outcomes[0].Correct || cand.Outcomes[0].Unstable {
		t.Fatalf("a consistent fix must stay a fix: %+v / %+v", base.Outcomes[0], cand.Outcomes[0])
	}
}

func TestDefaultVotesGiveAnOddTotalPerArm(t *testing.T) {
	if (DefaultConfirmVotes+1)%2 != 1 || DefaultConfirmVotes < 8 {
		t.Fatalf("DefaultConfirmVotes=%d: want an even extra count of at least 8", DefaultConfirmVotes)
	}
}
