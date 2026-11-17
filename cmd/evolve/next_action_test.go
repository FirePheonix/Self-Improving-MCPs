package main

import (
	"testing"

	selectiongate "self-improving-mcp/selection-gate"
)

func TestDecidePreGate(t *testing.T) {
	cases := []struct {
		name                                 string
		replayPassed, goldenConfigured, auto bool
		want                                 preGateAction
	}{
		{"replay failed beats everything", false, true, true, preDiscardReplayFailed},
		{"replay failed, no golden", false, false, false, preDiscardReplayFailed},
		{"golden configured runs the gate", true, true, false, preRunGate},
		{"golden configured, auto-promote", true, true, true, preRunGate},
		{"no golden + auto-promote is refused", true, false, true, preRefuseAutoPromote},
		{"no golden, manual: review only", true, false, false, preReviewOnly},
	}
	for _, c := range cases {
		if got := decidePreGate(c.replayPassed, c.goldenConfigured, c.auto); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestDecidePostGate(t *testing.T) {
	cases := []struct {
		name string
		v    selectiongate.Verdict
		auto bool
		want postGateAction
	}{
		{"pass + auto promotes", selectiongate.VerdictPass, true, postPromote},
		{"pass, manual waits for a human", selectiongate.VerdictPass, false, postAwaitManualPromote},
		{"fail discards even with auto", selectiongate.VerdictFail, true, postDiscard},
		{"fail discards", selectiongate.VerdictFail, false, postDiscard},
		{"inconclusive is not promoted, auto or not", selectiongate.VerdictInconclusive, true, postNotPromoted},
		{"inconclusive manual", selectiongate.VerdictInconclusive, false, postNotPromoted},
		{"unknown verdict never promotes", selectiongate.Verdict("whatever"), true, postNotPromoted},
		{"empty verdict never promotes", selectiongate.Verdict(""), true, postNotPromoted},
	}
	for _, c := range cases {
		if got := decidePostGate(c.v, c.auto); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
