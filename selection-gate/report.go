package selectiongate

// Change is one sealed task whose correctness differed between baseline and candidate.
// It is what a reviewer actually needs: the query, what was expected, and what the agent
// ended up using before and after.
type Change struct {
	TaskID           string `json:"task_id"`
	Query            string `json:"query"`
	Expected         string `json:"expected"`
	BaselineServed   string `json:"baseline_served"`
	CandidateServed  string `json:"candidate_served"`
	BaselineCorrect  bool   `json:"baseline_correct"`
	CandidateCorrect bool   `json:"candidate_correct"`
}

// Report is the full evidence behind a Decision.
type Report struct {
	VersionID         string   `json:"version_id"`
	BaselineVersionID string   `json:"baseline_version_id"`
	Model             string   `json:"model"`
	GoldenName        string   `json:"golden_name"`
	Decision          Decision `json:"decision"`
	// Changes lists only tasks whose correctness flipped (fixed or broken).
	Changes           []Change  `json:"changes"`
	BaselineOutcomes  []Outcome `json:"baseline_outcomes"`
	CandidateOutcomes []Outcome `json:"candidate_outcomes"`
}

// BuildChanges returns the tasks whose correctness differs between two aligned runs.
// tasks supplies the query text; runs and tasks must be in the same order.
func BuildChanges(tasks []Task, baseline, candidate Run) []Change {
	var out []Change
	for i := range baseline.Outcomes {
		if i >= len(candidate.Outcomes) {
			break // misaligned runs: Decide rejects them; never index past the end here
		}
		b, c := baseline.Outcomes[i], candidate.Outcomes[i]
		if b.Correct == c.Correct {
			continue
		}
		query := ""
		if i < len(tasks) && tasks[i].ID == b.TaskID {
			query = tasks[i].Query
		}
		out = append(out, Change{
			TaskID: b.TaskID, Query: query, Expected: b.Expected,
			BaselineServed: servedOrPicked(b), CandidateServed: servedOrPicked(c),
			BaselineCorrect: b.Correct, CandidateCorrect: c.Correct,
		})
	}
	return out
}

func servedOrPicked(o Outcome) string {
	switch {
	case o.Denied:
		return "(denied)"
	case o.Served != "":
		return o.Served
	case o.Err != "":
		return "(selector error)"
	default:
		return "(no tool)"
	}
}
