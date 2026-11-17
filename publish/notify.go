package publish

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	selectiongate "self-improving-mcp/selection-gate"
)

// maxNotificationRows caps the changed-task table so a notification stays readable.
const maxNotificationRows = 20

// RenderNotification explains why a candidate was not shipped and what a human can do about it.
func RenderNotification(rep selectiongate.Report, o improvementsandbox.Overlay) string {
	d := rep.Decision
	id := ShortID(rep.VersionID)
	var b strings.Builder

	fmt.Fprintf(&b, "# Overlay %s was NOT shipped: %s\n\n", id, d.Verdict)
	fmt.Fprintf(&b, "**Reason:** %s\n\n", d.Reason)
	fmt.Fprintf(&b, "On %d sealed tasks the baseline got %d right and the candidate got %d right (fixed %d, broke %d, p = %.4f).\n\n",
		d.SealedN, d.BaselineCorrect, d.CandidateCorrect, d.Fixed, d.Broken, d.PValue)

	b.WriteString("## What to do\n\n")
	switch d.Verdict {
	case selectiongate.VerdictFail:
		b.WriteString("The candidate made selection worse and has been left unpromoted. Review the broken tasks below; if the overlay was meant to help, the proposal logic needs fixing, not the gate.\n\n")
	default:
		b.WriteString("The evidence does not show an improvement, so nothing was promoted. Nothing is broken. Options: add more sealed tasks if the set is small, wait for more traffic to give the proposer something to fix, or discard the candidate.\n\n")
	}
	fmt.Fprintf(&b, "Discard it with `evolve discard -version %s`.\n\n", rep.VersionID)

	b.WriteString("## What the candidate changes\n\n")
	b.WriteString(DescribeOverlay(o))
	writeInfoLoss(&b, d)
	if flags := ReviewFlags(o); len(flags) > 0 {
		b.WriteString("## Review flags\n\n")
		for _, f := range flags {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	if len(rep.Changes) > 0 {
		b.WriteString("## Tasks that changed\n\n| Query | Expected | Before | After |\n|---|---|---|---|\n")
		for i, c := range rep.Changes {
			if i == maxNotificationRows {
				fmt.Fprintf(&b, "\n_%d more in the evidence report._\n", len(rep.Changes)-maxNotificationRows)
				break
			}
			fmt.Fprintf(&b, "| %s | `%s` | `%s` | `%s` |\n", cell(c.Query), c.Expected, c.BaselineServed, c.CandidateServed)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Golden set `%s`, sealed fingerprint `%s`, model `%s`.\n", rep.GoldenName, d.SealedSHA256, rep.Model)
	return b.String()
}

// WriteNotification writes the notification into dir and returns its path.
// The name is timestamped so repeated runs never overwrite earlier notices.
func WriteNotification(dir string, rep selectiongate.Report, o improvementsandbox.Overlay, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create notification dir: %w", err)
	}
	name := fmt.Sprintf("%s-%s-%s.md", now.UTC().Format("20060102-150405"), ShortID(rep.VersionID), rep.Decision.Verdict)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(RenderNotification(rep, o)), 0o644); err != nil {
		return "", fmt.Errorf("write notification: %w", err)
	}
	return path, nil
}
