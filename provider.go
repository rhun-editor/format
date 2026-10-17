package main

// Format Document as a provider (section 12h of the protocol): rhun's shift+alt+f asks the
// extension with the document's tab width and spaces, and the answer is one edit that replaces
// only the lines that changed, so the cursor and the undo step stay where the user works.

import (
	"bytes"

	"github.com/rhun-editor/extension-protocol/go/rhunext"
)

func (f *format) Format(ext *rhunext.Ext, req rhunext.FormatRequest) rhunext.WorkspaceEdit {
	var edit rhunext.WorkspaceEdit
	text, version, err := ext.Text(req.Doc)
	if err != nil || version != req.Version {
		return edit // the user typed meanwhile: rhun would refuse an edit for the old text
	}
	s := f.settings()
	if !f.jsonSet {
		// JSON follows the document's own indentation unless format.ini says otherwise
		s.indent, s.tabs = req.TabWidth, !req.Spaces
	}
	out, _, err := f.formatText(f.docs[req.Doc], text, s)
	if err != nil {
		ext.Toast("Format: " + err.Error())
		ext.Logf("%s: %v", f.docs[req.Doc].Path, err)
		return edit
	}
	if r, repl, ok := changedLines(text, out); ok {
		edit.AddDoc(req.Doc, req.Version, rhunext.TextEdit{Range: r, Text: repl})
	}
	return edit
}

// changedLines is the smallest run of whole lines of old that differ from new, and what replaces
// them; ok is false when the texts are equal.
func changedLines(old, new []byte) (rhunext.Range, string, bool) {
	if bytes.Equal(old, new) {
		return rhunext.Range{}, "", false
	}
	// the common prefix, back to a line start
	p := 0
	for p < len(old) && p < len(new) && old[p] == new[p] {
		p++
	}
	p = bytes.LastIndexByte(old[:p], '\n') + 1
	// the common suffix after it, forward to a line start (or the end)
	s := 0
	for s < len(old)-p && s < len(new)-p && old[len(old)-1-s] == new[len(new)-1-s] {
		s++
	}
	for s > 0 && old[len(old)-s-1] != '\n' {
		s--
	}
	start, _ := lineCol(old, p)
	end, endCol := lineCol(old, len(old)-s)
	r := rhunext.Range{Start: rhunext.Pos{Line: start, Col: 1}, End: rhunext.Pos{Line: end, Col: endCol}}
	return r, string(new[p : len(new)-s]), true
}
