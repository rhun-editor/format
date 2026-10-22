#!/bin/sh
# Builds format for this machine, lays it out as rhun installs it, and runs each tests/NAME.txt
# headlessly against a rhun binary, comparing what it prints with tests/NAME.expected. gofmt must
# be on PATH (it comes with Go).
#   basic    Format Document on JSON, invalid JSON, Go and a file it has no formatter for
#   partial  protocol 1.1: Format Selection, formatting as you type, rhun's format on save, the
#            status bar's error and its click
# usage: tests/run.sh [RHUN [NAME...]]   (UPDATE=1 rewrites the .expected files; while the SDK
# is unreleased, GOWORK=… names a go.work that uses a checkout of it)
set -e
cd "$(dirname "$0")/.."
RHUN=${1:-$(command -v rhun || echo "$HOME/.local/bin/rhun")}
[ $# -gt 0 ] && shift
names=${*:-basic partial}
case "$(uname -s)-$(uname -m)" in
    Linux-x86_64) target=linux-x86_64 ;;
    Darwin-arm64) target=macos-arm64 ;;
    *) echo "no target for $(uname -s)-$(uname -m)"; exit 77 ;;
esac
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
dir=$work/exts/rhun-editor/format
mkdir -p "$dir/bin"
CGO_ENABLED=0 go build -o "$dir/bin/format-$target" .
cp extension.ini "$dir/"
fail=0
for name in $names; do
    t=$work/$name
    mkdir -p "$t/proj" "$t/config/rhun" "$t/state"
    case $name in
    basic)
        printf '{"name":"Ahmed","projects":[{"name":"Rhun","status":"active"},{"n":1}]}' > "$t/proj/data.json"
        printf '{"a": 1,\n  "b": }\n' > "$t/proj/bad.json"
        printf 'package main\n\nimport "fmt"\n\nfunc  main( ) {\nfmt.Println("hi")\n}\n' > "$t/proj/main.go"
        printf 'just text\n' > "$t/proj/notes.txt"
        printf 'package main\n\nimport "fmt"\n\nfunc  main( ) {\nfmt.Println("hi")\n}\n\n// the end\n' > "$t/proj/main2.go"
        printf '{"a":1,"b":[true]}' > "$t/proj/data2.json"
        extra=
        ;;
    partial)
        printf 'package main\n\nfunc  a( ) {\nx:=1\n}\n\nfunc  b( ) {\ny:=2\n}\n' > "$t/proj/part.go"
        printf 'package main\n\nfunc  main( ) {\n}\n' > "$t/proj/save.go"
        printf '{"a": 1,\n  "b": }\n' > "$t/proj/bad.json"
        extra='[editor]\nformat_on_type = yes\nauto_pairs = false\n'
        ;;
    esac
    printf '[files]\nrestore_session = false\nrestore_project = false\nformat_on_save = true\n[updates]\ncheck = false\n[git]\nenabled = false\n[ui]\nsidebar = false\nagents_panel = false\n'"$extra" > "$t/config/rhun/config"
    sed "s|proj/|$t/proj/|" "tests/$name.txt" > "$t/$name.txt"
    HOME=$t XDG_CONFIG_HOME=$t/config XDG_STATE_HOME=$t/state PATH="$PATH:$(go env GOROOT)/bin" \
        "$RHUN" "$t/proj" --headless 1000x600 --scale 1 --extensions "$work/exts" --script "$t/$name.txt" 2>&1 \
        | sed "s/pid=[1-9][0-9]*/pid=PID/; s|$t/proj/|proj/|g" > "$t/out.txt"
    [ -n "$UPDATE" ] && cp "$t/out.txt" "tests/$name.expected"
    if cmp -s "$t/out.txt" "tests/$name.expected"; then
        echo "ok   $name"
    else
        echo "FAIL $name"; diff "tests/$name.expected" "$t/out.txt" || true; fail=1
    fi
done
exit $fail
