package publish

import "strings"

// Long descriptions are rarely one literal. They are usually written as adjacent string
// literals wrapped over lines:
//
//	description: "Read the complete contents of a file. " +
//	    "Handles various text encodings. " +
//	    "Only works within allowed directories.",
//
// The planner reads such a run as one string, matches it against the description the server
// serves today, and writes the replacement back in the same layout.

type litTok struct {
	start, end int
	quote      byte
	val        string
}

// scanLiterals finds single-line "..." and '...' literals. Escapes it cannot decode make the
// token undecodable and it is dropped, so a run containing one never matches.
func scanLiterals(s string) []litTok {
	var toks []litTok
	for i := 0; i < len(s); {
		c := s[i]
		if c != '"' && c != '\'' {
			i++
			continue
		}
		var b strings.Builder
		j, ok, bad := i+1, false, false
		for j < len(s) && s[j] != '\n' {
			ch := s[j]
			if ch == '\\' {
				if j+1 >= len(s) {
					break
				}
				switch n := s[j+1]; n {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				case 'r':
					b.WriteByte('\r')
				case '\\', '"', '\'', '/':
					b.WriteByte(n)
				default:
					bad = true
				}
				j += 2
				continue
			}
			if ch == c {
				ok = true
				break
			}
			b.WriteByte(ch)
			j++
		}
		if !ok {
			i++
			continue
		}
		if !bad {
			toks = append(toks, litTok{start: i, end: j + 1, quote: c, val: b.String()})
		} else {
			// keep the position so a neighbouring run is split here instead of joined across it
			toks = append(toks, litTok{start: i, end: j + 1, quote: c, val: "\x00undecodable"})
		}
		i = j + 1
	}
	return toks
}

// literalRuns groups tokens into maximal runs of adjacent literals: only whitespace and at most
// one "+" between them. Maximal means a run that is part of a longer expression of literals is
// matched as a whole or not at all.
func literalRuns(s string, toks []litTok) [][]litTok {
	var runs [][]litTok
	for i := 0; i < len(toks); {
		run := []litTok{toks[i]}
		j := i + 1
		for j < len(toks) {
			gap := strings.Trim(s[toks[j-1].end:toks[j].start], " \t\r\n")
			if gap != "" && gap != "+" {
				break
			}
			run = append(run, toks[j])
			j++
		}
		runs = append(runs, run)
		i = j
	}
	return runs
}

func runValue(run []litTok) string {
	var b strings.Builder
	for _, t := range run {
		b.WriteString(t.val)
	}
	return b.String()
}

// findLiteralRunHits returns the runs in content whose joined text equals old.
func findLiteralRunHits(file, ext, content, old string) []hit {
	var out []hit
	for _, run := range literalRuns(content, scanLiterals(content)) {
		if runValue(run) != old {
			continue
		}
		run := run
		start, end := run[0].start, run[len(run)-1].end
		kind := quoteKind(run[0].quote)
		if len(run) > 1 {
			kind += ", wrapped over lines"
		}
		out = append(out, hit{
			file: file, start: start, end: end, ext: ext, kind: kind, quote: run[0].quote,
			line:   1 + strings.Count(content[:start], "\n"),
			render: func(newText string) string { return renderRun(content, run, ext, newText) },
		})
	}
	return out
}

// renderRun writes newText in the layout of the run it replaces: a single literal if the
// original was on one line, otherwise wrapped at about the original width with the same
// separator (for example ` +` and the same indentation) between pieces.
func renderRun(content string, run []litTok, ext, newText string) string {
	q := run[0].quote
	whole := func(s string) string { return string(q) + escapeFor(q, ext, s) + string(q) }
	if len(run) == 1 {
		return whole(newText)
	}
	sep := content[run[0].end:run[1].start]
	if !strings.Contains(sep, "\n") {
		return whole(newText)
	}
	width := 40
	for _, t := range run {
		if w := t.end - t.start; w > width {
			width = w
		}
	}
	chunks := wrapWords(newText, width-2)
	parts := make([]string, len(chunks))
	for i, c := range chunks {
		parts[i] = whole(c)
	}
	return strings.Join(parts, sep)
}

// wrapWords splits text into pieces of at most limit bytes where it can, always after a space,
// so that joining the pieces reproduces text exactly.
func wrapWords(text string, limit int) []string {
	var chunks []string
	cur := ""
	for _, w := range strings.SplitAfter(text, " ") {
		if cur != "" && len(cur)+len(w) > limit {
			chunks = append(chunks, cur)
			cur = ""
		}
		cur += w
	}
	if cur != "" {
		chunks = append(chunks, cur)
	}
	if len(chunks) == 0 {
		return []string{""}
	}
	return chunks
}
