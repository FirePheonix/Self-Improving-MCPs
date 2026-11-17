package selectiongate

import (
	"fmt"
)

// Verdict is the gate's three-way outcome.
type Verdict string

const (
	// VerdictPass means the overlay measurably improved selection with no unacceptable regressions.
	VerdictPass Verdict = "pass"
	// VerdictFail means the overlay made selection worse; discard it.
	VerdictFail Verdict = "fail"
	// VerdictInconclusive means the evidence is insufficient either way; do not ship, but tell a human.
	VerdictInconclusive Verdict = "inconclusive"
)

// GateConfig sets the evidence bar. Zero values take safe defaults.
type GateConfig struct {
	// MinSealed is the smallest sealed set the gate will judge on (default 30).
	MinSealed int
	// Alpha is the one-sided significance level for the improvement (default 0.05).
	Alpha float64
	// MaxBroken is how many previously-correct sealed tasks the overlay may break (default 0).
	MaxBroken int
	// MaxSelectorErrorRate is the largest share of selector failures tolerated in either run
	// before the result is considered unreliable (default 0.05).
	MaxSelectorErrorRate float64
}

func (c GateConfig) withDefaults() GateConfig {
	if c.MinSealed <= 0 {
		c.MinSealed = 30
	}
	if c.Alpha <= 0 {
		c.Alpha = 0.05
	}
	if c.MaxBroken < 0 {
		c.MaxBroken = 0
	}
	if c.MaxSelectorErrorRate <= 0 {
		c.MaxSelectorErrorRate = 0.05
	}
	return c
}

// Decision is the gate's full, auditable result.
type Decision struct {
	Verdict           Verdict  `json:"verdict"`
	Reason            string   `json:"reason"`
	SealedN           int      `json:"sealed_n"`
	BaselineCorrect   int      `json:"baseline_correct"`
	CandidateCorrect  int      `json:"candidate_correct"`
	BaselineAccuracy  float64  `json:"baseline_accuracy"`
	CandidateAccuracy float64  `json:"candidate_accuracy"`
	Fixed             int      `json:"fixed"`  // wrong under baseline, right under candidate
	Broken            int      `json:"broken"` // right under baseline, wrong under candidate
	FixedTasks        []string `json:"fixed_tasks,omitempty"`
	BrokenTasks       []string `json:"broken_tasks,omitempty"`
	PValue            float64  `json:"p_value"` // one-sided exact sign test on fixed vs broken
	Alpha             float64  `json:"alpha"`
	SelectorErrors    int      `json:"selector_errors"` // max across the two runs
	SealedSHA256      string   `json:"sealed_sha256"`
	// UnstableTasks counts sealed tasks the selector answered inconsistently, excluded from fixed/broken.
	UnstableTasks int `json:"unstable_tasks,omitempty"`
	// InfoLoss lists needed sentences a candidate description no longer states (retention check).
	InfoLoss []Loss `json:"info_loss,omitempty"`
	// InfoLossAccepted is true when a person chose to ship despite InfoLoss.
	InfoLossAccepted bool `json:"info_loss_accepted,omitempty"`
	// Headroom is how many sealed tasks the baseline gets wrong: the most a candidate could fix.
	Headroom int `json:"headroom"`
	// MinFixedToPass is the fewest fixed tasks (with none broken) that can reach p <= alpha. When
	// Headroom is below it, no candidate can pass on this sealed set, whatever it does: the set is
	// too easy for the agent, and "no improvement" would wrongly suggest the candidate was the problem.
	MinFixedToPass int `json:"min_fixed_to_pass"`
}

// MinFixedToPass returns the smallest number of fixed tasks, with none broken, whose one-sided sign
// test reaches alpha (5 at alpha 0.05). It is the floor on evidence any pass needs.
func MinFixedToPass(alpha float64) int {
	for k := 1; k <= 64; k++ {
		if SignTestOneSided(k, 0) <= alpha {
			return k
		}
	}
	return 65
}

// Decide compares a baseline run with a candidate run over the same sealed tasks.
// Order of checks matters: unreliable or too-small evidence is inconclusive before any
// judgement; a regression fails even if other tasks improved; a pass needs statistical support.
func Decide(baseline, candidate Run, sealedSHA string, cfg GateConfig) (Decision, error) {
	cfg = cfg.withDefaults()
	if len(baseline.Outcomes) != len(candidate.Outcomes) {
		return Decision{}, fmt.Errorf("runs cover different task counts (%d vs %d)", len(baseline.Outcomes), len(candidate.Outcomes))
	}

	d := Decision{SealedN: len(baseline.Outcomes), Alpha: cfg.Alpha, SealedSHA256: sealedSHA, PValue: 1}
	for _, o := range baseline.Outcomes {
		if o.Unstable {
			d.UnstableTasks++
		}
	}
	baseErrs, candErrs := 0, 0
	for i := range baseline.Outcomes {
		b, c := baseline.Outcomes[i], candidate.Outcomes[i]
		if b.TaskID != c.TaskID {
			return Decision{}, fmt.Errorf("runs are misaligned at index %d (%q vs %q)", i, b.TaskID, c.TaskID)
		}
		if b.Err != "" {
			baseErrs++
		}
		if c.Err != "" {
			candErrs++
		}
		if b.Correct {
			d.BaselineCorrect++
		}
		if c.Correct {
			d.CandidateCorrect++
		}
		switch {
		case !b.Correct && c.Correct:
			d.Fixed++
			d.FixedTasks = append(d.FixedTasks, b.TaskID)
		case b.Correct && !c.Correct:
			d.Broken++
			d.BrokenTasks = append(d.BrokenTasks, b.TaskID)
		}
	}
	d.SelectorErrors = max(baseErrs, candErrs)
	if d.SealedN > 0 {
		d.BaselineAccuracy = float64(d.BaselineCorrect) / float64(d.SealedN)
		d.CandidateAccuracy = float64(d.CandidateCorrect) / float64(d.SealedN)
	}
	d.PValue = SignTestOneSided(d.Fixed, d.Broken)
	d.Headroom = d.SealedN - d.BaselineCorrect
	d.MinFixedToPass = MinFixedToPass(cfg.Alpha)
	outOfReach := ""
	if d.Headroom < d.MinFixedToPass {
		outOfReach = fmt.Sprintf("; a pass is out of reach on this sealed set: the baseline misses only %d task(s) and a pass needs at least %d fixed",
			d.Headroom, d.MinFixedToPass)
	}

	switch {
	case d.SealedN < cfg.MinSealed:
		d.Verdict = VerdictInconclusive
		d.Reason = fmt.Sprintf("sealed set too small to judge (%d < %d)", d.SealedN, cfg.MinSealed)
	case float64(d.SelectorErrors) > cfg.MaxSelectorErrorRate*float64(d.SealedN):
		d.Verdict = VerdictInconclusive
		d.Reason = fmt.Sprintf("selector unreliable: %d of %d calls failed (limit %.0f%%)",
			d.SelectorErrors, d.SealedN, cfg.MaxSelectorErrorRate*100)
	case d.Broken > cfg.MaxBroken:
		d.Verdict = VerdictFail
		d.Reason = fmt.Sprintf("regression: overlay broke %d previously-correct sealed tasks (limit %d)", d.Broken, cfg.MaxBroken)
	case d.Fixed == 0:
		d.Verdict = VerdictInconclusive
		d.Reason = "no improvement: overlay fixed no sealed tasks" + outOfReach
	case d.PValue <= cfg.Alpha:
		d.Verdict = VerdictPass
		d.Reason = fmt.Sprintf("improved selection: fixed %d, broke %d (p=%.4f <= %.2f)", d.Fixed, d.Broken, d.PValue, cfg.Alpha)
	default:
		d.Verdict = VerdictInconclusive
		d.Reason = fmt.Sprintf("improvement not statistically supported: fixed %d, broke %d (p=%.4f > %.2f)", d.Fixed, d.Broken, d.PValue, cfg.Alpha) + outOfReach
	}
	return d, nil
}
