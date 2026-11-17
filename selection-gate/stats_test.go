package selectiongate

import (
	"math"
	"testing"
)

func TestSignTestOneSidedExactValues(t *testing.T) {
	cases := []struct {
		fixed, broken int
		want          float64
	}{
		{0, 0, 1},           // nothing changed: no evidence
		{7, 0, 1.0 / 128},   // 7 of 7 changed tasks improved
		{4, 1, 6.0 / 32},    // P(X>=4 | n=5) = (5+1)/32
		{3, 3, 42.0 / 64},   // P(X>=3 | n=6)
		{0, 5, 1},           // only regressions: no support
		{10, 0, 1.0 / 1024}, //
	}
	for _, c := range cases {
		got := SignTestOneSided(c.fixed, c.broken)
		if math.Abs(got-c.want) > 1e-12 {
			t.Errorf("SignTestOneSided(%d,%d) = %.10f, want %.10f", c.fixed, c.broken, got, c.want)
		}
	}
}

func TestSignTestMonotone(t *testing.T) {
	// more fixes with the same breaks can only strengthen the evidence
	prev := 2.0
	for fixed := 0; fixed <= 12; fixed++ {
		p := SignTestOneSided(fixed, 2)
		if p > prev+1e-12 {
			t.Fatalf("p increased from %.6f to %.6f at fixed=%d", prev, p, fixed)
		}
		prev = p
	}
}
