// format: Format Document for rhun (shift+alt+f asks it, provider.go; its own command too), Format
// Selection and formatting as you type (partial.go). JSON is formatted here; every other language
// runs the formatter the user has installed, found on PATH and named in extension.ini's `run`
// list. A file that does not format shows why in the status bar (status.go).
// Settings: $RHUN_STATE/format.ini (section 7 of the README).
// Protocol: https://rhun.app/extensions/v1/ and its 1.1 features (partial-format, status-items,
// command-args).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rhun-editor/extension-protocol/go/rhunext"
)

// formatter is an external program that reads the text on stdin and writes the result on stdout.
type formatter struct {
	program string
	args    []string // {path} is replaced by the file's path (for tools that want the name)
}

// byLang maps rhun's grammar names (spaces as underscores) to the formatter to try, in order.
var byLang = map[string][]formatter{
	"Go":               {{"gofmt", nil}},
	"Rust":             {{"rustfmt", []string{"--emit", "stdout", "--edition", "2021"}}},
	"Python":           {{"ruff", []string{"format", "-", "--stdin-filename", "{path}"}}, {"black", []string{"-q", "-", "--stdin-filename", "{path}"}}},
	"JavaScript":       {{"prettier", []string{"--stdin-filepath", "{path}"}}},
	"TypeScript":       {{"prettier", []string{"--stdin-filepath", "{path}"}}},
	"CSS":              {{"prettier", []string{"--stdin-filepath", "{path}"}}},
	"HTML":             {{"prettier", []string{"--stdin-filepath", "{path}"}}},
	"Markdown":         {{"prettier", []string{"--stdin-filepath", "{path}"}}},
	"YAML":             {{"prettier", []string{"--stdin-filepath", "{path}"}}},
	"C":                {{"clang-format", []string{"--assume-filename={path}"}}},
	"C++":              {{"clang-format", []string{"--assume-filename={path}"}}},
	"Shell":            {{"shfmt", []string{"-filename", "{path}"}}},
	"Zig":              {{"zig", []string{"fmt", "--stdin"}}},
	"Nix":              {{"nixfmt", nil}},
	"Terraform":        {{"terraform", []string{"fmt", "-"}}},
	"HCL":              {{"terraform", []string{"fmt", "-"}}},
	"SQL":              {{"sql-formatter", nil}},
	"Dart":             {{"dart", []string{"format"}}},
	"Swift":            {{"swift-format", nil}},
	"Kotlin":           {{"ktfmt", []string{"-"}}},
	"Lua":              {{"stylua", []string{"-"}}},
	"Ruby":             {{"rubocop", []string{"-a", "--stdin", "{path}", "--stderr", "-f", "quiet"}}},
	"Elixir":           {{"mix", []string{"format", "-"}}},
	"Haskell":          {{"ormolu", nil}, {"fourmolu", nil}},
	"OCaml":            {{"ocamlformat", []string{"--name", "{path}", "-"}}},
	"Protobuf":         {{"buf", []string{"format", "-"}}},
	"Protocol_Buffers": {{"buf", []string{"format", "-"}}},
	"TOML":             {{"taplo", []string{"format", "-"}}},
}

type settings struct {
	indent  int  // JSON: spaces per level
	tabs    bool // JSON: tabs instead
	timeout time.Duration
}

type format struct {
	docs    map[rhunext.Doc]rhunext.Opened
	state   string
	jsonSet bool        // format.ini set the JSON indentation (else a provider request's settings count)
	failing rhunext.Doc // the document the status bar's error is about, 0 for none
}

// unavailable is a file no formatter here can format: said in a toast, not in the status bar.
type unavailable string

func (u unavailable) Error() string { return string(u) }

func (f *format) Ready(ext *rhunext.Ext, project string) {
	f.docs = map[rhunext.Doc]rhunext.Opened{}
	f.state = os.Getenv("RHUN_STATE")
}

func (f *format) Event(ext *rhunext.Ext, ev rhunext.Event) {
	switch ev.Name {
	case "opened":
		if o, ok := rhunext.ParseOpened(ev); ok {
			f.docs[o.Doc] = o
		}
	case "closed":
		if len(ev.Args) > 0 {
			var d int64
			fmt.Sscan(ev.Args[0], &d)
			delete(f.docs, rhunext.Doc(d))
			f.succeeded(ext, rhunext.Doc(d))
		}
	}
}

// Run is a command without command-args (a host that did not agree it).
func (f *format) Run(ext *rhunext.Ext, command string, doc rhunext.Doc) {
	f.Invoke(ext, rhunext.Invocation{Command: command, Doc: doc})
}

// Invoke is every invocation of a command, with where it came from and its body.
func (f *format) Invoke(ext *rhunext.Ext, inv rhunext.Invocation) {
	switch inv.Name() {
	case "format_document":
		f.formatDocument(ext, inv.Doc)
	case "goto_error":
		f.gotoError(ext, inv.Doc, inv.Body)
	}
}

func (f *format) formatDocument(ext *rhunext.Ext, doc rhunext.Doc) {
	if doc == 0 {
		ext.Toast("Nothing to format")
		return
	}
	info := f.docs[doc]
	text, version, err := ext.Text(doc)
	if err != nil {
		ext.Toast("Format: " + err.Error())
		return
	}
	s := f.settings()
	out, how, err := f.formatText(info, text, s)
	if err != nil {
		f.failed(ext, doc, err)
		return
	}
	f.succeeded(ext, doc)
	if bytes.Equal(out, text) {
		ext.Status("")
		return
	}
	if _, err := ext.Replace(doc, version, rhunext.WholeRange(text), out); err != nil {
		ext.Toast("Format: the document changed meanwhile")
		return
	}
	ext.Toast("Formatted with " + how)
}

// formatText returns the formatted text and the name of what did it.
func (f *format) formatText(info rhunext.Opened, text []byte, s settings) ([]byte, string, error) {
	lang := info.Lang
	if lang == "" && info.Path != "" {
		switch strings.ToLower(filepath.Ext(info.Path)) {
		case ".json", ".jsonc", ".json5":
			lang = "JSON"
		}
	}
	if lang == "JSON" {
		out, err := formatJSON(text, s)
		return out, "JSON", err
	}
	cands, ok := byLang[lang]
	if !ok || lang == "" {
		return nil, "", unavailable(fmt.Sprintf("no formatter for %s", orDash(lang)))
	}
	for _, c := range cands {
		if _, err := exec.LookPath(c.program); err != nil {
			continue
		}
		out, err := runFormatter(c, info.Path, text, s.timeout)
		return out, c.program, err
	}
	names := make([]string, len(cands))
	for i, c := range cands {
		names[i] = c.program
	}
	return nil, "", unavailable(strings.Join(names, " or ") + " not installed")
}

func runFormatter(c formatter, path string, text []byte, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := make([]string, len(c.args))
	for i, a := range c.args {
		args[i] = strings.ReplaceAll(a, "{path}", path)
	}
	cmd := exec.CommandContext(ctx, c.program, args...)
	cmd.Stdin = bytes.NewReader(text)
	if path != "" {
		cmd.Dir = filepath.Dir(path)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		return nil, fmt.Errorf("%s: %s", c.program, msg)
	}
	return out.Bytes(), nil
}

// formatJSON re-indents JSON, keeping key order. Invalid JSON is an error with its position.
func formatJSON(text []byte, s settings) ([]byte, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		if se, ok := err.(*json.SyntaxError); ok {
			// Offset counts the byte at fault: the position is the one before it
			line, col := lineCol(text, int(se.Offset)-1)
			return nil, fmt.Errorf("invalid JSON at line %d, column %d: %s", line, col, se.Error())
		}
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("invalid JSON: more than one value")
	}
	indent := strings.Repeat(" ", s.indent)
	if s.tabs {
		indent = "\t"
	}
	var buf bytes.Buffer
	// encoding/json's Indent keeps the input's key order; Marshal would sort it
	if err := json.Indent(&buf, bytes.TrimSpace(text), "", indent); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func lineCol(text []byte, off int) (int, int) {
	line, col := 1, 1
	for i := 0; i < off && i < len(text); i++ {
		if text[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// settings reads $RHUN_STATE/format.ini:
//
//	[json]
//	indent = 2
//	tabs = no
//	[run]
//	timeout = 10     # seconds a formatter may take
func (f *format) settings() settings {
	s := settings{indent: 2, timeout: 10 * time.Second}
	f.jsonSet = false
	data, err := os.ReadFile(filepath.Join(f.state, "format.ini"))
	if err != nil {
		return s
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == '[' {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "indent":
			fmt.Sscan(v, &s.indent)
			f.jsonSet = true
		case "tabs":
			s.tabs = v == "yes" || v == "true"
			f.jsonSet = true
		case "timeout":
			var n int
			fmt.Sscan(v, &n)
			if n > 0 {
				s.timeout = time.Duration(n) * time.Second
			}
		}
	}
	if s.indent < 0 || s.indent > 16 {
		s.indent = 2
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "this file type"
	}
	return s
}

func main() {
	features := []string{rhunext.FeatureFormat, rhunext.FeaturePartialFormat, rhunext.FeatureStatusItems, rhunext.FeatureCommandArgs}
	if err := rhunext.RunWith(features, &format{}); err != nil {
		os.Exit(1)
	}
}
