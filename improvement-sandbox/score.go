package improvementsandbox

import (
	"encoding/json"
	"math"
	"sort"

	"self-improving-mcp/config"
)

// Scorecard compares baseline vs candidate replay stats.
type Scorecard struct {
	BaselineCalls    int     `json:"baseline_calls"`
	CandidateCalls   int     `json:"candidate_calls"`
	BaselineErrors   int     `json:"baseline_errors"`
	CandidateErrors  int     `json:"candidate_errors"`
	BaselineP95Ms    float64 `json:"baseline_p95_ms"`
	CandidateP95Ms   float64 `json:"candidate_p95_ms"`
	BaselineErrRate  float64 `json:"baseline_error_rate"`
	CandidateErrRate float64 `json:"candidate_error_rate"`
	EfficiencyWin    bool    `json:"efficiency_win"`
	Promote          bool    `json:"promote"`
	Reason           string  `json:"reason"`
}

// Score builds a scorecard and applies promote thresholds.
func Score(baseline, candidate ReplayResult, cfg config.PromoteConfig) Scorecard {
	sc := Scorecard{
		BaselineCalls:    baseline.Calls,
		CandidateCalls:   candidate.Calls,
		BaselineErrors:   baseline.Errors,
		CandidateErrors:  candidate.Errors,
		BaselineP95Ms:    p95(baseline.Latencies),
		CandidateP95Ms:   p95(candidate.Latencies),
		BaselineErrRate:  rate(baseline.Errors, baseline.Calls),
		CandidateErrRate: rate(candidate.Errors, candidate.Calls),
	}

	errDelta := sc.CandidateErrRate - sc.BaselineErrRate
	latRatio := 0.0
	if sc.BaselineP95Ms > 0 {
		latRatio = (sc.CandidateP95Ms - sc.BaselineP95Ms) / sc.BaselineP95Ms
	}

	fewerCalls := candidate.Calls > 0 && candidate.Calls < baseline.Calls
	fewerErrors := candidate.Errors < baseline.Errors
	faster := sc.CandidateP95Ms < sc.BaselineP95Ms
	sc.EfficiencyWin = fewerCalls || fewerErrors || faster

	maxErr := cfg.MaxErrorRateIncrease
	if maxErr == 0 {
		maxErr = 0.02
	}
	maxLat := cfg.MaxLatencyP95Increase
	if maxLat == 0 {
		maxLat = 0.10
	}

	if errDelta > maxErr {
		sc.Reason = "error rate regression"
		return sc
	}
	if latRatio > maxLat {
		sc.Reason = "latency regression"
		return sc
	}
	if cfg.RequireEfficiencyWin && !sc.EfficiencyWin {
		sc.Reason = "no efficiency win"
		return sc
	}
	sc.Promote = true
	sc.Reason = "passed promote gate"
	return sc
}

// MarshalScorecard encodes a scorecard as JSON.
func MarshalScorecard(sc Scorecard) string {
	b, _ := json.Marshal(sc)
	return string(b)
}

func rate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func p95(xs []int64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]int64(nil), xs...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	idx := int(math.Floor(0.95 * float64(len(cp)-1)))
	return float64(cp[idx])
}
