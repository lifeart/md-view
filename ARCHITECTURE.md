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
**Markdown pipeline:** entirely on the Go side — `goldmark` with the GFM extension (parsing) → `chroma` via the goldmark-highlighting extension (syntax highlighting, class-based output) → `bluemonday` (HTML sanitization). 
**Frontend:** a deliberately thin, framework-free layer — one HTML shell, one CSS file of theme variables, and a few KB of vanilla TypeScript for link interception, history, copy handling, and settings.

Why this shape:

- **Launch speed.** The system webview is a shared OS framework (already warm in memory on macOS), and the binary is ~10 MB. Markdown→HTML happens in Go before the webview paints — no JS framework boot, no client-side parser on the critical path. `goldmark` (the engine behind Hugo) renders megabyte-sized documents in tens of milliseconds.
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

1. Finder launches the app; the path arrives via `OnFileOpen` (macOS) or argv (Windows/Linux). `OnFileOpen` can fire before the frontend is ready, so the Go side buffers the "current document" path.
2. The Wails asset server middleware intercepts the initial page request and returns the HTML shell **with the rendered, sanitized document already inlined**, plus critical CSS and the persisted theme variables.
3. First paint is the finished document. The TS layer hydrates afterward (event listeners only — no DOM rebuilding).
4. Subsequent navigations call a bound Go method (`RenderDocument(path)`) and swap the content in place — no page reload, JS state (history stack) survives.

Performance budget: cold launch → content **< 400 ms**, warm launch **< 150 ms**, in-app navigation to another document **< 50 ms**, 1 MB file parse+render **< 400 ms** (typical prose ~150 ms measured). Syntax highlighting is capped: any single code fence larger than 50 KB renders as a plain escaped `<pre><code>` block (no chroma) — chroma tokenization costs seconds per megabyte, so the cap keeps pathological code-heavy documents bounded.

Note: file associations only take effect for the **built/installed** app bundle, not under `wails dev` — during development, open files via `Cmd+O`/drag-and-drop or a CLI argument.

Per-platform delivery of the opened file:

| Platform | Association mechanism | Path delivery |
|---|---|---|
| macOS | `info.fileAssociations` in `wails.json` → bundle `Info.plist` | `Mac.OnFileOpen` callback (launch and while running) |
| Windows | Registered by the NSIS installer | argv on launch; `SingleInstanceLock.OnSecondInstanceLaunch` routes repeat opens to the running instance |
| Linux | No Wails bundling — manual `.desktop` + MIME XML shipped in a package (e.g. nfpm-built `.deb`) | argv / `OnSecondInstanceLaunch`, same as Windows |

### Link handling rules

All clicks are intercepted in `main.ts` and routed:

| Link | Behavior |
|---|---|
| `#heading` | Scroll within the current document (slugs generated at render time). |
| Relative/absolute path to `.md`/`.markdown` | Resolve against the current file's directory in Go, render, swap content in place, push a history entry. Supports `file.md#section`. |
| `http(s)://…` | Open in the default browser (`runtime.BrowserOpenURL`). Never loaded in-app. |
| Other local files (PDFs, etc.) | Open with the system default app (`open` on macOS and equivalents). |
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

- `bluemonday` strips scripts, iframes, event handlers, and dangerous URLs from the rendered HTML (raw HTML in markdown is sanitized, not passed through). The policy allows chroma's `span` classes and heading-anchor ids.
- Strict CSP in the webview: no remote scripts/styles; remote images allowed (settable), everything else local.
- Filesystem access is scoped: the asset server and `RenderDocument` refuse any path outside the opened document's directory tree (extended per navigation) — the webview can never read arbitrary paths.
- Bound-method surface is minimal: `RenderDocument(path)`, `ResolveLink(base, href)`, `GetSettings()/SetSettings()`.

## Project layout

```
md-view/
├── main.go              # bootstrap, OnFileOpen, single instance, argv
├── internal/
│   ├── render/          # goldmark(GFM) → chroma → bluemonday
│   ├── links/           # path resolution, scope checks, doc-asset routes
│   └── settings/
├── frontend/
│   ├── index.html       # shell + toolbar
│   ├── theme.css        # variables + default light theme
│   └── main.ts          # links, history, copy, settings application
├── wails.json           # info.fileAssociations (md, markdown, mdown, mkd)
└── ARCHITECTURE.md
```

## Milestones

- **M0 — walking skeleton:** file association (packaged build) + argv open, GFM rendering with highlighting, default light theme, plain-text copy. (Requirements 1, 2, 3, 6 in basic form.)
- **M1 — navigation:** md-to-md links, anchors, back/forward with scroll restoration, external links to browser, relative images.
- **M2 — appearance & convenience:** settings UI + persistence (theme/font/size/width), `Cmd+O`, recent files menu, drag-and-drop onto window/dock.
- **M3 — polish:** file watcher with auto-refresh (preserving scroll), find-in-page (`Cmd+F`), outline/TOC sidebar, multi-window (one document per window, Preview-style), print/export to PDF.

Deliberately out of scope for now: editing, mermaid/math rendering (both need JS on the critical path — can be added later as lazy-loaded opt-ins), remote markdown URLs.
