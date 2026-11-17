package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	"self-improving-mcp/publish"
	selectiongate "self-improving-mcp/selection-gate"
	"self-improving-mcp/store"
)

// publishOptions controls what happens to a gate decision beyond recording it.
type publishOptions struct {
	// Branch records a passing overlay on a new local side branch. It never pushes.
	Branch bool
	// Repo is the git repository the branch is created in.
	Repo string
	// WebhookURL, when set, also receives a JSON summary of the outcome. A delivery failure is
	// reported but never hides or fails the gate result.
	WebhookURL string
}

// publishAction is what publishOutcome will do for a verdict.
type publishAction int

const (
	publishNothing publishAction = iota // pass without -branch: only a hint is printed
	publishBranch                       // pass with -branch: side branch with the evidence
	publishNotify                       // fail or inconclusive: always write a notification
)

// decidePublish is pure so the policy is tested: a candidate that does not ship must never
// disappear silently, and a branch is only made on an explicit pass.
func decidePublish(v selectiongate.Verdict, branch bool) publishAction {
	if v == selectiongate.VerdictPass {
		if branch {
			return publishBranch
		}
		return publishNothing
	}
	return publishNotify
}

// publishOutcome turns a gate decision into something a human can act on.
func publishOutcome(db *store.DB, dataDir string, rep selectiongate.Report, opt publishOptions) error {
	ver, err := db.GetVersion(rep.VersionID)
	if err != nil {
		return err
	}
	overlay, err := improvementsandbox.ParseOverlay(ver.OverlayJSON)
	if err != nil {
		return fmt.Errorf("parse overlay for publishing: %w", err)
	}

	defer announce(rep, opt.WebhookURL)
	switch decidePublish(rep.Decision.Verdict, opt.Branch) {
	case publishBranch:
		res, err := publish.CreateBranch(opt.Repo, rep, ver.OverlayJSON, publish.RenderPR(rep, overlay))
		if err != nil {
			return fmt.Errorf("create side branch: %w", err)
		}
		fmt.Printf("side branch created: %s (commit %s, files in %s/)\nnothing was pushed; review it, then push or open a PR yourself\n",
			res.Branch, res.Commit[:8], res.Dir)
	case publishNothing:
		fmt.Println("gate passed; add -branch to record this overlay on a side branch with the evidence")
	case publishNotify:
		if dataDir == "" {
			dataDir = "data"
		}
		path, err := publish.WriteNotification(filepath.Join(dataDir, "notifications"), rep, overlay, time.Now())
		if err != nil {
			return err
		}
		fmt.Printf("NOT SHIPPED (%s): %s\nnotification: %s\n", rep.Decision.Verdict, rep.Decision.Reason, path)
	}
	return nil
}

// announce posts the outcome to the configured webhook. It runs last so the local result
// (branch or notification file) is already recorded, and its failure only prints a warning.
func announce(rep selectiongate.Report, url string) {
	if url == "" {
		return
	}
	if err := publish.SendWebhook(context.Background(), url, publish.BuildWebhookPayload(rep)); err != nil {
		fmt.Printf("warning: webhook not delivered: %v\n", err)
		return
	}
	fmt.Println("webhook delivered")
}
