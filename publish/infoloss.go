package publish

import (
	"fmt"
	"strings"

	selectiongate "self-improving-mcp/selection-gate"
)

// writeInfoLoss lists the old sentences a candidate's descriptions no longer state. The text
// differs by outcome: a blocked candidate tells the reader what to check, an accepted one records
// that a person already chose to ship it.
func writeInfoLoss(b *strings.Builder, d selectiongate.Decision) {
	if len(d.InfoLoss) == 0 {
		return
	}
	if d.InfoLossAccepted {
		b.WriteString("## Information the new descriptions drop (accepted by a person)\n\n")
		b.WriteString("The retention check found these needed sentences missing, and the candidate was shipped anyway with `-accept-info-loss`.\n\n")
	} else {
		b.WriteString("## Information the new descriptions drop\n\n")
		b.WriteString("The selection result is good, but the retention check found sentences an agent needs after choosing the tool (return format, limits, side effects) that the new description no longer states. Restore them, or ship with `-accept-info-loss` after deciding they are not needed.\n\n")
	}
	for _, l := range d.InfoLoss {
		fmt.Fprintf(b, "**`%s`**\n\n", l.Tool)
		for _, s := range l.Dropped {
			fmt.Fprintf(b, "- %s\n", oneLine(s))
		}
		b.WriteString("\n")
	}
}
