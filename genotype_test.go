package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rhun-editor/extension-protocol/go/rhunext"
)

// The copied test binary is a deterministic, portable stand-in for gt, including on Windows.
func TestMain(m *testing.M) {
	if os.Getenv("RHUN_FORMAT_GT_HELPER") == "1" {
		genotypeHelper()
		return
	}
	os.Exit(m.Run())
}

type gtCall struct {
	Args   []string
	Dir    string
	Files  []string
	Input  bool
	Source string
	Mode   uint32
}

func genotypeHelper() {
	mode := os.Getenv("RHUN_GT_TEST_MODE")
	cwd, _ := os.Getwd()
	call := gtCall{Args: os.Args[1:], Dir: cwd}
	entries, _ := os.ReadDir(".")
	for _, entry := range entries {
		call.Files = append(call.Files, entry.Name())
	}
	if data, err := os.ReadFile("input.type"); err == nil {
		call.Input, call.Source = true, string(data)
		info, _ := os.Stat("input.type")
		call.Mode = uint32(info.Mode().Perm())
	}
	trace, err := os.OpenFile(os.Getenv("RHUN_GT_TEST_TRACE"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(10)
	}
	json.NewEncoder(trace).Encode(call)
	trace.Close()
	if len(call.Args) != 1 {
		os.Exit(11)
	}
	switch call.Args[0] {
	case "--version":
		switch mode {
		case "version-fail":
			fmt.Fprintln(os.Stderr, "version failed")
			os.Exit(2)
		case "version-timeout":
			time.Sleep(5 * time.Second)
		case "wrong-tool":
			os.WriteFile("input.type", []byte("unrelated output"), 0600)
			fmt.Println("Graphite CLI 1.0.0")
			return
		}
		version := os.Getenv("RHUN_GT_TEST_VERSION")
		if version == "" {
			version = "gt 0.18.0"
		}
		fmt.Println(version)
	case "fmt":
		switch mode {
		case "fmt-fail":
			os.WriteFile("input.type", []byte("partial output"), 0600)
			fmt.Fprintln(os.Stderr, "\x1b[31msyntax error\x1b[0m\ninput.type:2:4")
			os.Exit(2)
		case "fmt-timeout":
			time.Sleep(5 * time.Second)
		case "missing-output":
			os.Remove("input.type")
			return
		}
		if !call.Input {
			os.Exit(12)
		}
		if err := os.WriteFile("input.type", []byte(strings.ToUpper(call.Source)), 0600); err != nil {
			os.Exit(13)
		}
		fmt.Println("Formatted 1 file(s)")
	default:
		os.Exit(14)
	}
}

func installGT(t *testing.T) (state, trace string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	state, trace = filepath.Join(root, "state"), filepath.Join(root, "trace.jsonl")
	for _, folder := range []string{bin, state} {
		if err := os.Mkdir(folder, 0700); err != nil {
			t.Fatal(err)
		}
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	name := "gt"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), data, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("RHUN_FORMAT_GT_HELPER", "1")
	t.Setenv("RHUN_GT_TEST_TRACE", trace)
	t.Setenv("RHUN_GT_TEST_MODE", "")
	t.Setenv("RHUN_GT_TEST_VERSION", "")
	return state, trace
}

func readGTCalls(t *testing.T, trace string) []gtCall {
	t.Helper()
	f, err := os.Open(trace)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var calls []gtCall
	decoder := json.NewDecoder(f)
	for {
		var call gtCall
		err := decoder.Decode(&call)
		if err == io.EOF {
			return calls
		}
		if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
}

func emptyGTState(t *testing.T, state string) {
	t.Helper()
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files remain: %v, %v", entries, err)
	}
}

func TestGenotypeBufferIsolation(t *testing.T) {
	state, trace := installGT(t)
	project := filepath.Join(t.TempDir(), "project café with spaces")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, "user.type")
	for _, name := range []string{"user.type", "other.type"} {
		if err := os.WriteFile(filepath.Join(project, name), []byte("saved text"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f := format{state: state}
	input := "User:{name:string,note:\"café\"}\n"
	out, how, err := f.formatText(rhunext.Opened{Lang: "Genotype", Path: path}, []byte(input), settings{timeout: time.Second})
	if err != nil || string(out) != strings.ToUpper(input) || how != "gt" {
		t.Fatalf("out=%q formatter=%q error=%v", out, how, err)
	}
	calls := readGTCalls(t, trace)
	if len(calls) != 2 || strings.Join(calls[0].Args, " ") != "--version" || calls[0].Input || len(calls[0].Files) != 0 {
		t.Fatalf("version must run before exposing buffer text: %+v", calls)
	}
	call := calls[1]
	if strings.Join(call.Args, " ") != "fmt" || call.Source != input || strings.Join(call.Files, ",") != "input.type" || call.Dir != calls[0].Dir {
		t.Fatalf("unexpected formatter input: %+v", calls)
	}
	resolved, _ := filepath.EvalSymlinks(state)
	parent, err := filepath.EvalSymlinks(filepath.Dir(call.Dir))
	relative, relErr := filepath.Rel(resolved, parent)
	if err != nil || relErr != nil || relative != "." {
		t.Fatalf("formatter cwd %q is outside state %q", call.Dir, resolved)
	}
	if runtime.GOOS != "windows" && call.Mode != 0600 {
		t.Fatalf("buffer permissions: %o", call.Mode)
	}
	for _, name := range []string{"user.type", "other.type"} {
		data, err := os.ReadFile(filepath.Join(project, name))
		if err != nil || string(data) != "saved text" {
			t.Fatalf("project file %s changed: %q, %v", name, data, err)
		}
	}
	emptyGTState(t, state)
}

func TestGenotypeFailuresDoNotReturnPartialOutput(t *testing.T) {
	for _, mode := range []string{"wrong-tool", "version-fail", "version-timeout", "fmt-fail", "fmt-timeout", "missing-output"} {
		t.Run(mode, func(t *testing.T) {
			state, trace := installGT(t)
			t.Setenv("RHUN_GT_TEST_MODE", mode)
			f := format{state: state}
			out, _, err := f.formatText(rhunext.Opened{Lang: "Genotype"}, []byte("User:string"), settings{timeout: 500 * time.Millisecond})
			if err == nil || out != nil {
				t.Fatalf("partial result on failure: %q, %v", out, err)
			}
			if strings.Contains(err.Error(), "\x1b") {
				t.Fatalf("terminal escape in error: %v", err)
			}
			if mode == "wrong-tool" {
				calls := readGTCalls(t, trace)
				if len(calls) != 1 || calls[0].Input {
					t.Fatalf("wrong program received buffer: %+v", calls)
				}
			}
			emptyGTState(t, state)
		})
	}
}

func TestGenotypeRoutingAndEmptyBuffer(t *testing.T) {
	for _, info := range []rhunext.Opened{{Lang: "Genotype"}, {Path: "/project/schema.type"}} {
		t.Run(info.Lang+info.Path, func(t *testing.T) {
			state, _ := installGT(t)
			out, _, err := (&format{state: state}).formatText(info, []byte{}, settings{timeout: time.Second})
			if err != nil || len(out) != 0 {
				t.Fatalf("empty buffer: %q, %v", out, err)
			}
			emptyGTState(t, state)
		})
	}
	t.Run("explicit-language", func(t *testing.T) {
		state, _ := installGT(t)
		_, _, err := (&format{state: state}).formatText(rhunext.Opened{Lang: "Plain", Path: "schema.type"}, []byte("text"), settings{timeout: time.Second})
		var missing unavailable
		if !errors.As(err, &missing) {
			t.Fatalf("explicit language was overridden: %v", err)
		}
		emptyGTState(t, state)
	})
}

func TestGenotypeNeedsStateAndCompatibleCLI(t *testing.T) {
	state, trace := installGT(t)
	for _, folder := range []string{"", filepath.Join(state, "missing")} {
		out, _, err := (&format{state: folder}).formatText(rhunext.Opened{Lang: "Genotype"}, []byte("User:string"), settings{timeout: time.Second})
		if err == nil || out != nil {
			t.Fatalf("unavailable state: %q, %v", out, err)
		}
	}
	if _, err := os.Stat(trace); !os.IsNotExist(err) {
		t.Fatalf("formatter started without a private state directory: %v", err)
	}
	t.Setenv("RHUN_GT_TEST_VERSION", "gt 0.17.9")
	out, _, err := (&format{state: state}).formatText(rhunext.Opened{Lang: "Genotype"}, []byte("User:string"), settings{timeout: time.Second})
	if err == nil || out != nil || len(readGTCalls(t, trace)) != 1 {
		t.Fatalf("old CLI accepted: %q, %v", out, err)
	}
	emptyGTState(t, state)
}

func TestGenotypeMissingCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	state := t.TempDir()
	out, _, err := (&format{state: state}).formatText(rhunext.Opened{Lang: "Genotype"}, []byte("User:string"), settings{timeout: time.Second})
	var missing unavailable
	if out != nil || !errors.As(err, &missing) || !strings.Contains(err.Error(), "gt not installed") {
		t.Fatalf("missing CLI: %q, %v", out, err)
	}
	emptyGTState(t, state)
}

func TestGenotypeVersionContract(t *testing.T) {
	for _, version := range []string{"gt 0.18.0\n", "gt 0.19.0", "gt 1.0.0", "gt 0.18.1-beta.1"} {
		if !supportedGenotype([]byte(version)) {
			t.Errorf("supported version rejected: %q", version)
		}
	}
	for _, version := range []string{"gt 0.17.9", "0.18.0", "Graphite CLI 1.0.0", "gt 0.18", "gt 0.18.0 extra", "gt 999999999999999999999.0.0"} {
		if supportedGenotype([]byte(version)) {
			t.Errorf("unsupported version accepted: %q", version)
		}
	}
}
