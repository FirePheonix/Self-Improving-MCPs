package publish

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A gate-approved overlay changes what agents read at the proxy. The real fix is to change it
// where it is written, so the upstream server stops needing the overlay. PlanSourceEdits finds
// each tool's current description in a source tree and rewrites it, but only where it can do so
// exactly. It never guesses: a description it cannot place exactly is reported, not edited.

// SourceTarget is one description change to carry back into source.
type SourceTarget struct {
	Tool string
	Old  string // the description the upstream serves today
	New  string // the gate-approved description
}

// SourceEdit is one place a description was rewritten.
type SourceEdit struct {
	Tool string
	File string // slash-separated, relative to the scanned root
	Line int    // 1-based line where the old text starts
	Kind string // "double-quoted", "single-quoted", "backtick", "doc text"
	// Old and New are the description before and after, kept so the PR can show them verbatim.
	Old, New string
}

// SourceSkip is a target that was left alone, and why.
type SourceSkip struct {
	Tool   string
	Reason string
}

// SourcePlan is the outcome of PlanSourceEdits. Files holds the full new content of every
// changed file, keyed by the same relative path as the edits.
type SourcePlan struct {
	Edits []SourceEdit
	Skips []SourceSkip
	Files map[string][]byte
}

const (
	// minMatchLen keeps short, generic descriptions ("Read a file") from matching unrelated text.
	minMatchLen = 20
	// maxLocations: a description found in more places than this is ambiguous, so it is skipped.
	maxLocations = 5
	maxFileBytes = 1 << 20
)

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, ".venv": true, "venv": true, "__pycache__": true, ".next": true, "bin": true,
}

var codeExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".mjs": true, ".cjs": true, ".py": true,
	".rs": true, ".java": true, ".kt": true, ".cs": true, ".rb": true, ".json": true, ".yaml": true, ".yml": true, ".toml": true,
}

var docExts = map[string]bool{".md": true, ".mdx": true, ".txt": true, ".rst": true}

// PlanSourceEdits scans root and rewrites each target's description where it appears as an exact
// string literal (code) or as plain text (docs). Nothing is written to disk; the caller decides.
func PlanSourceEdits(root string, targets []SourceTarget) (SourcePlan, error) {
	files, err := loadTextFiles(root)
	if err != nil {
		return SourcePlan{}, err
	}
	plan := SourcePlan{Files: map[string][]byte{}}
	changed := map[string]bool{}

	for _, t := range targets {
		switch {
		case strings.TrimSpace(t.Old) == "":
			plan.Skips = append(plan.Skips, SourceSkip{t.Tool, "the upstream serves no description to look for"})
			continue
		case len(strings.TrimSpace(t.Old)) < minMatchLen:
			plan.Skips = append(plan.Skips, SourceSkip{t.Tool, fmt.Sprintf("current description is shorter than %d characters, too generic to match safely", minMatchLen)})
			continue
		case t.Old == t.New:
			plan.Skips = append(plan.Skips, SourceSkip{t.Tool, "description is already the approved text"})
			continue
		}

		var hits []hit
		for _, name := range sortedNames(files) {
			hits = append(hits, findHits(name, files[name], t.Old)...)
		}
		switch {
		case len(hits) == 0:
			plan.Skips = append(plan.Skips, SourceSkip{t.Tool, "current description not found as an exact string literal (it may be built from pieces, wrapped over lines, or generated); edit it by hand"})
			continue
		case len(hits) > maxLocations:
			plan.Skips = append(plan.Skips, SourceSkip{t.Tool, fmt.Sprintf("found in %d places, more than %d; too ambiguous to edit automatically", len(hits), maxLocations)})
			continue
		}
		if bad := unrepresentable(hits, t.New); bad != "" {
			plan.Skips = append(plan.Skips, SourceSkip{t.Tool, bad})
			continue
		}

		// apply per file from the end so earlier offsets stay valid
		byFile := map[string][]hit{}
		for _, h := range hits {
			byFile[h.file] = append(byFile[h.file], h)
		}
		for name, hs := range byFile {
			sort.Slice(hs, func(i, j int) bool { return hs[i].start > hs[j].start })
			content := files[name]
			for _, h := range hs {
				content = content[:h.start] + h.replacement(t.New) + content[h.end:]
			}
			files[name] = content
			changed[name] = true
		}
		for _, h := range hits {
			plan.Edits = append(plan.Edits, SourceEdit{Tool: t.Tool, File: h.file, Line: h.line, Kind: h.kind, Old: t.Old, New: t.New})
		}
	}

	for name := range changed {
		plan.Files[name] = []byte(files[name])
	}
	sort.Slice(plan.Edits, func(i, j int) bool {
		if plan.Edits[i].File != plan.Edits[j].File {
			return plan.Edits[i].File < plan.Edits[j].File
		}
		return plan.Edits[i].Line < plan.Edits[j].Line
	})
	return plan, nil
}

// hit is one exact occurrence of an old description.
type hit struct {
	file       string
	start, end int // byte range replaced (includes the quotes for a string literal)
	line       int
	kind       string
	quote      byte // 0 for plain doc text
	ext        string
	// render, when set, writes the replacement in the layout of what it replaces.
	render func(newText string) string
}

func (h hit) replacement(newText string) string {
	if h.render != nil {
		return h.render(newText)
	}
	if h.quote == 0 {
		return newText
	}
	return string(h.quote) + escapeFor(h.quote, h.ext, newText) + string(h.quote)
}

func findHits(file, content, old string) []hit {
	ext := strings.ToLower(filepath.Ext(file))
	var out []hit
	if codeExts[ext] {
		// "..." and '...' literals, including descriptions wrapped over several lines
		out = append(out, findLiteralRunHits(file, ext, content, old)...)
		// backtick strings: Go raw strings and JS template literals
		if ext != ".json" {
			pattern := "`" + escapeFor('`', ext, old) + "`"
			for _, at := range indexAll(content, pattern) {
				out = append(out, hit{file: file, start: at, end: at + len(pattern),
					line: 1 + strings.Count(content[:at], "\n"), kind: quoteKind('`'), quote: '`', ext: ext})
			}
		}
	}
	if docExts[ext] {
		for _, at := range indexAll(content, old) {
			out = append(out, hit{file: file, start: at, end: at + len(old),
				line: 1 + strings.Count(content[:at], "\n"), kind: "doc text", ext: ext})
		}
	}
	return out
}

func quoteKind(q byte) string {
	switch q {
	case '"':
		return "double-quoted"
	case '\'':
		return "single-quoted"
	}
	return "backtick"
}

// unrepresentable reports why newText cannot be written in the style of one of the hits.
func unrepresentable(hits []hit, newText string) string {
	for _, h := range hits {
		if h.quote == '`' && h.ext == ".go" && strings.Contains(newText, "`") {
			return fmt.Sprintf("%s:%d is a Go raw string, which cannot contain a backtick; edit it by hand", h.file, h.line)
		}
	}
	return ""
}

// escapeFor writes text so it reads back as the same string inside a literal delimited by quote.
func escapeFor(quote byte, ext, text string) string {
	if quote == '`' {
		if ext == ".go" {
			return text // Go raw strings take everything literally
		}
		r := strings.NewReplacer("\\", "\\\\", "`", "\\`", "${", "\\${")
		return r.Replace(text)
	}
	r := strings.NewReplacer("\\", "\\\\", string(quote), "\\"+string(quote), "\n", "\\n", "\r", "\\r", "\t", "\\t")
	return r.Replace(text)
}

func indexAll(s, sub string) []int {
	var out []int
	for from := 0; from <= len(s)-len(sub); {
		i := strings.Index(s[from:], sub)
		if i < 0 {
			break
		}
		out = append(out, from+i)
		from += i + len(sub)
	}
	return out
}

func sortedNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// loadTextFiles reads candidate source and doc files under root (relative, slash-separated keys).
func loadTextFiles(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !codeExts[ext] && !docExts[ext] {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > maxFileBytes || !d.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.ContainsRune(string(raw), 0) {
			return nil // binary
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	return files, err
}
