package main

// Format Document as a provider (section 12h of the protocol): rhun's shift+alt+f asks the
// extension with the document's tab width and spaces, and the answer replaces only the lines that
// changed (partial.go's hunks), so the cursor and the undo step stay where the user works. rhun's
// own [files] format_on_save = true asks the same before it writes the file.

import (
	"math"

	"github.com/rhun-editor/extension-protocol/go/rhunext"
)

func (f *format) Format(ext *rhunext.Ext, req rhunext.FormatRequest) rhunext.WorkspaceEdit {
	return f.partial(ext, req.Doc, req.Version, req.TabWidth, req.Spaces, 1, math.MaxInt, true)
}

// formatFor formats a document's text with its settings: format.ini's, else for JSON the
// document's own tab width and spaces.
func (f *format) formatFor(doc rhunext.Doc, text []byte, tab int, spaces bool) ([]byte, error) {
	s := f.settings()
	if !f.jsonSet {
		s.indent, s.tabs = tab, !spaces
	}
	out, _, err := f.formatText(f.docs[doc], text, s)
	return out, err
}
