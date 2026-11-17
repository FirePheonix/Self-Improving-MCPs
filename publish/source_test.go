package publish

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	selectiongate "self-improving-mcp/selection-gate"
)

const oldD = "Read the complete contents of a file as text."
const newD = `Read ONE file as text. For several files use "read_multiple_files".`

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func plan1(t *testing.T, files map[string]string, old, nw string) SourcePlan {
	t.Helper()
	p, err := PlanSourceEdits(writeTree(t, files), []SourceTarget{{Tool: "read_text_file", Old: old, New: nw}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSameDescriptionEverywhereIsRefusedAsAmbiguous(t *testing.T) {
	// 7 locations across 5 files and every quoting style: more than maxLocations, so nothing is edited.
	files := map[string]string{
		"server.go":  "package s\nvar d = \"" + oldD + "\"\nvar raw = `" + oldD + "`\n",
		"server.js":  "const a = '" + oldD + "';\nconst b = `" + oldD + "`;\n",
		"tool.py":    "DESC = \"" + oldD + "\"\n",
		"tools.json": `{"description": "` + oldD + `"}`,
		"README.md":  "| read_text_file | " + oldD + " |\n",
	}
	p := plan1(t, files, oldD, newD)
	if len(p.Edits) != 0 || len(p.Files) != 0 || len(p.Skips) != 1 || !strings.Contains(p.Skips[0].Reason, "7 places") {
		t.Fatalf("want one ambiguity skip, got %+v", p)
	}
	// drop two locations and the same tree is editable, in every style
	delete(files, "server.js")
	p = plan1(t, files, oldD, newD)
	if len(p.Skips) != 0 || len(p.Edits) != 5 || len(p.Files) != 4 {
		t.Fatalf("want 5 edits in 4 files, got %+v", p)
	}
}

func TestEscapingPerLiteralKind(t *testing.T) {
	cases := []struct{ file, src, want string }{
		{"a.go", `var d = "` + oldD + `"`, `var d = "Read ONE file as text. For several files use \"read_multiple_files\"."`},
		{"a.js", `const d = '` + oldD + `'`, `const d = 'Read ONE file as text. For several files use "read_multiple_files".'`},
		{"a.py", `D = "` + oldD + `"`, `D = "Read ONE file as text. For several files use \"read_multiple_files\"."`},
		{"a.json", `{"d": "` + oldD + `"}`, `{"d": "Read ONE file as text. For several files use \"read_multiple_files\"."}`},
		{"a.md", "x " + oldD + " y", `x Read ONE file as text. For several files use "read_multiple_files". y`},
	}
	for _, c := range cases {
		p := plan1(t, map[string]string{c.file: c.src}, oldD, newD)
		if len(p.Edits) != 1 {
			t.Fatalf("%s: edits=%+v skips=%+v", c.file, p.Edits, p.Skips)
		}
		if got := string(p.Files[c.file]); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.file, got, c.want)
		}
	}
}

func TestApostropheAndNewlineAndBackslashAreEscaped(t *testing.T) {
	nw := "It's the tool for\nfiles, path C:\\tmp."
	p := plan1(t, map[string]string{"a.js": "const d = '" + oldD + "';"}, oldD, nw)
	if want := `const d = 'It\'s the tool for\nfiles, path C:\\tmp.';`; string(p.Files["a.js"]) != want {
		t.Fatalf("got %s\nwant %s", p.Files["a.js"], want)
	}
	p = plan1(t, map[string]string{"a.js": "const d = `" + oldD + "`;"}, oldD, "uses `${x}` here")
	if want := "const d = `uses \\`\\${x}\\` here`;"; string(p.Files["a.js"]) != want {
		t.Fatalf("template literal: got %s want %s", p.Files["a.js"], want)
	}
}

func TestGoRawStringTakesTextLiterallyButNeverABacktick(t *testing.T) {
	src := "var d = `" + oldD + "`"
	p := plan1(t, map[string]string{"a.go": src}, oldD, `has "quotes" and \ backslash`)
	if want := "var d = `has \"quotes\" and \\ backslash`"; string(p.Files["a.go"]) != want {
		t.Fatalf("got %s want %s", p.Files["a.go"], want)
	}
	p = plan1(t, map[string]string{"a.go": src}, oldD, "has a `backtick`")
	if len(p.Edits) != 0 || len(p.Skips) != 1 || !strings.Contains(p.Skips[0].Reason, "raw string") {
		t.Fatalf("backtick in a Go raw string must be refused: %+v", p)
	}
}

func TestOldTextWithQuotesIsMatchedInItsEscapedForm(t *testing.T) {
	old := `Reads the "main" file at the given path.`
	src := `var d = "Reads the \"main\" file at the given path."`
	p := plan1(t, map[string]string{"a.go": src}, old, "Reads one file at the given path only.")
	if string(p.Files["a.go"]) != `var d = "Reads one file at the given path only."` {
		t.Fatalf("got %s skips=%+v", p.Files["a.go"], p.Skips)
	}
}

func TestSkipsAreExplainedNotGuessed(t *testing.T) {
	tree := map[string]string{"a.go": `var d = "something else entirely, not the description"`}
	for name, c := range map[string]struct{ old, nw, want string }{
		"not found": {oldD, newD, "not found as an exact string literal"},
		"too short": {"Read a file", newD, "too generic"},
		"identical": {oldD, oldD, "already the approved text"},
		"empty":     {"", newD, "serves no description"},
	} {
		p := plan1(t, tree, c.old, c.nw)
		if len(p.Edits) != 0 || len(p.Files) != 0 || len(p.Skips) != 1 || !strings.Contains(p.Skips[0].Reason, c.want) {
			t.Errorf("%s: %+v", name, p)
		}
	}
	// found too many times
	many := map[string]string{}
	for _, f := range []string{"a.md", "b.md", "c.md", "d.md", "e.md", "f.md"} {
		many[f] = oldD
	}
	p := plan1(t, many, oldD, newD)
	if len(p.Edits) != 0 || !strings.Contains(p.Skips[0].Reason, "ambiguous") {
		t.Errorf("ambiguity: %+v", p)
	}
}

func TestDependencyAndBuildDirsAreNeverTouched(t *testing.T) {
	p := plan1(t, map[string]string{
		"src/server.go":                 `var d = "` + oldD + `"`,
		"node_modules/dep/index.js":     `var d = "` + oldD + `"`,
		"vendor/x/x.go":                 `var d = "` + oldD + `"`,
		".git/hooks/note.md":            oldD,
		"dist/bundle.js":                `var d = "` + oldD + `"`,
		"notes.bin":                     oldD,
		"src/unrelated_substring_ok.go": `var other = "` + oldD + ` And more words."`,
	}, oldD, newD)
	if len(p.Edits) != 1 || p.Edits[0].File != "src/server.go" {
		t.Fatalf("edits: %+v", p.Edits)
	}
	if len(p.Files) != 1 {
		t.Fatalf("only src/server.go may change: %v", p.Files)
	}
}

func TestSeveralToolsInOneFile(t *testing.T) {
	a, b := "Description of the first tool, long enough.", "Description of the second tool, long enough."
	root := writeTree(t, map[string]string{"s.go": "var a = \"" + a + "\"\nvar b = \"" + b + "\"\n"})
	p, err := PlanSourceEdits(root, []SourceTarget{{"one", a, "A2 text that is new."}, {"two", b, "B2 text that is new."}})
	if err != nil || len(p.Edits) != 2 {
		t.Fatalf("%v %+v", err, p)
	}
	if got := string(p.Files["s.go"]); got != "var a = \"A2 text that is new.\"\nvar b = \"B2 text that is new.\"\n" {
		t.Fatalf("got %q", got)
	}
	if p.Edits[0].Line != 1 || p.Edits[1].Line != 2 {
		t.Fatalf("lines: %+v", p.Edits)
	}
}

// ---------- the branch ----------

func passReport() selectiongate.Report { return sampleReport(selectiongate.VerdictPass) }

func commitFile(t *testing.T, repo, name, content string) {
	t.Helper()
	p := filepath.Join(repo, filepath.FromSlash(name))
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", name)
	git(t, repo, "commit", "-q", "-m", "add "+name)
}

func TestCreateSourceBranchEditsCommittedSourceAndLeavesWorktreeAlone(t *testing.T) {
	repo := newRepo(t)
	commitFile(t, repo, "src/server.go", "package s\n\nvar desc = \""+oldD+"\"\n")
	commitFile(t, repo, "docs/tools.md", "- `read_text_file`: "+oldD+"\n")
	// the user's in-progress work, including an uncommitted edit to the very file we will change
	wip := "package s\n\nvar desc = \"MY UNCOMMITTED EDIT\"\n"
	_ = os.WriteFile(filepath.Join(repo, "src/server.go"), []byte(wip), 0o644)
	_ = os.WriteFile(filepath.Join(repo, "scratch.txt"), []byte("wip"), 0o644)
	headBefore := git(t, repo, "rev-parse", "HEAD")
	statusBefore := git(t, repo, "status", "--porcelain")

	res, err := CreateSourceBranch(repo, passReport(), overlayJSON, sampleOverlay(),
		[]SourceTarget{{Tool: "read_text_file", Old: oldD, New: newD}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Branch != "improve/source-1b1acc5d" {
		t.Fatalf("branch %q", res.Branch)
	}

	if git(t, repo, "rev-parse", "HEAD") != headBefore || git(t, repo, "status", "--porcelain") != statusBefore {
		t.Fatal("working tree, index or HEAD changed")
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "src/server.go")); string(b) != wip {
		t.Fatal("uncommitted edit was touched")
	}

	// the branch is based on committed HEAD: the edit applies to the committed text
	got := git(t, repo, "show", res.Branch+":src/server.go")
	if !strings.Contains(got, `For several files use \"read_multiple_files\".`) || strings.Contains(got, "MY UNCOMMITTED") {
		t.Fatalf("source on branch:\n%s", got)
	}
	if !strings.Contains(git(t, repo, "show", res.Branch+":docs/tools.md"), "Read ONE file as text") {
		t.Fatal("doc not updated")
	}

	files := strings.Fields(git(t, repo, "show", "--name-only", "--format=", res.Branch))
	want := map[string]bool{
		"src/server.go": true, "docs/tools.md": true,
		"improvements/1b1acc5d/overlay.json": true, "improvements/1b1acc5d/evidence.json": true, "improvements/1b1acc5d/PR.md": true,
	}
	if len(files) != len(want) {
		t.Fatalf("commit touched %v", files)
	}
	for _, f := range files {
		if !want[f] {
			t.Errorf("unexpected file in commit: %s", f)
		}
	}
	pr := git(t, repo, "show", res.Branch+":improvements/1b1acc5d/PR.md")
	for _, must := range []string{"Source changes in this branch", "`src/server.go`", "`docs/tools.md`", "Parts of the overlay that source edits cannot carry", "read_text_file` are sent to `read_multiple_files`", "Run your server's own tests"} {
		if !strings.Contains(pr, must) {
			t.Errorf("PR.md missing %q", must)
		}
	}
	if n := len(strings.Split(git(t, repo, "worktree", "list"), "\n")); n != 1 {
		t.Errorf("leftover worktrees: %d", n)
	}
}

func TestCreateSourceBranchRefusesNonPassAndNoMatch(t *testing.T) {
	repo := newRepo(t)
	commitFile(t, repo, "src/server.go", "package s\n\nvar other = \"unrelated\"\n")
	targets := []SourceTarget{{Tool: "t", Old: oldD, New: newD}}

	for _, v := range []selectiongate.Verdict{selectiongate.VerdictFail, selectiongate.VerdictInconclusive} {
		if _, err := CreateSourceBranch(repo, sampleReport(v), overlayJSON, sampleOverlay(), targets); err == nil {
			t.Fatalf("%s candidate must never reach source", v)
		}
	}
	res, err := CreateSourceBranch(repo, passReport(), overlayJSON, sampleOverlay(), targets)
	if !errors.Is(err, ErrNoSourceEdits) || len(res.Plan.Skips) != 1 {
		t.Fatalf("err=%v plan=%+v", err, res.Plan)
	}
	if out := git(t, repo, "branch", "--list", "improve/*"); out != "" {
		t.Fatalf("a failed attempt left a branch: %q", out)
	}
	if n := len(strings.Split(git(t, repo, "worktree", "list"), "\n")); n != 1 {
		t.Fatal("leftover worktree")
	}
}

func TestCreateSourceBranchNeverOverwrites(t *testing.T) {
	repo := newRepo(t)
	commitFile(t, repo, "src/server.go", "package s\n\nvar desc = \""+oldD+"\"\n")
	targets := []SourceTarget{{Tool: "t", Old: oldD, New: newD}}
	if _, err := CreateSourceBranch(repo, passReport(), overlayJSON, sampleOverlay(), targets); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateSourceBranch(repo, passReport(), overlayJSON, sampleOverlay(), targets); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second attempt: %v", err)
	}
}
