package selectiongate

import "math"

// SignTestOneSided returns P(X >= fixed) for X ~ Binomial(fixed+broken, 0.5): the chance of
// seeing at least this many improvements among the tasks whose outcome changed, if the overlay
// were no better than a coin flip. Tasks that did not change carry no information and are ignored.
// Exact (no normal approximation), so it stays valid for the small samples gates actually see.
func SignTestOneSided(fixed, broken int) float64 {
	n := fixed + broken
	if n == 0 {
		return 1
	}
	sum := 0.0
	for k := fixed; k <= n; k++ {
		sum += math.Exp(logChoose(n, k) - float64(n)*math.Ln2)
	}
	return math.Min(1, sum)
}

func logChoose(n, k int) float64 {
	a, _ := math.Lgamma(float64(n + 1))
	b, _ := math.Lgamma(float64(k + 1))
	c, _ := math.Lgamma(float64(n - k + 1))
	return a - b - c
}
