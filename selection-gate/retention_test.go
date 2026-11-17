package selectiongate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSplitSentences(t *testing.T) {
	got := SplitSentences("Read many files. Each result has a path (see e.g. the docs). Works in v1.2 only! Is it fast? Yes.")
	want := []string{"Read many files.", "Each result has a path (see e.g. the docs).", "Works in v1.2 only!", "Is it fast?", "Yes."}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %q != %q", i, got[i], want[i])
		}
	}
	if SplitSentences("   ") != nil || len(SplitSentences("no punctuation")) != 1 {
		t.Fatal("edge cases")
	}
}

// scripted returns the given raw answers in order, then repeats the last one.
func scripted(answers ...string) (RetentionJudge, *int) {
	n := 0
	return func(context.Context, string, string) (string, error) {
		a := answers[min(n, len(answers)-1)]
		n++
		if strings.HasPrefix(a, "ERR") {
			return "", errors.New(a)
		}
		return a, nil
	}, &n
}

func ans(entries ...[3]any) string { // {id, needed, retained}
	var parts []string
	for _, e := range entries {
		parts = append(parts, fmt.Sprintf(`{"id":%d,"needed":%v,"retained":%v}`, e[0], e[1], e[2]))
	}
	return `{"sentences":[` + strings.Join(parts, ",") + `]}`
}

const oldDesc = "Read many files at once. Each result starts with the file path. Fails softly on one bad file. Only works inside allowed directories."

func TestOnlyNeededAndMissingSentencesAreLosses(t *testing.T) {
	// 1: rationale (not needed), 2: needed + dropped, 3: needed + retained, 4: needed + dropped
	judge, _ := scripted(ans([3]any{1, false, false}, [3]any{2, true, false}, [3]any{3, true, true}, [3]any{4, true, false}))
	losses, err := CheckRetention(context.Background(), judge, []DescPair{{"read_many", oldDesc, "Read several files in one call."}}, 3)
	if err != nil || len(losses) != 1 {
		t.Fatalf("%v %+v", err, losses)
	}
	if len(losses[0].Dropped) != 2 || losses[0].Dropped[0] != "Each result starts with the file path." || losses[0].Dropped[1] != "Only works inside allowed directories." {
		t.Fatalf("dropped: %q", losses[0].Dropped)
	}
}

func TestMajorityOfVotesDecidesNotAnyOneAnswer(t *testing.T) {
	flaky := ans([3]any{1, true, false}, [3]any{2, false, true}, [3]any{3, false, true}, [3]any{4, false, true})
	calm := ans([3]any{1, false, true}, [3]any{2, false, true}, [3]any{3, false, true}, [3]any{4, false, true})
	judge, calls := scripted(flaky, calm, calm)
	losses, err := CheckRetention(context.Background(), judge, []DescPair{{"t", oldDesc, "changed"}}, 3)
	if err != nil || len(losses) != 0 {
		t.Fatalf("one noisy vote raised a loss: %v %+v", err, losses)
	}
	if *calls != 3 {
		t.Fatalf("judge called %d times, want 3", *calls)
	}
	// and the reverse: one vote saying "fine" cannot hide a loss the other two see
	loss := ans([3]any{1, false, true}, [3]any{2, true, false}, [3]any{3, false, true}, [3]any{4, false, true})
	judge, _ = scripted(calm, loss, loss)
	losses, _ = CheckRetention(context.Background(), judge, []DescPair{{"t", oldDesc, "changed"}}, 3)
	if len(losses) != 1 || len(losses[0].Dropped) != 1 {
		t.Fatalf("majority loss missed: %+v", losses)
	}
}

func TestUnverifiableIsAnErrorNotAPass(t *testing.T) {
	for name, answers := range map[string][]string{
		"judge down":      {"ERR boom"},
		"not json":        {"nope"},
		"missing ids":     {ans([3]any{1, true, true})},
		"id out of range": {ans([3]any{1, true, true}, [3]any{2, true, true}, [3]any{3, true, true}, [3]any{9, true, true})},
		"duplicate id":    {ans([3]any{1, true, true}, [3]any{1, true, true}, [3]any{3, true, true}, [3]any{4, true, true})},
	} {
		judge, _ := scripted(answers...)
		if _, err := CheckRetention(context.Background(), judge, []DescPair{{"t", oldDesc, "changed"}}, 3); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	// one good vote out of three is enough to proceed; bad votes do not poison the counts
	good := ans([3]any{1, false, true}, [3]any{2, true, false}, [3]any{3, false, true}, [3]any{4, false, true})
	judge, _ := scripted("ERR boom", ans([3]any{1, true, false}), good)
	losses, err := CheckRetention(context.Background(), judge, []DescPair{{"t", oldDesc, "changed"}}, 3)
	if err != nil || len(losses) != 1 || len(losses[0].Dropped) != 1 {
		t.Fatalf("%v %+v", err, losses)
	}
}

func TestNothingToCheckSkipsTheJudge(t *testing.T) {
	judge, calls := scripted("ERR must not be called")
	losses, err := CheckRetention(context.Background(), judge, []DescPair{
		{"same", oldDesc, oldDesc},
		{"superset", oldDesc, oldDesc + " Extra detail."},
		{"empty-old", "", "anything"},
	}, 3)
	if err != nil || len(losses) != 0 || *calls != 0 {
		t.Fatalf("%v %+v calls=%d", err, losses, *calls)
	}
}

func passDecision() Decision {
	return Decision{Verdict: VerdictPass, Reason: "improved selection", Fixed: 7, Broken: 0}
}

func TestApplyRetention(t *testing.T) {
	loss := []Loss{{Tool: "read_many", Dropped: []string{"Only works inside allowed directories."}}}

	if d := ApplyRetention(passDecision(), nil, nil, false); d.Verdict != VerdictPass || len(d.InfoLoss) != 0 {
		t.Fatalf("clean pass changed: %+v", d)
	}
	d := ApplyRetention(passDecision(), loss, nil, false)
	if d.Verdict != VerdictInconclusive || !strings.Contains(d.Reason, "read_many") || !strings.Contains(d.Reason, "allowed directories") || !strings.Contains(d.Reason, "-accept-info-loss") || len(d.InfoLoss) != 1 {
		t.Fatalf("loss must block: %+v", d)
	}
	d = ApplyRetention(passDecision(), loss, nil, true)
	if d.Verdict != VerdictPass || !d.InfoLossAccepted || len(d.InfoLoss) != 1 {
		t.Fatalf("accepted loss must stay recorded: %+v", d)
	}
	d = ApplyRetention(passDecision(), nil, errors.New("judge down"), true)
	if d.Verdict != VerdictInconclusive || !strings.Contains(d.Reason, "could not be verified") {
		t.Fatalf("an unverifiable check must block even when losses are accepted: %+v", d)
	}
	for _, v := range []Verdict{VerdictFail, VerdictInconclusive} {
		in := Decision{Verdict: v, Reason: "r"}
		if out := ApplyRetention(in, loss, nil, true); out.Verdict != v || out.Reason != "r" || len(out.InfoLoss) != 0 {
			t.Fatalf("%s must be untouched: %+v", v, out)
		}
	}
}
