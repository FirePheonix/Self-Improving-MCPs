package selectiongate

import (
	"context"
	"fmt"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
)

// Arm is one side of a comparison: the tools the agent sees and the routing applied to its pick.
type Arm struct {
	Tools   []Tool
	Overlay improvementsandbox.Overlay
}

// strongShare is how lopsided the re-votes must be for a disputed task to count as a real change.
// A task counts as FIXED only if the candidate is right at least this share of the time and the
// baseline at most 1-strongShare of it (and BROKEN in the mirror case).
const strongShare = 0.75

// DefaultConfirmVotes is the number of extra votes per arm for each disputed task.
const DefaultConfirmVotes = 8

// ConfirmDisputed re-asks the selector about tasks where the two arms disagree, and keeps only
// the disagreements that hold up.
//
// Temperature 0 does not make hosted models fully deterministic: the same prompt can flip on a
// borderline task between calls. A single flip would read as "the overlay broke a task" and, with
// a strict regression limit, wrongly discard a harmless candidate. Majority voting alone is not
// enough: for a task the model answers right about half the time, two independent majorities
// disagree about half the time, so an identical overlay still looks like a change.
//
// So each disputed task gets `extra` more votes per arm, and is classified by how the votes fall:
//   - decisively right under the candidate and wrong under the baseline: a real FIX;
//   - the mirror image: a real REGRESSION;
//   - anything else: UNSTABLE. The model is not consistent on this task, so it says nothing about
//     the overlay. Both arms are set to the same outcome (so it counts as unchanged) and the task
//     is marked Unstable in both runs, which keeps it visible in the evidence.
//
// A real effect repeats and is decisive; noise is not. Agreed tasks are not re-asked, so the cost
// is small. base and cand are updated in place. extra <= 0 does nothing; an odd extra is rounded
// up so that each arm has an odd number of votes.
func ConfirmDisputed(ctx context.Context, sel Selector, tasks []Task, baseArm, candArm Arm, base, cand *Run, extra int, opt EvalOptions) (disputed int, err error) {
	if extra <= 0 {
		return 0, nil
	}
	if extra%2 == 1 {
		extra++
	}
	if len(base.Outcomes) != len(tasks) || len(cand.Outcomes) != len(tasks) {
		return 0, fmt.Errorf("confirm: runs (%d, %d) do not match %d tasks", len(base.Outcomes), len(cand.Outcomes), len(tasks))
	}
	var idx []int
	var sub []Task
	for i := range tasks {
		if base.Outcomes[i].Correct != cand.Outcomes[i].Correct {
			idx = append(idx, i)
			sub = append(sub, tasks[i])
		}
	}
	if len(idx) == 0 {
		return 0, nil
	}

	// votes[k] = outcomes of the k-th disputed task across the votes, the original first.
	collect := func(arm Arm, run *Run) ([][]Outcome, error) {
		votes := make([][]Outcome, len(idx))
		for k, i := range idx {
			votes[k] = []Outcome{run.Outcomes[i]}
		}
		for v := 0; v < extra; v++ {
			r, err := Evaluate(ctx, sel, arm.Tools, sub, arm.Overlay, opt)
			if err != nil {
				return nil, err
			}
			for k := range idx {
				votes[k] = append(votes[k], r.Outcomes[k])
			}
		}
		return votes, nil
	}
	baseVotes, err := collect(baseArm, base)
	if err != nil {
		return 0, fmt.Errorf("confirm baseline: %w", err)
	}
	candVotes, err := collect(candArm, cand)
	if err != nil {
		return 0, fmt.Errorf("confirm candidate: %w", err)
	}

	for k, i := range idx {
		bShare, cShare := rightShare(baseVotes[k]), rightShare(candVotes[k])
		switch {
		case bShare >= strongShare && cShare <= 1-strongShare: // real regression
			base.Outcomes[i] = firstWith(baseVotes[k], true)
			cand.Outcomes[i] = firstWith(candVotes[k], false)
		case cShare >= strongShare && bShare <= 1-strongShare: // real fix
			base.Outcomes[i] = firstWith(baseVotes[k], false)
			cand.Outcomes[i] = firstWith(candVotes[k], true)
		default: // unstable: not evidence about the overlay
			m := majority(baseVotes[k])
			c := majority(candVotes[k])
			c.Correct = m.Correct
			m.Unstable, c.Unstable = true, true
			base.Outcomes[i], cand.Outcomes[i] = m, c
		}
	}
	return len(idx), nil
}

func rightShare(votes []Outcome) float64 {
	right := 0
	for _, v := range votes {
		if v.Correct {
			right++
		}
	}
	return float64(right) / float64(len(votes))
}

// firstWith returns the first vote whose correctness equals want. The caller has already shown
// such a vote exists (the share condition), so the fallback is never reached in practice.
func firstWith(votes []Outcome, want bool) Outcome {
	for _, v := range votes {
		if v.Correct == want {
			return v
		}
	}
	return votes[0]
}

// majority returns a vote whose correctness matches the majority. Ties cannot happen with an
// odd number of votes; if one does, the task is treated as incorrect (the safe direction).
func majority(votes []Outcome) Outcome {
	right := 0
	for _, v := range votes {
		if v.Correct {
			right++
		}
	}
	want := right*2 > len(votes)
	for _, v := range votes {
		if v.Correct == want {
			return v
		}
	}
	return votes[0]
}
