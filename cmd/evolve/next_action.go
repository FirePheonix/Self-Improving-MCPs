package main

import selectiongate "self-improving-mcp/selection-gate"

// preGateAction is what `evolve run` does after the replay sandbox, before any selection gate.
type preGateAction int

const (
	// preDiscardReplayFailed: the replay sandbox rejected the candidate; that is final.
	preDiscardReplayFailed preGateAction = iota
	// preRunGate: judge selection quality on the sealed golden tasks.
	preRunGate
	// preRefuseAutoPromote: auto-promote was requested but nothing can verify selection quality.
	preRefuseAutoPromote
	// preReviewOnly: no golden set; leave the candidate for a human.
	preReviewOnly
)

// decidePreGate is deliberately a pure function so every branch can be tested.
// Without a golden set nothing verifies selection quality, so nothing may be promoted automatically.
func decidePreGate(replayPassed, goldenConfigured, autoPromote bool) preGateAction {
	switch {
	case !replayPassed:
		return preDiscardReplayFailed
	case goldenConfigured:
		return preRunGate
	case autoPromote:
		return preRefuseAutoPromote
	default:
		return preReviewOnly
	}
}

// postGateAction is what `evolve run` does with the gate's verdict.
type postGateAction int

const (
	// postPromote: gate passed and auto-promote was requested.
	postPromote postGateAction = iota
	// postAwaitManualPromote: gate passed; a human promotes.
	postAwaitManualPromote
	// postDiscard: selection regressed.
	postDiscard
	// postNotPromoted: inconclusive (or anything unrecognized); leave in place and tell a human.
	postNotPromoted
)

// decidePostGate maps a verdict to an action. Only an explicit pass can promote;
// an unknown verdict falls through to "not promoted".
func decidePostGate(v selectiongate.Verdict, autoPromote bool) postGateAction {
	switch v {
	case selectiongate.VerdictPass:
		if autoPromote {
			return postPromote
		}
		return postAwaitManualPromote
	case selectiongate.VerdictFail:
		return postDiscard
	default:
		return postNotPromoted
	}
}
