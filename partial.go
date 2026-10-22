package main

// Formatting part of a document (protocol 1.1, feature partial-format). The formatters this
// extension runs only format whole files, so both requests format the whole text, compare it with
// the document line by line (hunks) and answer with the hunks that touch the lines asked for:
//
//   - Format Selection (range-format): the hunks that touch the selected lines;
//   - formatting as you type (type-format, with [editor] format_on_type = yes): after `}`, the
//     hunks from the line of its `{` to the `}`; after Enter, those of the line Enter ended, never
//     the line the cursor is on. Errors are quiet here: the code is half typed.

import (
	"bytes"
	"strings"

	"github.com/rhun-editor/extension-protocol/go/rhunext"
)

func (f *format) RangeFormat(ext *rhunext.Ext, req rhunext.RangeFormatRequest) rhunext.WorkspaceEdit {
	first, last := req.Range.Start.Line, req.Range.End.Line
	if req.Range.End.Col == 1 && last > first {
		last-- // a selection of whole lines ends at the start of the next one
	}
	return f.partial(ext, req.Doc, req.Version, req.TabWidth, req.Spaces, first, last, true)
}

func (f *format) TypeFormat(ext *rhunext.Ext, req rhunext.TypeFormatRequest) rhunext.WorkspaceEdit {
	var edit rhunext.WorkspaceEdit
	text, version, err := ext.Text(req.Doc)
	if err != nil || version != req.Version {
		return edit
	}
	switch req.Char {
	case "}":
		open := openingLine(text, offsetOf(text, req.At)-1)
		return f.partialText(ext, req.Doc, req.Version, text, req.TabWidth, req.Spaces, open, req.At.Line, false)
	case "newline":
		if req.At.Line < 2 {
			return edit
		}
		return f.partialText(ext, req.Doc, req.Version, text, req.TabWidth, req.Spaces, req.At.Line-1, req.At.Line-1, false)
	}
	return edit
}

func (f *format) partial(ext *rhunext.Ext, doc rhunext.Doc, version int64, tab int, spaces bool, first, last int, report bool) rhunext.WorkspaceEdit {
	text, v, err := ext.Text(doc)
	if err != nil || v != version {
		return rhunext.WorkspaceEdit{}
	}
	return f.partialText(ext, doc, version, text, tab, spaces, first, last, report)
}

// partialText formats text and keeps the hunks that touch lines first to last; report says
// whether a failure is shown (an explicit command) or kept quiet (typing).
func (f *format) partialText(ext *rhunext.Ext, doc rhunext.Doc, version int64, text []byte, tab int, spaces bool, first, last int, report bool) rhunext.WorkspaceEdit {
	var edit rhunext.WorkspaceEdit
	out, err := f.formatFor(doc, text, tab, spaces)
	if err != nil {
		if report {
			f.failed(ext, doc, err)
		}
		return edit
	}
	if report {
		f.succeeded(ext, doc)
	}
	var edits []rhunext.TextEdit
	for _, h := range hunks(text, out) {
		if h.touches(first, last) {
			edits = append(edits, h.edit)
		}
	}
	if len(edits) > 0 {
		edit.AddDoc(doc, version, edits...)
	}
	return edit
}

// openingLine is the line of the `{` that the `}` at offset at closes, counting braces back (an
// example's approximation: braces in strings and comments count too); the `}`'s own line when
// there is none.
func openingLine(text []byte, at int) int {
	depth := 0
	for i := at; i >= 0 && i < len(text); i-- {
		switch text[i] {
		case '}':
			depth++
		case '{':
			depth--
			if depth == 0 {
				line, _ := lineCol(text, i)
				return line
			}
		}
	}
	line, _ := lineCol(text, at)
	return line
}

func offsetOf(text []byte, p rhunext.Pos) int {
	off := 0
	for line := 1; line < p.Line && off < len(text); line++ {
		i := bytes.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	return min(off+p.Col-1, len(text))
}

// hunk is a run of old lines [a1, a2) that the formatted text replaces, as an edit of the old text.
type hunk struct {
	a1, a2 int // zero-based old lines
	edit   rhunext.TextEdit
}

// touches says whether the hunk changes one of the one-based lines first to last; an insertion
// between two lines touches both.
func (h hunk) touches(first, last int) bool {
	lo, hi := h.a1+1, h.a2
	if h.a1 == h.a2 {
		lo, hi = h.a1, h.a1+1
	}
	return lo <= last && hi >= first
}

// hunks compares old and new line by line (a longest common subsequence of the lines that differ
// after the common start and end) and returns the changed runs, one per line where a run replaces
// as many lines as it has, so a hunk is as small as it can be.
func hunks(old, new []byte) []hunk {
	a, b := splitLines(old), splitLines(new)
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	am, bm := a[p:len(a)-s], b[p:len(b)-s]
	if len(am) == 0 && len(bm) == 0 {
		return nil
	}
	// pairs of equal lines in the middle, in order; one big hunk when the table would be too big
	var pairs [][2]int
	if len(am)*len(bm) <= 4_000_000 {
		pairs = lcs(am, bm)
	}
	starts := lineStarts(old)
	var out []hunk
	add := func(a1, a2, b1, b2 int) {
		if a1 == a2 && b1 == b2 {
			return
		}
		if a2-a1 == b2-b1 {
			for k := 0; k < a2-a1; k++ {
				out = append(out, makeHunk(old, starts, p+a1+k, p+a1+k+1, bm[b1+k:b1+k+1]))
			}
			return
		}
		out = append(out, makeHunk(old, starts, p+a1, p+a2, bm[b1:b2]))
	}
	i, j := 0, 0
	for _, pr := range pairs {
		add(i, pr[0], j, pr[1])
		i, j = pr[0]+1, pr[1]+1
	}
	add(i, len(am), j, len(bm))
	return out
}

func makeHunk(old []byte, starts []int, a1, a2 int, lines []string) hunk {
	from, to := starts[a1], starts[a2]
	text := strings.Join(lines, "")
	// a run that ends in a line feed on both sides keeps it: the edit stops before the next line,
	// and a cursor at that line's start stays where it is
	if a2 > a1 && to > from && old[to-1] == '\n' && strings.HasSuffix(text, "\n") {
		to--
		text = text[:len(text)-1]
	}
	l1, c1 := lineCol(old, from)
	l2, c2 := lineCol(old, to)
	return hunk{a1, a2, rhunext.TextEdit{Range: rhunext.Range{Start: rhunext.Pos{Line: l1, Col: c1}, End: rhunext.Pos{Line: l2, Col: c2}}, Text: text}}
}

// splitLines cuts text after each line feed; the last line may lack one.
func splitLines(text []byte) []string {
	var out []string
	for len(text) > 0 {
		i := bytes.IndexByte(text, '\n') + 1
		if i == 0 {
			i = len(text)
		}
		out = append(out, string(text[:i]))
		text = text[i:]
	}
	return out
}

// lineStarts is the byte offset of each line of text, and its length at the end.
func lineStarts(text []byte) []int {
	starts := []int{0}
	for i, c := range text {
		if c == '\n' && i+1 < len(text) {
			starts = append(starts, i+1)
		}
	}
	if len(text) == 0 {
		return []int{0, 0}
	}
	return append(starts, len(text))
}

// lcs pairs equal lines of a and b, in order, as many as there can be.
func lcs(a, b []string) [][2]int {
	n, m := len(a), len(b)
	t := make([][]int32, n+1)
	for i := range t {
		t[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i][j] = t[i+1][j+1] + 1
			} else {
				t[i][j] = max(t[i+1][j], t[i][j+1])
			}
		}
	}
	var out [][2]int
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case a[i] == b[j]:
			out = append(out, [2]int{i, j})
			i++
			j++
		case t[i+1][j] >= t[i][j+1]:
			i++
		default:
			j++
		}
	}
	return out
}
