# md-view

A fast, minimal markdown viewer for macOS (Windows/Linux from the same codebase). Double-click a `.md` file and it opens instantly, rendered and readable — no editor chrome, no framework boot.

- **Fast:** rendering happens in Go (goldmark → chroma → bluemonday) before the webview paints; the first paint *is* the finished document. Cold launch to a live process measures ~0.1 s; a typical document renders in single-digit milliseconds.
- **Navigable:** relative links to other markdown files open in-app with back/forward (`Cmd+[` / `Cmd+]`), scroll restoration, and anchor support; `http(s)` links open in your browser.
- **Copy-friendly:** selecting and copying yields plain text; links offer "Copy Link Address"; code blocks have a hover copy button.
- **Readable by default:** white background, system font, ~72ch column. Light / dark / sepia / follow-system themes, font family/size and column width adjustable (`Cmd+=` / `Cmd+-`), persisted.
- **Safe:** all markdown HTML is sanitized; file access is scoped to the directories of documents you open; executables are never handed to the system opener.

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full design.

## Install

Build the DMG (or grab a built one), then:

1. Open `md-view-0.1.0.dmg` and drag **md-view** into **Applications**.
2. First launch of a locally-signed app may require right-click → **Open** once (Gatekeeper).

The very first launch after installing can take a few seconds while macOS verifies the new binary — every launch after that is fast (~0.1 s).

## Make it the default for all markdown files

File associations are registered by the app bundle, but macOS still needs you to pick the *default* handler once. Two ways:

**Finder (no terminal):** right-click any `.md` file → **Get Info** → *Open with:* choose **md-view** → click **Change All…**. Repeat for a `.markdown` file if you use that extension.

**Scripted (all extensions at once):**

```sh
swift scripts/set-default.swift            # uses /Applications/md-view.app
```

The script assigns md-view as default for `.md`, `.markdown`, `.mdown`, and `.mkd` via the same `NSWorkspace` API Finder uses, and prints the before/after handler for each type. (Alternative if you use [duti](https://github.com/moretension/duti): `duti -s com.wails.md-view net.daringfireball.markdown all`.)

To undo, use Finder's **Change All…** to point the types back at another app.

## Development

Requires Go 1.25+, Node 22+, and the Wails v2 CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

```sh
wails dev                  # live-reload dev session (also serves the UI to a browser)
go test ./...              # Go tests: render pipeline, links/scope, settings, shell inlining
cd frontend && npx tsc --noEmit   # frontend type check
```

Note: file associations only work for the built, installed bundle — under `wails dev`, open files via `Cmd+O`, drag-and-drop, or a CLI argument.

## Build & package

```sh
wails build                # production bundle at build/bin/md-view.app
```

DMG (compressed, with an Applications shortcut):

```sh
STAGE=$(mktemp -d) && cp -R build/bin/md-view.app "$STAGE/" && ln -s /Applications "$STAGE/Applications"
hdiutil create -volname "md-view" -srcfolder "$STAGE" -ov -format UDZO build/bin/md-view-0.1.0.dmg
```
