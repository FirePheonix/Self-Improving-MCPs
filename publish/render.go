package publish

import (
	"fmt"
	"sort"
	"strings"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	selectiongate "self-improving-mcp/selection-gate"
)

// ShortID is the first 8 characters of a version id, used in branch and file names.
func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// DescribeOverlay summarizes, in markdown, exactly what an overlay changes. New descriptions are
// shown verbatim because they are what an agent will read.
func DescribeOverlay(o improvementsandbox.Overlay) string {
	var b strings.Builder
	if len(o.Routing) > 0 {
		b.WriteString("**Routing**\n\n")
		for _, r := range o.Routing {
			switch {
			case r.Deny:
				fmt.Fprintf(&b, "- `%s` is **denied**", r.FromTool)
			default:
				fmt.Fprintf(&b, "- `%s` → `%s`", r.FromTool, r.ToTool)
			}
			if r.Note != "" {
				fmt.Fprintf(&b, " (%s)", r.Note)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if names := sortedKeys(o.ToolPatches); len(names) > 0 {
		b.WriteString("**Tool description patches** (shown to agents verbatim; review the wording)\n\n")
		for _, name := range names {
			p := o.ToolPatches[name]
			if p.Description != nil {
				fmt.Fprintf(&b, "- `%s`:\n\n  > %s\n\n", name, oneLine(*p.Description))
			}
			if p.InputSchema != nil {
				fmt.Fprintf(&b, "- `%s`: input schema changed\n", name)
			}
			if p.Hidden {
				fmt.Fprintf(&b, "- `%s`: **hidden** from agents (still exists upstream)\n", name)
			}
		}
	}
	if len(o.Cache) > 0 {
		b.WriteString("**Cache rules**\n\n")
		for _, c := range o.Cache {
			fmt.Fprintf(&b, "- `%s` cached for %ds\n", c.ToolName, c.TTLSeconds)
		}
		b.WriteString("\n")
	}
	if names := sortedKeys(o.ComposedTools); len(names) > 0 {
		b.WriteString("**Composed tools** (new tools the agent will see)\n\n")
		for _, name := range names {
			ct := o.ComposedTools[name]
			steps := make([]string, len(ct.Steps))
			for i, s := range ct.Steps {
				steps[i] = "`" + s.Tool + "`"
			}
			fmt.Fprintf(&b, "- `%s` = %s: %s\n", name, strings.Join(steps, " → "), oneLine(ct.Description))
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "_This overlay changes nothing._\n"
	}
	return b.String()
}

// RenderPR writes the pull-request description for a passing candidate.
func RenderPR(rep selectiongate.Report, o improvementsandbox.Overlay) string {
	d := rep.Decision
	id := ShortID(rep.VersionID)
	var b strings.Builder

	fmt.Fprintf(&b, "# Overlay %s: improved tool selection\n\n", id)
	fmt.Fprintf(&b, "**Verdict: %s.** %s\n\n", d.Verdict, d.Reason)

	b.WriteString("| | Baseline | Candidate |\n|---|---|---|\n")
	fmt.Fprintf(&b, "| Correct on sealed tasks | %d / %d | %d / %d |\n", d.BaselineCorrect, d.SealedN, d.CandidateCorrect, d.SealedN)
	fmt.Fprintf(&b, "| Accuracy | %.1f%% | %.1f%% |\n\n", d.BaselineAccuracy*100, d.CandidateAccuracy*100)
	fmt.Fprintf(&b, "Fixed **%d**, broke **%d** (exact one-sided sign test p = %.4f, threshold %.2f).\n\n", d.Fixed, d.Broken, d.PValue, d.Alpha)

	b.WriteString("## What this changes\n\n")
	b.WriteString(DescribeOverlay(o))
	writeInfoLoss(&b, d)

	if flags := ReviewFlags(o); len(flags) > 0 {
		b.WriteString("## Review flags\n\nThe automatic scan raised these; a person should check them before merging.\n\n")
		for _, f := range flags {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Tasks that changed\n\n")
	writeChanges(&b, rep.Changes, true, "Fixed")
	writeChanges(&b, rep.Changes, false, "Broken")

	b.WriteString("## How it was measured\n\n")
	fmt.Fprintf(&b, "- Selector model: `%s`, temperature 0, same prompt and same tasks for baseline and candidate.\n", rep.Model)
	fmt.Fprintf(&b, "- Golden set: `%s`; sealed set fingerprint `%s`.\n", rep.GoldenName, d.SealedSHA256)
	b.WriteString("- Routing was applied to each pick exactly as the proxy does at call time.\n")
	if rep.BaselineVersionID != "" {
		fmt.Fprintf(&b, "- Baseline: active overlay `%s`.\n", ShortID(rep.BaselineVersionID))
	} else {
		b.WriteString("- Baseline: the upstream with no overlay.\n")
	}
	b.WriteString("\n## Limits\n\n")
	b.WriteString("- This measures a stand-in agent (the selector model) on labeled sealed tasks, not your production agent.\n")
	b.WriteString("- It is evidence of improvement on those tasks, not a guarantee for all traffic.\n")
	b.WriteString("- Nothing is applied by this branch. To use the overlay: `evolve promote -version " + rep.VersionID + "`, then restart the proxy.\n")
	return b.String()
}

func writeChanges(b *strings.Builder, changes []selectiongate.Change, fixed bool, title string) {
	var rows []selectiongate.Change
	for _, c := range changes {
		if c.CandidateCorrect == fixed {
			rows = append(rows, c)
		}
	}
	fmt.Fprintf(b, "### %s (%d)\n\n", title, len(rows))
	if len(rows) == 0 {
		b.WriteString("None.\n\n")
		return
	}
	b.WriteString("| Query | Expected | Before | After |\n|---|---|---|---|\n")
	for _, c := range rows {
		fmt.Fprintf(b, "| %s | `%s` | `%s` | `%s` |\n", cell(c.Query), c.Expected, c.BaselineServed, c.CandidateServed)
	}
	b.WriteString("\n")
}

func cell(s string) string {
	s = oneLine(s)
	s = strings.ReplaceAll(s, "|", "\\|")
	if len(s) > 160 {
		s = s[:157] + "..."
	}
	return s
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
