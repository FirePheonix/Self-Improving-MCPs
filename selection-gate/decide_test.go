package selectiongate

import (
	"fmt"
	"strings"
	"testing"
)

// mkRuns builds aligned runs of n tasks: the first `both` are right in both runs,
// the next `fixed` wrong->right, the next `broken` right->wrong, the rest wrong in both.
func mkRuns(n, both, fixed, broken int) (Run, Run) {
	var b, c Run
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("t%d", i)
		var bc, cc bool
		switch {
		case i < both:
			bc, cc = true, true
		case i < both+fixed:
			bc, cc = false, true
		case i < both+fixed+broken:
			bc, cc = true, false
		}
		b.Outcomes = append(b.Outcomes, Outcome{TaskID: id, Correct: bc})
		c.Outcomes = append(c.Outcomes, Outcome{TaskID: id, Correct: cc})
	}
	return b, c
}

func TestDecideVerdicts(t *testing.T) {
	cases := []struct {
		name                  string
		n, both, fixed, broke int
		cfg                   GateConfig
		want                  Verdict
		reasonHas             string
	}{
		{"clear win", 73, 58, 7, 0, GateConfig{}, VerdictPass, "improved selection"},
		{"win with one regression is a fail", 73, 55, 8, 1, GateConfig{}, VerdictFail, "regression"},
		{"regression tolerated when allowed, evidence strong", 73, 40, 12, 1, GateConfig{MaxBroken: 1}, VerdictPass, "improved selection"},
		{"net loss", 73, 50, 2, 6, GateConfig{}, VerdictFail, "regression"},
		{"no change", 73, 60, 0, 0, GateConfig{}, VerdictInconclusive, "no improvement"},
		{"weak evidence 4 vs 1", 73, 60, 4, 1, GateConfig{MaxBroken: 1}, VerdictInconclusive, "not statistically supported"},
		{"too small", 20, 10, 5, 0, GateConfig{}, VerdictInconclusive, "too small"},
		{"small ok when configured", 20, 10, 6, 0, GateConfig{MinSealed: 10}, VerdictPass, "improved selection"},
	}
	for _, c := range cases {
		b, cand := mkRuns(c.n, c.both, c.fixed, c.broke)
		d, err := Decide(b, cand, "sha", c.cfg)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if d.Verdict != c.want || !strings.Contains(d.Reason, c.reasonHas) {
			t.Errorf("%s: verdict=%s reason=%q, want %s containing %q", c.name, d.Verdict, d.Reason, c.want, c.reasonHas)
		}
	}
}

func TestDecideCountsAndEvidence(t *testing.T) {
	b, c := mkRuns(73, 58, 7, 0)
	d, err := Decide(b, c, "abc", GateConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Fixed != 7 || d.Broken != 0 || d.BaselineCorrect != 58 || d.CandidateCorrect != 65 {
		t.Fatalf("counts wrong: %+v", d)
	}
	if len(d.FixedTasks) != 7 || d.SealedSHA256 != "abc" {
		t.Fatalf("evidence missing: %+v", d)
	}
	if want := 1.0 / 128; d.PValue < want-1e-12 || d.PValue > want+1e-12 {
		t.Fatalf("p = %v, want %v", d.PValue, want)
	}
}

func TestDecideSelectorErrorsMakeResultInconclusive(t *testing.T) {
	b, c := mkRuns(73, 58, 7, 0)
	for i := 0; i < 10; i++ { // 10 of 73 failed in the candidate run: ~14%
		c.Outcomes[i].Err = "429"
	}
	d, _ := Decide(b, c, "", GateConfig{})
	if d.Verdict != VerdictInconclusive || !strings.Contains(d.Reason, "selector unreliable") {
		t.Fatalf("a flaky selector must never pass: %s %q", d.Verdict, d.Reason)
	}
}

func TestDecideRejectsMisalignedRuns(t *testing.T) {
	b, c := mkRuns(40, 30, 5, 0)
	c.Outcomes[3].TaskID = "other"
	if _, err := Decide(b, c, "", GateConfig{}); err == nil {
		t.Error("expected misalignment error")
	}
	b2, c2 := mkRuns(40, 30, 5, 0)
	c2.Outcomes = c2.Outcomes[:39]
	if _, err := Decide(b2, c2, "", GateConfig{}); err == nil {
		t.Error("expected length error")
	}
}

func TestBuildChangesListsOnlyFlippedTasks(t *testing.T) {
	tasks := []Task{
		{ID: "a", Query: "same right", ExpectedTool: "x"},
		{ID: "b", Query: "fixed one", ExpectedTool: "y"},
		{ID: "c", Query: "broken one", ExpectedTool: "z"},
		{ID: "d", Query: "same wrong", ExpectedTool: "w"},
	}
	base := Run{Outcomes: []Outcome{
		{TaskID: "a", Expected: "x", Served: "x", Correct: true},
		{TaskID: "b", Expected: "y", Served: "read_text_file", Correct: false},
		{TaskID: "c", Expected: "z", Served: "z", Correct: true},
		{TaskID: "d", Expected: "w", Served: "v", Correct: false},
	}}
	cand := Run{Outcomes: []Outcome{
		{TaskID: "a", Expected: "x", Served: "x", Correct: true},
		{TaskID: "b", Expected: "y", Served: "y", Correct: true},
		{TaskID: "c", Expected: "z", Denied: true, Correct: false},
		{TaskID: "d", Expected: "w", Served: "v", Correct: false},
	}}
	got := BuildChanges(tasks, base, cand)
	if len(got) != 2 {
		t.Fatalf("want 2 changes (b fixed, c broken), got %d: %+v", len(got), got)
	}
	if got[0].TaskID != "b" || got[0].Query != "fixed one" || got[0].BaselineServed != "read_text_file" || got[0].CandidateServed != "y" || !got[0].CandidateCorrect {
		t.Errorf("fixed change wrong: %+v", got[0])
	}
	if got[1].TaskID != "c" || got[1].CandidateServed != "(denied)" || got[1].CandidateCorrect {
		t.Errorf("broken change wrong: %+v", got[1])
	}
}

func TestBuildChangesToleratesMisalignedRuns(t *testing.T) {
	base := Run{Outcomes: []Outcome{{TaskID: "a"}, {TaskID: "b"}}}
	cand := Run{Outcomes: []Outcome{{TaskID: "a"}}} // shorter: must not panic
	if got := BuildChanges(nil, base, cand); len(got) != 0 {
		t.Fatalf("unexpected changes: %+v", got)
	}
}

// A sealed set the baseline nearly aces cannot produce a pass, whatever the candidate does. Seen live on
// Browser (56 of 57 right): the gate said "no improvement", which blamed the candidate for the set's limit.
func TestDecideSaysWhenAPassIsOutOfReach(t *testing.T) {
	if got := MinFixedToPass(0.05); got != 5 {
		t.Fatalf("MinFixedToPass(0.05) = %d, want 5 (p = 1/32 at 5/0, 1/16 at 4/0)", got)
	}
	b, c := mkRuns(57, 56, 0, 0) // baseline misses 1
	d, err := Decide(b, c, "sha", GateConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Headroom != 1 || d.MinFixedToPass != 5 || !strings.Contains(d.Reason, "out of reach") {
		t.Fatalf("headroom=%d min=%d reason=%q", d.Headroom, d.MinFixedToPass, d.Reason)
	}
	b, c = mkRuns(73, 58, 7, 0) // 15 wrong at baseline: a pass is possible and happens
	d, _ = Decide(b, c, "sha", GateConfig{})
	if d.Verdict != VerdictPass || d.Headroom != 15 || strings.Contains(d.Reason, "out of reach") {
		t.Fatalf("verdict=%s headroom=%d reason=%q", d.Verdict, d.Headroom, d.Reason)
	}
	b, c = mkRuns(73, 70, 0, 0) // 3 wrong, nothing fixed
	if d, _ = Decide(b, c, "sha", GateConfig{}); !strings.Contains(d.Reason, "misses only 3") {
		t.Fatalf("reason=%q", d.Reason)
	}
}
