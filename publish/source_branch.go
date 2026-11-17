package publish

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	improvementsandbox "self-improving-mcp/improvement-sandbox"
	selectiongate "self-improving-mcp/selection-gate"
)

// ErrNoSourceEdits means no description could be placed exactly; the Plan says why for each tool.
var ErrNoSourceEdits = errors.New("no description could be located exactly in the source")

// SourceBranchResult describes the branch CreateSourceBranch made.
type SourceBranchResult struct {
	BranchResult
	Plan SourcePlan
}

// CreateSourceBranch carries a gate-approved overlay's description changes into the upstream's
// own source on a new local branch `improve/source-<id>`: the edited source and doc files, plus
// improvements/<id>/{overlay.json,evidence.json,PR.md}.
//
// Edits are planned against the committed files in a throwaway worktree, so uncommitted work in
// the caller's tree can neither be edited nor swept into the commit. Everything BranchSafety
// promises for CreateBranch holds here too: nothing is pushed, no PR is opened, an existing
// branch is never overwritten, and a failure leaves no branch or worktree behind.
//
// Only the report's passing verdict may ship; anything else is refused here as well as by the caller.
func CreateSourceBranch(repoDir string, rep selectiongate.Report, overlayJSON string, o improvementsandbox.Overlay, targets []SourceTarget) (SourceBranchResult, error) {
	if rep.Decision.Verdict != selectiongate.VerdictPass {
		return SourceBranchResult{}, fmt.Errorf("refusing to patch source for a %q candidate: only a gate pass may ship", rep.Decision.Verdict)
	}
	id := ShortID(rep.VersionID)
	if id == "" {
		return SourceBranchResult{}, fmt.Errorf("report has no version id")
	}
	branch := "improve/source-" + id
	rel := filepath.ToSlash(filepath.Join("improvements", id))

	var plan SourcePlan
	commit, err := buildBranch(repoDir, branch, func(tmp string) ([]string, string, string, error) {
		var perr error
		plan, perr = PlanSourceEdits(tmp, targets)
		if perr != nil {
			return nil, "", "", perr
		}
		if len(plan.Files) == 0 {
			return nil, "", "", ErrNoSourceEdits
		}
		stage := []string{rel}
		for name, content := range plan.Files {
			full := filepath.Join(tmp, filepath.FromSlash(name))
			info, serr := os.Stat(full)
			mode := os.FileMode(0o644)
			if serr == nil {
				mode = info.Mode().Perm()
			}
			if werr := os.WriteFile(full, content, mode); werr != nil {
				return nil, "", "", werr
			}
			stage = append(stage, name)
		}
		sort.Strings(stage[1:])
		if werr := writeEvidenceFiles(filepath.Join(tmp, "improvements", id), rep, overlayJSON, RenderSourcePR(rep, o, plan)); werr != nil {
			return nil, "", "", werr
		}
		d := rep.Decision
		subject := fmt.Sprintf("Update %d tool description(s) from overlay %s: fixed %d, broke %d on %d sealed tasks",
			countTools(plan.Edits), id, d.Fixed, d.Broken, d.SealedN)
		return stage, subject, d.Reason, nil
	})
	if err != nil {
		return SourceBranchResult{Plan: plan}, err
	}
	return SourceBranchResult{BranchResult: BranchResult{Branch: branch, Commit: commit, Dir: rel}, Plan: plan}, nil
}

func countTools(edits []SourceEdit) int {
	seen := map[string]bool{}
	for _, e := range edits {
		seen[e.Tool] = true
	}
	return len(seen)
}

// RenderSourcePR is the pull-request text for a source patch: the evidence the overlay earned,
// then exactly where the source was changed, what was skipped, and what cannot be carried over.
func RenderSourcePR(rep selectiongate.Report, o improvementsandbox.Overlay, plan SourcePlan) string {
	var b strings.Builder
	b.WriteString(RenderPR(rep, o))

	b.WriteString("\n## Source changes in this branch\n\n")
	b.WriteString("These edits make the server itself serve the descriptions the overlay proved better, so the overlay patch is no longer needed for them.\n\n")
	b.WriteString("| Tool | File | Line | Kind |\n|---|---|---|---|\n")
	for _, e := range plan.Edits {
		fmt.Fprintf(&b, "| `%s` | `%s` | %d | %s |\n", e.Tool, e.File, e.Line, e.Kind)
	}
	b.WriteString("\n")

	b.WriteString("### Before and after (verbatim)\n\n")
	shown := map[string]bool{}
	for _, e := range plan.Edits {
		if shown[e.Tool] {
			continue
		}
		shown[e.Tool] = true
		fmt.Fprintf(&b, "**`%s`**\n\n- before: %s\n- after:  %s\n\n", e.Tool, oneLine(e.Old), oneLine(e.New))
	}
	if flags := shrinkFlags(plan.Edits); len(flags) > 0 {
		b.WriteString("### Review flags\n\n")
		for _, f := range flags {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	if len(plan.Skips) > 0 {
		b.WriteString("### Not changed (do these by hand)\n\n")
		for _, s := range plan.Skips {
			fmt.Fprintf(&b, "- `%s`: %s\n", s.Tool, s.Reason)
		}
		b.WriteString("\n")
	}

	var rest []string
	for _, r := range o.Routing {
		if r.Deny {
			rest = append(rest, fmt.Sprintf("Routing: `%s` is denied. Consider removing or renaming that tool in the server.", r.FromTool))
		} else {
			rest = append(rest, fmt.Sprintf("Routing: calls to `%s` are sent to `%s`. Agents confuse them; consider merging them, deprecating one, or making their purposes clearer in code.", r.FromTool, r.ToTool))
		}
	}
	for _, name := range sortedKeys(o.ToolPatches) {
		if o.ToolPatches[name].Hidden {
			rest = append(rest, fmt.Sprintf("Hidden: `%s` is hidden from agents by the overlay. Consider removing or deprecating it in the server.", name))
		}
	}
	for _, c := range o.Cache {
		rest = append(rest, fmt.Sprintf("Cache: `%s` is cached for %ds. Consider caching inside the server.", c.ToolName, c.TTLSeconds))
	}
	for _, name := range sortedKeys(o.ComposedTools) {
		rest = append(rest, fmt.Sprintf("Composed tool `%s`: consider adding it to the server as a real tool.", name))
	}
	if len(rest) > 0 {
		b.WriteString("### Parts of the overlay that source edits cannot carry\n\n")
		b.WriteString("These stay in the overlay. They are suggestions for a maintainer, not changes in this branch.\n\n")
		for _, r := range rest {
			fmt.Fprintf(&b, "- %s\n", r)
		}
		b.WriteString("\n")
	}

	b.WriteString("### Before merging\n\n")
	b.WriteString("- Read the diff: each changed string is what agents will read. The gate measured these exact descriptions on sealed tasks.\n")
	b.WriteString("- Run your server's own tests: some may assert the old description text.\n")
	b.WriteString("- After this ships, the overlay's description patches become no-ops and can be retired.\n")
	return b.String()
}

// shrinkFlags warns when a description got much shorter. The gate measures whether the agent picks
// the right tool, not whether the description still says what an agent needs once it has chosen:
// return format, limits, side effects. A shorter text can win on selection and lose that.
func shrinkFlags(edits []SourceEdit) []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range edits {
		if seen[e.Tool] {
			continue
		}
		seen[e.Tool] = true
		if len(e.Old) > 0 && len(e.New)*10 < len(e.Old)*6 {
			out = append(out, fmt.Sprintf("`%s`: the new description is %d%% shorter (%d -> %d characters). Check that nothing agents rely on after choosing the tool (return format, limits, side effects) was dropped; the gate only measures tool selection.",
				e.Tool, 100-len(e.New)*100/len(e.Old), len(e.Old), len(e.New)))
		}
	}
	return out
}
