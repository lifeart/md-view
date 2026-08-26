# md-view — Architecture

A cross-platform markdown viewer (macOS first). Double-click a `.md` file → it opens instantly, rendered and readable. Select and copy as plain text, follow links between markdown files with back/forward navigation, adjust color/font — defaults to a clean white, readable theme.

## Requirements

1. **Open on click** — OS file association for `.md`/`.markdown`; the file renders immediately.
2. **Very fast to open** — perceived cold launch to rendered content well under half a second; this constraint drives the stack choice below.
3. **Copy as text by default** — selecting and copying yields plain text, not rich HTML; link URLs copyable via context menu.
4. **Cross-document navigation** — relative links to other `.md` files open in-app; back/forward buttons and shortcuts; `#anchor` links scroll in place; `http(s)` links open in the default browser.
5. **Appearance control** — theme (light default: white background), font family and size are user-adjustable and persisted.
6. **Cross-platform** — macOS is the priority; Windows/Linux from the same codebase.
7. **Lightweight development** — the toolchain must not eat multiple GB of RAM (this rules out a Rust/Tauri core; see alternatives).

## Stack: Wails v2, with rendering done in Go

**Shell:** [Wails v2](https://wails.io) (Go core + the OS's system webview — WKWebView on macOS, WebView2 on Windows, WebKitGTK on Linux). 
**Markdown pipeline:** entirely on the Go side — `goldmark` (parsing) → `chroma` via the goldmark-highlighting extension (syntax highlighting, class-based output) → `bluemonday` (HTML sanitization). See [GitHub feature parity](#github-feature-parity) for what sits on top of goldmark's GFM. 
**Frontend:** a deliberately thin, framework-free layer — one HTML shell, one CSS file of theme variables, and a few KB of vanilla TypeScript for link interception, history, copy handling, and settings. Two exceptions load lazily and only when a document needs them: KaTeX for math and Mermaid for diagrams.

Why this shape:

- **Launch speed.** The system webview is a shared OS framework (already warm in memory on macOS), and the binary is ~16 MB (of which ~4 MB is the KaTeX and Mermaid chunks, which are never on the launch path). Markdown→HTML happens in Go before the webview paints — no JS framework boot, no client-side parser on the critical path. `goldmark` (the engine behind Hugo) renders megabyte-sized documents in tens of milliseconds.
- **Light development.** `gopls` runs in a few hundred MB (vs 2–4 GB for rust-analyzer), `go build` takes seconds with no multi-GB target directory, and compile memory stays flat. Daily iteration on theming/UX is TS/CSS via `wails dev` hot reload with no Go rebuild at all.
- **File associations are first-class on macOS.** Declared in `wails.json` under `info.fileAssociations` (ext, name, icon, `role: "Viewer"` → `CFBundleTypeRole`), delivered at runtime through the `Mac.OnFileOpen(filePaths []string)` callback — both at launch and while running.
- **Native copy/selection behavior for free.** WKWebView gives macOS-native text selection, context menus, and services; we only override the copy payload to plain text.

### Alternatives considered

| Option | Verdict |
|---|---|
| Tauri 2 (Rust) | Equivalent runtime story (system webview, backend rendering), but the dev toolchain is heavy: rust-analyzer at 2–4 GB, parallel `rustc` jobs spiking ~1 GB each, 2–4 GB `target/` dirs. Rejected on requirement 7. |
| Electron | Best-known DX, but ~150 MB bundle and ~1s+ cold start — fails the "very fast to open" requirement. |
| Native SwiftUI + cmark-gfm | Fastest possible on macOS, but means a second (and third) codebase for Windows/Linux. Not justified for a viewer. |
| Wails + JS-side renderer (markdown-it/Shiki) | Puts parsing and grammar loading on the webview's critical path; Shiki alone costs hundreds of ms on first load. Go-side rendering keeps the frontend inert and fast. |
| Wails v3 | Has built-in file associations too, but still alpha. Start on stable v2; migrate later if v3 stabilizes with something we need. |

## Component overview

```
┌────────────────────────── Wails app ──────────────────────────┐
│  Go core                                                      │
│  ├─ open-file entry: Mac.OnFileOpen (macOS),                  │
│  │    argv + SingleInstanceLock/OnSecondInstanceLaunch (Win/  │
│  │    Linux and repeat launches)                              │
│  ├─ render pipeline: goldmark(GFM) → chroma → bluemonday      │
│  ├─ asset server middleware: serves rendered docs + local     │
│  │    images, scoped to the current document's directory      │
│  ├─ settings store (JSON in os.UserConfigDir())               │
│  └─ file watcher (fsnotify) → auto-refresh       [milestone]  │
│                          │ bound methods + runtime events     │
│  Webview (WKWebView)     ▼                                    │
│  ├─ index.html — toolbar (◀ ▶, title, appearance menu)        │
│  ├─ theme.css — CSS custom properties, light theme default    │
│  └─ main.ts — link interception, history stack,               │
│               copy-as-plain-text, theme/font application      │
└───────────────────────────────────────────────────────────────┘
```

### Opening a file (the fast path)

1. Finder launches the app; the path arrives via `OnFileOpen` (macOS) or argv (Windows/Linux). `OnFileOpen` can fire before the frontend is ready, so the Go side buffers pending opens and always remembers the current document.
2. The Wails asset server middleware intercepts the initial shell request (`/` or `/index.html`) and rewrites it before serving. The persisted appearance settings are **always** inlined — a `data-theme` attribute plus font-size/width/family CSS variable overrides on `<html>` (values HTML-escaped; they are user-controlled strings) — so first paint carries the correct theme with no flash. When a document is already known (a buffered open, or the current document on a webview reload), its rendered, bluemonday-sanitized HTML is also inlined into the content container, together with the window/document title and `data-doc-path`/`data-doc-dir` attributes.
3. First paint is the finished, correctly themed document. The TS layer hydrates afterward: it reads `data-doc-path`, seeds its state (current path, first history entry, title) **without** calling `RenderDocument`, and passes the inlined path to `Ready(inlinedPath)`. Go then skips delivering the buffered open for that same path but still delivers any open that arrived later; `Ready("")` (nothing inlined) flushes buffered opens as `doc:open` events, and — with nothing pending either — re-delivers the current document so a webview reload cannot lose it.
4. Subsequent navigations call a bound Go method (`RenderDocument(path)`) and swap the content in place — no page reload, JS state (history stack) survives.

Performance budget: cold launch → window **< 250 ms** (measured ~200 ms; content ~360 ms), warm open into the resident instance **< 200 ms** (measured ~150 ms: event → render → DOM commit → invisible present → painted frame → reveal), in-app navigation to another document **< 50 ms**, 1 MB file parse+render **< 400 ms** (typical prose ~150 ms measured).

The app is **resident**: `HideWindowOnClose` keeps the process alive when the window is closed (Cmd+Q quits), so subsequent document opens skip process spawn and WebKit init entirely — LaunchServices delivers the file to the running instance and the frontend commits the DOM and calls a single native present primitive — the window is ordered front at imperceptible alpha so the suspended compositor applies the new content, then revealed with a 50 ms native alpha fade. Visible-window document swaps run through document.startViewTransition (engine-managed 50 ms crossfade — atomic, no intermediate frame to flicker). The document is cleared whenever the window is genuinely hidden (close, Cmd+H, minimize — checked natively, so mere occlusion keeps it) and restored via the same transition on re-show (~140 ms measured). The restore is deliberately second-class: macOS un-hides the app a few ms *before* it delivers a file-open to it (8–17 ms measured), so `visibilitychange: visible` and `doc:open` arrive together in either order. A restore started at once would take a newer navigation token than the open and put the previous document back instead of the one just opened — so the restore waits a 50 ms grace, yields to any render already in flight, and every render re-checks its token inside the (view-transition-deferred) commit, not just before it. An optional login item (`scripts/prewarm.sh`, launches the app with `--hidden`) makes the first open of a session warm too: `--hidden` boots everything but keeps the window gated (`hiddenUntilOpen`) until the first open. Syntax highlighting is capped: any single code fence larger than 50 KB renders as a plain escaped `<pre><code>` block (no chroma) — chroma tokenization costs seconds per megabyte, so the cap keeps pathological code-heavy documents bounded.

#### Getting the window on screen

macOS swallows the order-front AppKit issues during `applicationWillFinishLaunching` when the app was launched by opening a document (the LaunchServices `odoc` Apple Event a Finder double-click sends): AppKit reports the window as visible while the window server keeps it off screen for ~5 s, until the LaunchServices check-in times out. Any order-front issued once the main loop is running clears it, so `OnStartup` — the first callback Wails makes, before it hands the main thread to `NSApplication` — re-asserts `WindowShow`; queued there it runs on the first main-loop turn. `Ready()` re-asserts once more as a safety net (`WindowShow` is idempotent).

The window therefore appears before the webview has painted, ~100 ms ahead of the document — the same ordering a launch with no document already had. The window's background colour is what shows in that gap (and wherever the web view has not drawn, e.g. during a live resize), so it tracks the persisted theme rather than a hard-coded white; `TestThemeBackgroundMatchesCSS` pins those colours to `theme.css`.

Measured on an M-series Mac, medians of 7 interleaved runs per arm, time from the launch request to the window being on screen (`CGWindowList`):

| Launch path | Show from `Ready()` | Show from `OnStartup` |
|---|---|---|
| Finder / file association (`odoc`) | 344 ms | **240 ms** |
| App launch, no document | 219 ms | 217 ms |
| Binary + argv (no LaunchServices) | 177 ms | 183 ms |

What is left is not ours: ~105 ms of LaunchServices + `exec` + dyld + Go package init (of which chroma's lexer and style registries are ~15 ms), ~95 ms of Wails setup and WKWebView creation, then the window server. The document itself paints ~130 ms after that, dominated by WKWebView's first navigation; serving the shell (render + inject) costs ~2.5 ms of it.

`MDVIEW_TRACE=<file>` makes the binary append timestamped launch milestones to that file — LaunchServices launches have no stderr to attach to, so this is the only way to time them from inside the process. The frontend reports its own milestones (doc:open received, render committed, visibility changes, clear/restore) into the same file through the bound `Trace` method, so a warm open can be read end to end: `OnFileOpen` → `doc:open` → `RenderDocument` → commit → `PresentWindow`.

Note: file associations only take effect for the **built/installed** app bundle, not under `wails dev` — during development, open files via `Cmd+O`/drag-and-drop or a CLI argument.

Per-platform delivery of the opened file:

| Platform | Association mechanism | Path delivery |
|---|---|---|
| macOS | `info.fileAssociations` in `wails.json` → bundle `Info.plist` | `Mac.OnFileOpen` callback (launch and while running) |
| Windows | Registered by the NSIS installer | argv on launch; `SingleInstanceLock.OnSecondInstanceLaunch` routes repeat opens to the running instance |
| Linux | No Wails bundling — manual `.desktop` + MIME XML shipped in a package (e.g. nfpm-built `.deb`) | argv / `OnSecondInstanceLaunch`, same as Windows |

### GitHub feature parity

The promise is that a `.md` file looks the way it looks on github.com. goldmark's
`extension.GFM` covers tables, strikethrough, autolinks and task lists — roughly
half of what GitHub actually renders. The rest is in `internal/render/`:

| Feature | Where | Note |
| --- | --- | --- |
| Alerts (`> [!NOTE]`) | `alerts.go` | A paragraph transformer (runs during block parsing, while the marker is still a plain source line) tags the blockquote; a post-parse walk adds the title. Styling and the Octicon are CSS. |
| Footnotes | `extension.Footnote` | Not part of `extension.GFM`. |
| Math (`$…$`, `$$…$$`, ```` ```math ````) | `math.go` + `math.ts` | Go only *marks* it, emitting `<code class="language-math">` — the same hook GitHub emits — with the TeX source as text. KaTeX typesets it in the webview. |
| Diagrams (```` ```mermaid ````) | `diagrams.ts` | goldmark already emits `<code class="language-mermaid">`; Mermaid replaces it. |
| Emoji (`:tada:`) | `goldmark-emoji` | Rendered as unicode, not an `<img>` — no network, and it copies as text. |
| YAML front matter | `frontmatter.go` | Peeled off before parsing (otherwise it becomes an `<hr>` plus a mangled setext heading) and shown as a table, as GitHub does. The YAML parse is deliberately shallow; anything it cannot read is shown verbatim. |
| Heading anchors | `slug.go` | GitHub's slugger, not goldmark's: goldmark drops non-ASCII (`## Über uns` → `#ber-uns`) and slugs the *raw* line, so markup leaks into the id. |
| Table column alignment | `render.go` | goldmark defaults to `style="text-align:…"`, which bluemonday strips — so `WithTableCellAlignMethod(TableCellAlignAttribute)` emits `align` instead. |
| `<kbd>`, `<picture>`, `<div align>`, `<ol start>`, `<a name>` | `buildPolicy` | All silently eaten by `bluemonday.UGCPolicy` before; all common in real READMEs. |
| `<video>` / `<audio>` | `buildPolicy` | `src`/`poster` go through the same scope-checked asset route as images (`http.ServeFile` handles range requests). `autoplay` is deliberately not allowed. |

Two rules shape the split between Go and the webview:

- **Nothing that can be done in Go happens in JS.** Only math and diagrams need a
  library, and both would blow the launch budget if they were on the critical
  path. So Go emits a marked-up, *readable* placeholder (the TeX or diagram
  source), the document paints without waiting, and `enhanceRichContent` imports
  the library afterwards — only when the document contains one. If the import
  never lands, the reader still sees the source rather than a blank space.
- **Both entry paths must enhance.** A document reaches the DOM either through
  `renderInto` (a `doc:open` into a live window) or inlined into the shell by the
  Go middleware on a cold launch. `enhanceRichContent` is called from *both*; a
  fix that only covers `renderInto` means every double-clicked document shows raw
  TeX. `enhanceCodeBlocks` skips the `<pre>` blocks holding math and diagrams for
  the same reason — it runs first, and the copy button it adds would be stranded
  when the typeset output replaces the block.

Both of those are real bugs that got past `go test`, `tsc` and a green build,
because the Go tests stop at the HTML `internal/render` emits and nothing else
ran the bundle. `scripts/e2e-frontend.sh` closes that seam: it serves the built
`frontend/dist` with the Wails binding surface stubbed, drives it headless
through the cold-launch inline path, a `doc:open`, and a theme change, and
asserts what the reader would actually see. It runs in CI after `wails build`.

Untrusted input reaches two libraries in the webview rather than the Go
sanitizer, so both are pinned rather than left on their defaults: KaTeX with
`trust: false` (no `\href`/`\includegraphics`, which would build a link or an
image downstream of bluemonday), plus `maxExpand`/`maxSize` against macro and
layout bombs; Mermaid with `securityLevel: 'strict'` and `maxTextSize`/`maxEdges`.

### Link handling rules

All clicks are intercepted in `main.ts` and routed:

| Link | Behavior |
|---|---|
| `#heading` | Scroll within the current document (slugs generated at render time). |
| Relative/absolute path to `.md`/`.markdown` | Resolve against the current file's directory in Go, render, swap content in place, push a history entry. Supports `file.md#section`. |
| `http(s)://…` | Open in the default browser (`runtime.BrowserOpenURL`). Never loaded in-app. |
| Other local files (PDFs, etc.) | Open with the system default app (`open` on macOS and equivalents). Files that look executable (any `+x` bit, or a runnable-content extension such as `.sh`/`.app`/`.pkg`) are never launched — the user gets a notice and the file is revealed in the file manager instead. |
| Images (`<img>` relative src) | Rewritten at render time to an asset-server route (`/doc-asset/…`); the handler validates every request against the current document's directory scope. |

### Navigation history

An in-app stack per window: `{ path, scrollY, anchor }`. Back/forward via toolbar buttons, `Cmd+[` / `Cmd+]` (Alt+arrows on Win/Linux), and mouse buttons 4/5. Scroll position is captured on leave and restored on back. The stack lives in the TS layer; documents re-render on revisit (rendering is cheap enough that caching HTML is unnecessary initially).

### Clipboard behavior

- A `copy` event handler replaces the payload with the selection's plain text (`text/plain` only) — this is the default. A settings toggle can re-enable rich copy later.
- Context menu on links: **Copy Link Address** (absolute resolved URL for local targets, original URL for external).
- Code blocks get a hover "copy" button that copies raw code text.

### Theming and settings

- All colors/typography flow through CSS custom properties on `:root`; changing a setting flips variables — no re-render. Chroma emits class-based highlighting, so code themes flip with the same variables.
- **Default theme:** white background (`#ffffff`), near-black text (`#1f2328`), system font stack (`-apple-system, BlinkMacSystemFont, "Segoe UI", …`), 16 px base, line-height 1.6, content column ~72 ch centered. GitHub-like, optimized for reading.
- Settings: theme (light / dark / sepia / follow-system), font family, font size (`Cmd+=` / `Cmd+-`), content width. Persisted as JSON under `os.UserConfigDir()/md-view/` and inlined into the initial page response, so they apply before first paint.
- `theme: system` respects `prefers-color-scheme`.

### Security model

Rendered markdown is untrusted input:

- `bluemonday` strips scripts, iframes, event handlers, and dangerous URLs from the rendered HTML (raw HTML in markdown is sanitized, not passed through). The policy is widened only where GitHub parity requires it — chroma's classes, alert/footnote/math classes, heading and footnote ids, `<kbd>`, `<picture>`, `align`, `<ol start>`, `<a name>` — never for `style` or event handlers. One attribute needs care: bluemonday policies `src`/`href` as URLs but does **not** recognise `srcset`, so `render.srcsetPattern` does that job, admitting only http(s) URLs and scheme-less, non-protocol-relative paths.
- Strict CSP in the webview: no remote scripts/styles; remote images allowed (settable), everything else local.
- Filesystem access is scoped: the asset server and `RenderDocument` refuse any path outside the opened document's directory tree (extended per navigation) — the webview can never read arbitrary paths.
- Bound-method surface is minimal: `RenderDocument(path)`, `ResolveLink(base, href)`, `GetSettings()/SetSettings()`, `Ready(inlinedPath)`, `OpenFileDialog()`, `OpenExternal(url)` (http/https only), `OpenWithSystemDefault(path)` (scope-checked, executables refused and revealed instead), `PresentWindow()`, `IsWindowHidden()`, and `Trace(msg)` (appends a frontend milestone to the `MDVIEW_TRACE` file; no-op otherwise).

## Project layout

```
md-view/
├── main.go              # bootstrap, OnFileOpen, single instance, argv
├── scripts/
│   ├── e2e-warm-open.sh # the OS un-hide vs. doc:open race, against the real app
│   └── e2e-frontend.sh  # the built bundle, headless, both document entry paths
├── internal/
│   ├── render/          # goldmark(GFM+) → chroma → bluemonday
│   │                    #   alerts.go, math.go, slug.go, frontmatter.go
│   ├── links/           # path resolution, scope checks, doc-asset routes
│   └── settings/
├── frontend/
│   ├── index.html       # shell + toolbar
│   ├── theme.css        # variables + default light theme
│   ├── main.ts          # links, history, copy, settings application
│   ├── math.ts          # lazy KaTeX  } imported only when the document
│   └── diagrams.ts      # lazy Mermaid} actually contains one
├── wails.json           # info.fileAssociations (md, markdown, mdown, mkd)
└── ARCHITECTURE.md
```

## Milestones

- **M0 — walking skeleton:** file association (packaged build) + argv open, GFM rendering with highlighting, default light theme, plain-text copy. (Requirements 1, 2, 3, 6 in basic form.)
- **M1 — navigation:** md-to-md links, anchors, back/forward with scroll restoration, external links to browser, relative images.
- **M2 — appearance & convenience:** settings UI + persistence (theme/font/size/width), `Cmd+O`, recent files menu, drag-and-drop onto window/dock.
- **M3 — polish:** file watcher with auto-refresh (preserving scroll), find-in-page (`Cmd+F`), outline/TOC sidebar, multi-window (one document per window, Preview-style), print/export to PDF.

Deliberately out of scope for now: editing, mermaid/math rendering (both need JS on the critical path — can be added later as lazy-loaded opt-ins), remote markdown URLs.
