# format

Format Document for [rhun](https://rhun.app). JSON is formatted by the extension itself; every
other language is handed to the formatter you already have installed, found on your `PATH`:
gofmt, rustfmt, ruff or black, prettier, clang-format, shfmt, zig fmt, nixfmt, terraform fmt,
sql-formatter, dart format, swift-format, ktfmt, stylua, rubocop, mix format, ormolu or
fourmolu, ocamlformat, buf, taplo. Nothing is downloaded.

Format Document: **Shift+Alt+F** (rhun asks the extension, with the document's tab width and
spaces; JSON follows them unless `format.ini` sets its own) or the extension's command
(Ctrl+Shift+I). Only the lines that changed are replaced, as one undo step; if you typed
meanwhile, nothing changes.

## Settings

`$RHUN_STATE/format.ini` (rhun shows the folder in Settings > Extensions):

```ini
[json]
indent = 2      # spaces per level
tabs = no       # tabs instead of spaces
[run]
timeout = 10    # seconds a formatter may take
```

## Needs

Documents: write (that is the point). Project: read (formatters read configuration files such
as `.prettierrc` or `rustfmt.toml` next to the file). Run: the formatters above, by name; no
network.

## Build and test

`tests/run.sh [RHUN]` builds the extension, lays it out as rhun installs it and runs
`tests/basic.txt` headlessly. Releases are built by the reusable workflow from
`rhun-editor/extension-template`.
