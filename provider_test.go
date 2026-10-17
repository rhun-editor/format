package main

import (
	"testing"

	"github.com/rhun-editor/extension-protocol/go/rhunext"
)

func TestChangedLines(t *testing.T) {
	cases := []struct {
		old, new string
		r        rhunext.Range
		repl     string
		ok       bool
	}{
		{"a\nb\nc\n", "a\nb\nc\n", rhunext.Range{}, "", false},
		// one line in the middle
		{"a\nbb\nc\n", "a\nb\nc\n", rhunext.Range{Start: rhunext.Pos{Line: 2, Col: 1}, End: rhunext.Pos{Line: 3, Col: 1}}, "b\n", true},
		// the first line
		{"func  main( ) {\n}\n", "func main() {\n}\n", rhunext.Range{Start: rhunext.Pos{Line: 1, Col: 1}, End: rhunext.Pos{Line: 2, Col: 1}}, "func main() {\n", true},
		// a final newline added
		{"x", "x\n", rhunext.Range{Start: rhunext.Pos{Line: 1, Col: 1}, End: rhunext.Pos{Line: 1, Col: 2}}, "x\n", true},
		// lines removed at the end
		{"a\nb\n\n\n", "a\nb\n", rhunext.Range{Start: rhunext.Pos{Line: 3, Col: 1}, End: rhunext.Pos{Line: 5, Col: 1}}, "", true},
	}
	for _, c := range cases {
		r, repl, ok := changedLines([]byte(c.old), []byte(c.new))
		if ok != c.ok || r != c.r || repl != c.repl {
			t.Errorf("%q -> %q: got %v %q %v, want %v %q %v", c.old, c.new, r, repl, ok, c.r, c.repl, c.ok)
		}
		if ok {
			// the edit applied to the old text gives the new one
			a, b := offset([]byte(c.old), r.Start), offset([]byte(c.old), r.End)
			if got := c.old[:a] + repl + c.old[b:]; got != c.new {
				t.Errorf("%q: the edit gives %q, want %q", c.old, got, c.new)
			}
		}
	}
}

// offset is the byte offset of a 1-based line and column
func offset(text []byte, p rhunext.Pos) int {
	line, i := 1, 0
	for i < len(text) && line < p.Line {
		if text[i] == '\n' {
			line++
		}
		i++
	}
	return i + p.Col - 1
}
