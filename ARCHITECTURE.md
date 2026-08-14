# md-view — Architecture

A cross-platform markdown viewer (macOS first). Double-click a `.md` file → it opens instantly, rendered and readable. Select and copy as plain text, follow links between markdown files with back/forward navigation, adjust color/font — defaults to a clean white, readable theme.

## Requirements

1. **Open on click** — OS file association for `.md`/`.markdown`; the file renders immediately.
2. **Very fast to open** — perceived cold launch to rendered content well under half a second; this constraint drives the stack choice below.
3. **Copy as text by default** — selecting and copying yields plain text, not rich HTML; link URLs copyable via context menu.
4. **Cross-document navigation** — relative links to other `.md` files open in-app; back/forward buttons and shortcuts; `#anchor` links scroll in place; `http(s)` links open in the default browser.
5. **Appearance control** — theme (light default: white background), font family and size are user-adjustable and persisted.
6. **Cross-platform** — macOS is the priority; Windows/Linux from the same codebase.

## Stack recommendation: Tauri 2, with rendering done in Rust

**Shell:** Tauri 2 (Rust core + the OS's system webview — WKWebView on macOS). 
**Markdown pipeline:** entirely on the Rust side — `pulldown-cmark` (GFM parsing) → `syntect` (syntax highlighting) → `ammonia` (HTML sanitization). 
**Frontend:** a deliberately thin, framework-free layer — one HTML shell, one CSS file of theme variables, and a few KB of vanilla TypeScript for link interception, history, copy handling, and settings.

Why this shape:

- **Launch speed.** WKWebView is a shared system framework (already warm in memory on macOS), and the binary is ~5 MB. Doing markdown→HTML in Rust means the webview receives finished HTML — no JS framework boot, no client-side parser download/parse/execute on the critical path. `pulldown-cmark` renders megabyte-sized documents in single-digit milliseconds.
- **Native copy/selection behavior for free.** WKWebView gives macOS-native text selection, context menus, and services; we only override the copy payload to plain text.
- **One codebase, three platforms.** Tauri handles bundling, file associations, and single-instance behavior on all three OSes.

### Alternatives considered

| Option | Verdict |
|---|---|
| Electron | Best-known DX, but ~150 MB bundle and ~1s+ cold start — fails the "very fast to open" requirement. |
| Native SwiftUI + cmark-gfm | Fastest possible on macOS, but means a second (and third) codebase for Windows/Linux. Not justified for a viewer. |
| Tauri + JS-side renderer (markdown-it/Shiki) | Works, but puts parsing and grammar loading on the webview's critical path; Shiki alone costs hundreds of ms on first load. Rust-side rendering keeps the frontend inert and fast. |

## Component overview

```
┌────────────────────────── Tauri app ──────────────────────────┐
│  Rust core (src-tauri)                                        │
│  ├─ open-file events (Apple Events on macOS, argv elsewhere)  │
│  ├─ render pipeline: pulldown-cmark → syntect → ammonia       │
│  ├─ path resolution + fs scope (per-document directory)       │
│  ├─ settings store (JSON in app config dir)                   │
│  └─ file watcher (notify) → auto-refresh          [milestone] │
│                          │ IPC (invoke/events)                │
│  Webview (WKWebView)     ▼                                    │
│  ├─ index.html — toolbar (◀ ▶, title, appearance menu)        │
│  ├─ theme.css — CSS custom properties, light theme default    │
│  └─ main.ts — link interception, history stack,               │
│               copy-as-plain-text, theme/font application      │
└───────────────────────────────────────────────────────────────┘
```

### Opening a file (the fast path)

1. Finder launches the app with the document (macOS `RunEvent::Opened`; argv + single-instance plugin on Windows/Linux).
2. Rust reads the file, renders sanitized HTML, and injects it into the window's initial page load together with inline critical CSS.
3. First paint is the finished document. The TS layer hydrates afterward (event listeners only — no DOM rebuilding).

Performance budget: cold launch → content **< 400 ms**, warm launch **< 150 ms**, in-app navigation to another document **< 50 ms**, 1 MB file parse+render **< 100 ms**.

### Link handling rules

All clicks are intercepted in `main.ts` and routed:

| Link | Behavior |
|---|---|
| `#heading` | Scroll within the current document (slugs generated at render time). |
| Relative/absolute path to `.md`/`.markdown` | Resolve against the current file's directory in Rust, render, swap content in place, push a history entry. Supports `file.md#section`. |
| `http(s)://…` | Open in the default browser (opener plugin). Never loaded in-app. |
| Other local files (PDFs, etc.) | Open with the system default app. |
| Images (`<img>` relative src) | Rewritten at render time to the Tauri asset protocol; fs scope is extended to the document's directory on open. |

### Navigation history

An in-app stack per window: `{ path, scrollY, anchor }`. Back/forward via toolbar buttons, `Cmd+[` / `Cmd+]` (Alt+arrows on Win/Linux), and mouse buttons 4/5. Scroll position is captured on leave and restored on back. The stack lives in the TS layer; documents re-render on revisit (rendering is cheap enough that caching HTML is unnecessary initially).

### Clipboard behavior

- A `copy` event handler replaces the payload with the selection's plain text (`text/plain` only) — this is the default. A settings toggle can re-enable rich copy later.
- Context menu on links: **Copy Link Address** (absolute resolved URL for local targets, original URL for external).
- Code blocks get a hover "copy" button that copies raw code text.

### Theming and settings

- All colors/typography flow through CSS custom properties on `:root`; changing a setting flips variables — no re-render.
- **Default theme:** white background (`#ffffff`), near-black text (`#1f2328`), system font stack (`-apple-system, BlinkMacSystemFont, "Segoe UI", …`), 16 px base, line-height 1.6, content column ~72 ch centered. GitHub-like, optimized for reading.
- Settings: theme (light / dark / sepia / follow-system), font family, font size (`Cmd+=` / `Cmd+-`), content width. Persisted as JSON via the Tauri store plugin in the app config directory and applied before first paint (defaults inlined so an unset config costs nothing).
- `theme: system` respects `prefers-color-scheme`.

### Security model

Rendered markdown is untrusted input:

- `ammonia` strips scripts, iframes, event handlers, and dangerous URLs from the rendered HTML (raw HTML in markdown is sanitized, not passed through).
- Strict CSP in the webview: no remote scripts/styles; remote images allowed (settable), everything else local.
- Filesystem access is scoped: only the opened document's directory tree is readable, extended per navigation — the webview can never read arbitrary paths.
- IPC surface is minimal: `render(path)`, `resolve_link(base, href)`, `get/set_settings`.

## Project layout

```
md-view/
├── src-tauri/
│   ├── src/
│   │   ├── main.rs        # bootstrap, open-file events, single instance
│   │   ├── render.rs      # pulldown-cmark → syntect → ammonia
│   │   ├── links.rs       # path resolution, image src rewriting, scopes
│   │   └── settings.rs
│   ├── tauri.conf.json    # fileAssociations (md, markdown, mdown, mkd), CSP
│   └── Cargo.toml
├── ui/
│   ├── index.html         # shell + toolbar
│   ├── theme.css          # variables + default light theme
│   └── main.ts            # links, history, copy, settings application
└── ARCHITECTURE.md
```

## Milestones

- **M0 — walking skeleton:** file association + argv open, GFM rendering with highlighting, default light theme, plain-text copy. (Requirements 1, 2, 3, 6 in basic form.)
- **M1 — navigation:** md-to-md links, anchors, back/forward with scroll restoration, external links to browser, relative images.
- **M2 — appearance & convenience:** settings UI + persistence (theme/font/size/width), `Cmd+O`, recent files menu, drag-and-drop onto window/dock.
- **M3 — polish:** file watcher with auto-refresh (preserving scroll), find-in-page (`Cmd+F`), outline/TOC sidebar, multi-window (one document per window, Preview-style), print/export to PDF.

Deliberately out of scope for now: editing, mermaid/math rendering (both need JS on the critical path — can be added later as lazy-loaded opt-ins), remote markdown URLs.
