package selectiongate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// The selection gate measures whether the agent PICKS the right tool. A description can win that
// while quietly dropping what an agent needs AFTER picking it: the return format, a limit, a side
// effect, a precondition. The retention check closes that gap. It compares the description served
// today with the candidate's, sentence by sentence, and reports needed information that is gone.

// DescPair is one tool whose description a candidate changes.
type DescPair struct {
	Tool, Old, New string
}

// Loss is information a candidate's description no longer states.
type Loss struct {
	Tool    string   `json:"tool"`
	Dropped []string `json:"dropped"` // the old sentences that are needed and not retained
}

// RetentionJudge is the single model call the check needs (JSON mode, temperature 0).
type RetentionJudge func(ctx context.Context, system, user string) (string, error)

const retentionSystem = `You compare two versions of an MCP tool description. An AI agent reads the description to choose a tool and then to call it correctly.
You are given the OLD description split into numbered sentences, and the NEW description.
For every numbered OLD sentence decide two things:
- "needed": true if an agent needs this information AFTER it has chosen the tool, to call it or to use its result correctly. Examples: what the result looks like, limits, side effects, required conditions, what happens on failure, how a parameter behaves, what the tool does not do. false for pure marketing or rationale (for example "this is more efficient than ...") and for text that only helps choose between tools.
- "retained": true if the NEW description still states that information, even in different words. false if it is gone or contradicted.
Return JSON only: {"sentences":[{"id":1,"needed":true,"retained":false}, ...]} with one entry for every id.`

const maxSentences = 30

// SplitSentences splits on . ! ? followed by whitespace and a capital letter, digit or opening
// quote/bracket, so "e.g. the" and "v1.2 is" stay whole.
func SplitSentences(s string) []string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '.' && rs[i] != '!' && rs[i] != '?' {
			continue
		}
		if i+2 >= len(rs) || rs[i+1] != ' ' {
			continue
		}
		next := rs[i+2]
		if unicode.IsUpper(next) || unicode.IsDigit(next) || strings.ContainsRune("\"'`([", next) {
			out = append(out, strings.TrimSpace(string(rs[start:i+1])))
			start = i + 2
		}
	}
	if tail := strings.TrimSpace(string(rs[start:])); tail != "" {
		out = append(out, tail)
	}
	return out
}

type retentionVote struct {
	Sentences []struct {
		ID       int  `json:"id"`
		Needed   bool `json:"needed"`
		Retained bool `json:"retained"`
	} `json:"sentences"`
}

// CheckRetention reports, per tool, the old sentences that are needed after selection and absent
// from the new description. Each pair is judged `votes` times (default 3) and the majority decides
// every sentence, so one noisy answer cannot raise or hide a loss. A tool whose votes cannot be
// parsed makes the whole check an error: the caller must treat "could not verify" as not safe.
func CheckRetention(ctx context.Context, judge RetentionJudge, pairs []DescPair, votes int) ([]Loss, error) {
	if votes <= 0 {
		votes = 3
	}
	var losses []Loss
	for _, p := range pairs {
		old := strings.Join(strings.Fields(p.Old), " ")
		nw := strings.Join(strings.Fields(p.New), " ")
		if old == "" || strings.Contains(nw, old) {
			continue // nothing to lose
		}
		sents := SplitSentences(old)
		if len(sents) > maxSentences {
			return nil, fmt.Errorf("retention check for %s: old description has %d sentences, over the limit of %d", p.Tool, len(sents), maxSentences)
		}
		var prompt strings.Builder
		prompt.WriteString("OLD description, numbered:\n")
		for i, s := range sents {
			fmt.Fprintf(&prompt, "%d. %s\n", i+1, s)
		}
		fmt.Fprintf(&prompt, "\nNEW description:\n%s\n", nw)

		needed := make([]int, len(sents))
		retained := make([]int, len(sents))
		valid := 0
		var lastErr error
		for v := 0; v < votes; v++ {
			raw, err := judge(ctx, retentionSystem, prompt.String())
			if err != nil {
				lastErr = err
				continue
			}
			var rv retentionVote
			if err := json.Unmarshal([]byte(raw), &rv); err != nil {
				lastErr = fmt.Errorf("unparseable judge answer: %w", err)
				continue
			}
			seen := make([]bool, len(sents))
			bad := false
			for _, e := range rv.Sentences {
				if e.ID < 1 || e.ID > len(sents) || seen[e.ID-1] {
					bad = true
					break
				}
				seen[e.ID-1] = true
				if e.Needed {
					needed[e.ID-1]++
				}
				if e.Retained {
					retained[e.ID-1]++
				}
			}
			for _, ok := range seen {
				bad = bad || !ok
			}
			if bad {
				lastErr = fmt.Errorf("judge answer did not cover every sentence exactly once")
				// roll back this vote's counts
				for _, e := range rv.Sentences {
					if e.ID >= 1 && e.ID <= len(sents) {
						if e.Needed && needed[e.ID-1] > 0 {
							needed[e.ID-1]--
						}
						if e.Retained && retained[e.ID-1] > 0 {
							retained[e.ID-1]--
						}
					}
				}
				continue
			}
			valid++
		}
		if valid == 0 {
			return nil, fmt.Errorf("retention check for %s: no usable judge answer: %v", p.Tool, lastErr)
		}
		var dropped []string
		for i, s := range sents {
			if needed[i]*2 > valid && retained[i]*2 <= valid {
				dropped = append(dropped, s)
			}
		}
		if len(dropped) > 0 {
			losses = append(losses, Loss{Tool: p.Tool, Dropped: dropped})
		}
	}
	return losses, nil
}

// ApplyRetention folds the retention result into a decision. It never turns a non-pass into
// anything better. For a pass:
//   - a failed check (err) is inconclusive: "could not verify" is not "safe";
//   - losses make it inconclusive, with the dropped sentences recorded for the reviewer;
//   - losses with accept=true keep the pass but are recorded as accepted, so the PR shows them.
func ApplyRetention(d Decision, losses []Loss, checkErr error, accept bool) Decision {
	if d.Verdict != VerdictPass {
		return d
	}
	if checkErr != nil {
		d.Verdict = VerdictInconclusive
		d.Reason = fmt.Sprintf("selection improved (fixed %d, broke %d) but information retention could not be verified: %v", d.Fixed, d.Broken, checkErr)
		return d
	}
	if len(losses) == 0 {
		return d
	}
	d.InfoLoss = losses
	if accept {
		d.InfoLossAccepted = true
		return d
	}
	d.Verdict = VerdictInconclusive
	d.Reason = fmt.Sprintf("selection improved (fixed %d, broke %d) but the new description of %s drops information agents need after choosing the tool, for example: %q. Review it; pass -accept-info-loss to ship anyway",
		d.Fixed, d.Broken, lossTools(losses), losses[0].Dropped[0])
	return d
}

func lossTools(losses []Loss) string {
	names := make([]string, len(losses))
	for i, l := range losses {
		names[i] = l.Tool
	}
	return strings.Join(names, ", ")
}
