package publish

import (
	"strings"
	"testing"
)

// The exact layout of @modelcontextprotocol/server-filesystem's compiled source.
const wrappedJS = `    server.registerTool("read_multiple_files", {
        title: "Read Multiple Files",
        description: "Read the contents of multiple files simultaneously. This is more " +
            "efficient than reading files one by one when you need to analyze " +
            "or compare multiple files. Only works within allowed directories.",
        inputSchema: schema,
    }, handler);
`

const wrappedOld = "Read the contents of multiple files simultaneously. This is more " +
	"efficient than reading files one by one when you need to analyze " +
	"or compare multiple files. Only works within allowed directories."

func joinedRuns(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	for _, r := range literalRuns(src, scanLiterals(src)) {
		out = append(out, runValue(r))
	}
	return out
}

func TestWrappedLiteralRunIsMatchedAndRewrittenInTheSameLayout(t *testing.T) {
	nw := "Read several text files in one call, which is more efficient than one by one. " +
		"Prefer this over read_text_file whenever the request names two or more files."
	p := plan1(t, map[string]string{"server.js": wrappedJS}, wrappedOld, nw)
	if len(p.Skips) != 0 || len(p.Edits) != 1 {
		t.Fatalf("%+v", p)
	}
	if p.Edits[0].Line != 3 || !strings.Contains(p.Edits[0].Kind, "wrapped over lines") {
		t.Fatalf("edit: %+v", p.Edits[0])
	}
	got := string(p.Files["server.js"])

	// the new description reads back exactly, as one joined string
	found := false
	for _, v := range joinedRuns(t, got) {
		if v == nw {
			found = true
		}
		if v == wrappedOld {
			t.Fatal("old text still present")
		}
	}
	if !found {
		t.Fatalf("new description not present as one run:\n%s", got)
	}
	// everything around it is untouched
	for _, keep := range []string{`server.registerTool("read_multiple_files", {`, `title: "Read Multiple Files",`, `inputSchema: schema,`, `}, handler);`, "description: \""} {
		if !strings.Contains(got, keep) {
			t.Errorf("lost surrounding code %q:\n%s", keep, got)
		}
	}
	// still wrapped with the original separator (` +` newline and 12-space indent), not one 150-char line
	if !strings.Contains(got, " +\n            \"") {
		t.Errorf("layout not preserved:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if len(line) > 100 {
			t.Errorf("line too long (%d): %s", len(line), line)
		}
	}
}

func TestRunMustMatchAsAWholeNotAsAPrefix(t *testing.T) {
	// the description is only the first two pieces of a longer run: do not edit it
	short := "Read the contents of multiple files simultaneously. This is more " +
		"efficient than reading files one by one when you need to analyze "
	p := plan1(t, map[string]string{"server.js": wrappedJS}, short, "x brand new description text here")
	if len(p.Edits) != 0 || len(p.Skips) != 1 {
		t.Fatalf("prefix of a run must not match: %+v", p)
	}
}

func TestNeighbouringPropertiesAreNotMergedIntoOneRun(t *testing.T) {
	src := `const t = { description: "A tool that does the first useful thing well.", title: "Second thing that is long enough" };`
	runs := joinedRuns(t, src)
	if len(runs) != 2 || runs[0] != "A tool that does the first useful thing well." {
		t.Fatalf("runs: %q", runs)
	}
	p := plan1(t, map[string]string{"a.js": src}, "A tool that does the first useful thing well.", "Replaced description text, clearly different.")
	if want := `const t = { description: "Replaced description text, clearly different.", title: "Second thing that is long enough" };`; string(p.Files["a.js"]) != want {
		t.Fatalf("got %s", p.Files["a.js"])
	}
}

func TestPythonImplicitConcatenationInParentheses(t *testing.T) {
	src := "TOOL = Tool(\n    description=(\n        \"Search notes by keyword across every note \"\n        \"in the notebook and return matching ids.\"\n    ),\n)\n"
	old := "Search notes by keyword across every note in the notebook and return matching ids."
	p := plan1(t, map[string]string{"t.py": src}, old, "Find notes containing a keyword; returns their ids, one per line.")
	if len(p.Edits) != 1 {
		t.Fatalf("%+v", p)
	}
	got := string(p.Files["t.py"])
	if !strings.Contains(got, "description=(") || !strings.Contains(got, "    ),\n)") {
		t.Fatalf("surrounding code damaged:\n%s", got)
	}
	if v := joinedRuns(t, got); len(v) != 1 || v[0] != "Find notes containing a keyword; returns their ids, one per line." {
		t.Fatalf("runs after edit: %q\n%s", v, got)
	}
}

func TestUndecodableEscapeNeverMatches(t *testing.T) {
	src := `const d = "Caf\u00e9 menu lookup tool for the kitchen" + "";` + "\n"
	p := plan1(t, map[string]string{"a.js": src}, "Caf\u00e9 menu lookup tool for the kitchen", "Replacement text for the kitchen tool.")
	if len(p.Edits) != 0 {
		t.Fatalf("a literal with an escape we cannot decode must not be edited: %+v", p)
	}
}

func TestQuotesInNewTextAreEscapedInEveryWrappedPiece(t *testing.T) {
	nw := `Use "read_multiple_files" for several files; it is the right choice when a request lists paths, and "read_text_file" for exactly one file.`
	p := plan1(t, map[string]string{"server.js": wrappedJS}, wrappedOld, nw)
	got := string(p.Files["server.js"])
	var ok bool
	for _, v := range joinedRuns(t, got) {
		ok = ok || v == nw
	}
	if !ok {
		t.Fatalf("quotes broke the literal:\n%s", got)
	}
}

func TestSourcePRShowsBeforeAfterAndFlagsMuchShorterDescriptions(t *testing.T) {
	short := "Read many files at once."
	p := plan1(t, map[string]string{"server.js": wrappedJS}, wrappedOld, short)
	pr := RenderSourcePR(passReport(), sampleOverlay(), p)
	for _, must := range []string{"Before and after (verbatim)", "- before: Read the contents of multiple files", "- after:  " + short, "Review flags", "shorter", "only measures tool selection"} {
		if !strings.Contains(pr, must) {
			t.Errorf("PR missing %q", must)
		}
	}
	// a description of similar length raises no shrink flag
	same := plan1(t, map[string]string{"server.js": wrappedJS}, wrappedOld, wrappedOld+" Extra detail.")
	if strings.Contains(RenderSourcePR(passReport(), sampleOverlay(), same), "shorter") {
		t.Error("flagged a description that grew")
	}
}
