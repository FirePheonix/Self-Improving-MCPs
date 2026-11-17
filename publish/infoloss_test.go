package publish

import (
	"strings"
	"testing"

	selectiongate "self-improving-mcp/selection-gate"
)

func lossReport(accepted bool, v selectiongate.Verdict) selectiongate.Report {
	r := sampleReport(v)
	r.Decision.InfoLoss = []selectiongate.Loss{{Tool: "read_multiple_files", Dropped: []string{"Only works within allowed directories."}}}
	r.Decision.InfoLossAccepted = accepted
	return r
}

func TestNotificationAndPRShowDroppedInformation(t *testing.T) {
	blocked := lossReport(false, selectiongate.VerdictInconclusive)
	n := RenderNotification(blocked, sampleOverlay())
	for _, must := range []string{"Information the new descriptions drop", "read_multiple_files", "Only works within allowed directories.", "-accept-info-loss"} {
		if !strings.Contains(n, must) {
			t.Errorf("notification missing %q", must)
		}
	}
	accepted := RenderPR(lossReport(true, selectiongate.VerdictPass), sampleOverlay())
	if !strings.Contains(accepted, "accepted by a person") || !strings.Contains(accepted, "Only works within allowed directories.") {
		t.Errorf("accepted loss must be recorded in the PR:\n%s", accepted)
	}
	clean := RenderPR(sampleReport(selectiongate.VerdictPass), sampleOverlay())
	if strings.Contains(clean, "Information the new descriptions drop") {
		t.Error("no loss, no section")
	}
}
