package main

// Why a file did not format, in the status bar (protocol 1.1, features status-items and
// command-args): an item in the error color that shows while that file is the active one (a when
// clause on its path), with the whole message as its tooltip. When the message names a line and a
// column, as JSON's and most formatters' do, a click runs goto_error with them and the cursor
// goes there. The next format of the file that works, or closing it, removes the item.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/rhun-editor/extension-protocol/go/rhunext"
)

// a position in a message: "line 2, column 9" (JSON here) or ":2:9" (gofmt, rustfmt, ruff …)
var (
	posWords = regexp.MustCompile(`line (\d+), column (\d+)`)
	posColon = regexp.MustCompile(`:(\d+):(\d+)`)
)

// failed says why a document did not format: in a toast, and unless no formatter applies, in the
// status bar.
func (f *format) failed(ext *rhunext.Ext, doc rhunext.Doc, err error) {
	ext.Toast("Format: " + err.Error())
	ext.Logf("%s: %v", f.docs[doc].Path, err)
	var u unavailable
	if errors.As(err, &u) || !ext.Has(rhunext.FeatureStatusItems) {
		return
	}
	msg := err.Error()
	text := "$(error) Format: " + msg
	if first, _, _ := strings.Cut(text, "\n"); len(first) > 100 {
		text = cut(first, 100) + "…"
	} else {
		text = first
	}
	item := rhunext.StatusItem{Name: "error", Align: "left", Priority: -10, Text: text, Tooltip: cut(msg, 500),
		Color: "error", When: whenPath(f.docs[doc].Path)}
	if m := posWords.FindStringSubmatch(msg); m != nil {
		item.Command, item.Args = "goto_error", []byte(m[1]+" "+m[2])
	} else if m := posColon.FindStringSubmatch(msg); m != nil {
		item.Command, item.Args = "goto_error", []byte(m[1]+" "+m[2])
	}
	if item.Command != "" {
		item.Tooltip = cut(msg, 470) + "\n\nClick to go there."
	}
	if err := ext.SetStatusItem(item); err != nil {
		ext.Logf("status item: %v", err)
		return
	}
	f.failing = doc
}

// succeeded clears the status bar's error when it was about this document.
func (f *format) succeeded(ext *rhunext.Ext, doc rhunext.Doc) {
	if f.failing == 0 || f.failing != doc {
		return
	}
	f.failing = 0
	ext.RemoveStatusItem("error")
}

// gotoError puts the cursor where the message said ("LINE COL" from the status item).
func (f *format) gotoError(ext *rhunext.Ext, doc rhunext.Doc, args []byte) {
	var p rhunext.Pos
	if _, err := fmt.Sscan(string(args), &p.Line, &p.Col); err != nil || doc == 0 || p.Line < 1 {
		return
	}
	if p.Col < 1 {
		p.Col = 1
	}
	if err := ext.Select(doc, rhunext.Range{Start: p, End: p}); err != nil {
		ext.Toast("Format: " + err.Error())
	}
}

// whenPath is a when clause that holds while the file at path is the active document; "" (always)
// for an untitled one or a path no quote can hold.
func whenPath(path string) string {
	switch {
	case path == "" || path == "-" || len(path) > 480:
		return ""
	case !strings.Contains(path, `"`):
		return `resourcePath == "` + path + `"`
	case !strings.Contains(path, "'"):
		return "resourcePath == '" + path + "'"
	}
	return ""
}

// cut shortens s to at most n bytes at a character boundary.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xc0 == 0x80 {
		n--
	}
	return s[:n]
}
