package improvementsandbox

import (
	"testing"

	"self-improving-mcp/config"
)

func TestScorePromotesEfficiencyWin(t *testing.T) {
	base := ReplayResult{Calls: 10, Errors: 2, Latencies: []int64{10, 10, 10, 10, 10, 10, 10, 10, 10, 100}}
	cand := ReplayResult{Calls: 8, Errors: 1, Latencies: []int64{10, 10, 10, 10, 10, 10, 10, 10}}
	sc := Score(base, cand, config.PromoteConfig{
		MaxErrorRateIncrease:  0.05,
		MaxLatencyP95Increase: 0.2,
		RequireEfficiencyWin:  true,
	})
	if !sc.Promote {
		t.Fatalf("expected promote: %+v", sc)
	}
}
