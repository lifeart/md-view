package main

import (
	"bytes"
	"fmt"
	"html"
	"net/http"
	"os"
	"strconv"
	"strings"

	"md-view/internal/render"
	"md-view/internal/settings"
)

// This file implements the "fast path" from ARCHITECTURE.md: the asset-server
// middleware intercepts the initial shell request and serves it with the
// persisted appearance settings always inlined (correct first-paint theme,
// no FOUC) and — when a document is already known — the rendered, sanitized
// document inlined into the content container. The frontend hydrates from the
// inlined state and reports the inlined path back via Ready(inlinedPath), so
// the buffered doc:open for that path is not delivered twice.

// isShellRequest reports whether the request is for the HTML shell.
func isShellRequest(r *http.Request) bool {
	return r.Method == http.MethodGet && (r.URL.Path == "/" || r.URL.Path == "/index.html")
}

// serveShell serves the shell response from next with the initial state
// injected. Non-200 responses from next are replayed unchanged.
func (a *App) serveShell(w http.ResponseWriter, r *http.Request, next http.Handler) {
	rec := &bufferedResponse{header: make(http.Header)}
	next.ServeHTTP(rec, r)
	if rec.statusOr200() != http.StatusOK {
		rec.replay(w)
		return
	}
	out, _ := injectInitialState(rec.body.Bytes(), a.renderInlineDoc(), a.inlineSettings())
	h := w.Header()
	for k, v := range rec.header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		h[k] = v
	}
	h.Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// renderInlineDoc renders the document to inline into the shell: the first
// buffered open if any, otherwise the current document (webview reload).
// On any failure it returns nil — the shell is then served without a document
// and Ready("") falls back to event delivery, where errors surface in the UI.
func (a *App) renderInlineDoc() *render.Doc {
	p := a.inlineDocPath()
	if p == "" {
		return nil
	}
	resolved, err := a.scope.Check(p)
	if err == nil {
		var doc render.Doc
		if doc, err = a.renderer.RenderFile(resolved); err == nil {
			return &doc
		}
	}
	fmt.Fprintf(os.Stderr, "md-view: cannot inline %s (falling back to event delivery): %v\n", p, err)
	return nil
}

// inlineSettings loads the persisted settings for first-paint inlining,
// falling back to defaults. A load error is not fatal here: the frontend's
// GetSettings call surfaces the same error in the UI right after boot.
func (a *App) inlineSettings() settings.Settings {
	if a.store == nil {
		return settings.Default()
	}
	s, err := a.store.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "md-view: settings unavailable for inlining (UI will report it): %v\n", err)
		return settings.Default()
	}
	return s
}

// injectInitialState rewrites the HTML shell: the appearance settings are
// always inlined (data-theme attribute + font/width CSS variable overrides on
// <html>), and when doc is non-nil its sanitized HTML, title and
// data-doc-path/-dir attributes replace the empty content container. All
// injected attribute values are HTML-escaped (settings are user-controlled
// strings; the document HTML itself is already bluemonday-sanitized).
// The returned bool reports whether the document was inlined.
func injectInitialState(shell []byte, doc *render.Doc, s settings.Settings) ([]byte, bool) {
	out := string(shell)

	const htmlMarker = `<html lang="en">`
	if strings.Contains(out, htmlMarker) {
		attrs := ` data-theme="` + html.EscapeString(s.Theme) +
			`" style="` + html.EscapeString(inlineStyle(s)) + `"`
		out = strings.Replace(out, htmlMarker, `<html lang="en"`+attrs+`>`, 1)
	} else {
		fmt.Fprintln(os.Stderr, "md-view: shell missing <html> marker; serving without inlined settings")
	}

	if doc == nil {
		return []byte(out), false
	}

	const openMarker = `<article id="content">`
	start := strings.Index(out, openMarker)
	end := -1
	if start >= 0 {
		if i := strings.Index(out[start:], "</article>"); i >= 0 {
			end = start + i
		}
	}
	if start < 0 || end < 0 {
		fmt.Fprintln(os.Stderr, "md-view: shell missing content markers; falling back to event delivery")
		return []byte(out), false
	}
	out = out[:start] +
		`<article id="content" data-doc-path="` + html.EscapeString(doc.Path) +
		`" data-doc-dir="` + html.EscapeString(doc.Dir) +
		`" data-doc-title="` + html.EscapeString(doc.Title) + `">` +
		doc.HTML +
		out[end:]

	out = strings.Replace(out, `<title>md-view</title>`,
		`<title>`+html.EscapeString(doc.Title)+` — md-view</title>`, 1)
	out = strings.Replace(out, `<div id="doc-title" title=""></div>`,
		`<div id="doc-title" title="`+html.EscapeString(doc.Path)+`">`+html.EscapeString(doc.Title)+`</div>`, 1)
	return []byte(out), true
}

// inlineStyle builds the CSS variable overrides mirroring what the frontend's
// applySettings sets. The font family is user-controlled text: values with
// CSS-structural characters are skipped here (JS applies them safely via
// setProperty after boot) so a hostile settings file cannot break out of the
// style attribute's declaration list.
func inlineStyle(s settings.Settings) string {
	style := fmt.Sprintf("--font-size:%dpx;--content-width:%dch", s.FontSize, s.ContentWidth)
	if ff := s.FontFamily; ff != "" && !strings.ContainsAny(ff, `;{}<>&"\`) {
		style += ";--font-family:" + ff
	}
	return style
}

// readyDeliveries decides which document paths to emit when the frontend
// reports readiness. inlined is the path the frontend hydrated from the
// served shell ("" when nothing was inlined). The first pending occurrence of
// the inlined path is skipped (it is already rendered in the shell), while
// opens that arrived later are still delivered in order. With nothing inlined
// and nothing pending, the current document is re-delivered so a webview
// reload cannot lose the open document.
func readyDeliveries(inlined string, pending []string, currentDoc string) []string {
	if inlined == "" {
		if len(pending) == 0 && currentDoc != "" {
			return []string{currentDoc}
		}
		return pending
	}
	out := make([]string, 0, len(pending))
	skipped := false
	for _, p := range pending {
		if !skipped && p == inlined {
			skipped = true
			continue
		}
		out = append(out, p)
	}
	return out
}

// bufferedResponse records a handler's response so it can be rewritten (or
// replayed unchanged) before reaching the real ResponseWriter.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }

func (b *bufferedResponse) WriteHeader(code int) {
	if b.status == 0 {
		b.status = code
	}
}

func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

func (b *bufferedResponse) statusOr200() int {
	if b.status == 0 {
		return http.StatusOK
	}
	return b.status
}

func (b *bufferedResponse) replay(w http.ResponseWriter) {
	h := w.Header()
	for k, v := range b.header {
		h[k] = v
	}
	w.WriteHeader(b.statusOr200())
	_, _ = w.Write(b.body.Bytes())
}
