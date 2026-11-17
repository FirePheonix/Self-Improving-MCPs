package main

import (
	"testing"

	selectiongate "self-improving-mcp/selection-gate"
)

func TestDecidePublish(t *testing.T) {
	cases := []struct {
		name   string
		v      selectiongate.Verdict
		branch bool
		want   publishAction
	}{
		{"pass with -branch makes a branch", selectiongate.VerdictPass, true, publishBranch},
		{"pass without -branch only hints", selectiongate.VerdictPass, false, publishNothing},
		{"fail always notifies", selectiongate.VerdictFail, false, publishNotify},
		{"fail never branches, even with -branch", selectiongate.VerdictFail, true, publishNotify},
		{"inconclusive always notifies", selectiongate.VerdictInconclusive, false, publishNotify},
		{"inconclusive never branches", selectiongate.VerdictInconclusive, true, publishNotify},
		{"unknown verdict never branches", selectiongate.Verdict("x"), true, publishNotify},
	}
	for _, c := range cases {
		if got := decidePublish(c.v, c.branch); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
