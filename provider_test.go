package main

import (
	"testing"

	"github.com/rhun-editor/extension-protocol/go/rhunext"
)

// apply makes edits (sorted, not overlapping) on text
func apply(text string, edits []rhunext.TextEdit) string {
	out, last := "", 0
	for _, e := range edits {
		a, b := offsetOf([]byte(text), e.Range.Start), offsetOf([]byte(text), e.Range.End)
		out += text[last:a] + e.Text
		last = b
	}
	return out + text[last:]
}

func TestHunks(t *testing.T) {
	cases := []struct {
		old, new string
		n        int // hunks
	}{
		{"a\nb\nc\n", "a\nb\nc\n", 0},
		{"a\nbb\nc\n", "a\nb\nc\n", 1},                    // one line in the middle
		{"func  main( ) {\n}\n", "func main() {\n}\n", 1}, // the first line
		{"x", "x\n", 1},                                   // a final newline added
		{"a\nb\n\n\n", "a\nb\n", 1},                       // lines removed at the end
		{"a\nX\nc\nY\ne\n", "a\nx\nc\ny\ne\n", 2},         // two apart
		{"a\nb\nc\n", "a\nb2\nc2\n", 2},                   // two in a row: one each
		{"{\nx\n}\n", "{\n\tx,\n\ty\n}\n", 1},             // one line becomes two
	}
	for _, c := range cases {
		hs := hunks([]byte(c.old), []byte(c.new))
		if len(hs) != c.n {
			t.Errorf("%q -> %q: %d hunks, want %d: %v", c.old, c.new, len(hs), c.n, hs)
		}
		var edits []rhunext.TextEdit
		for _, h := range hs {
			edits = append(edits, h.edit)
		}
		if got := apply(c.old, edits); got != c.new {
			t.Errorf("%q: the edits give %q, want %q", c.old, got, c.new)
		}
	}
}

func TestPartialHunks(t *testing.T) {
	old, new := "a\nX\nc\nY\ne\n", "a\nx\nc\ny\ne\n"
	var kept []rhunext.TextEdit
	for _, h := range hunks([]byte(old), []byte(new)) {
		if h.touches(4, 5) {
			kept = append(kept, h.edit)
		}
	}
	if got := apply(old, kept); got != "a\nX\nc\ny\ne\n" {
		t.Errorf("lines 4-5 only: %q", got)
	}
	if line := openingLine([]byte("f {\n\tif x {\n\t}\n}"), 15); line != 1 {
		t.Errorf("the } on line 4 closes line %d's {", line)
	}
}
