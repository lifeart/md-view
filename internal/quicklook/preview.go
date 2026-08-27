// Package quicklook renders a markdown file into one self-contained HTML
// document for the macOS Quick Look preview extension.
//
// Quick Look is the only way MDv can put a rendered document on screen in
// under 100 ms: the preview host process is already running and warm, so
// pressing Space in Finder skips every cost a cold launch pays — LaunchServices,
// dyld, AppKit, and WebKit's first navigation (see ARCHITECTURE.md, which
// measures those at ~158 ms of floor before MDv runs a line).
//
// The output has to be self-contained, for two reasons. The preview runs in a
// sandbox whose only guaranteed read access is the previewed file itself, so
// there is no asset server and no second request; and Quick Look's HTML
// previews do not execute JavaScript, so anything the app defers to the webview
// has to be either inlined or accepted as source text.
package quicklook

import (
	_ "embed"

	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	xhtml "golang.org/x/net/html"

	"md-view/internal/render"
	"md-view/internal/settings"
)

// maxInlineImageBytes caps a single embedded image. Base64 inflates by a third
// and the whole preview is one string handed across a process boundary, so a
// document full of screenshots must not turn into a hundred-megabyte reply. An
// image over the cap keeps its alt text, which is what a reader needs anyway.
const maxInlineImageBytes = 4 << 20 // 4 MB

//go:embed styles.css
var previewCSS string

// Render returns a complete HTML document for the markdown file at path, or an
// error page describing why it could not be read. It never returns an empty
// string: a blank Quick Look panel tells the reader nothing.
func Render(path string, dark bool) string {
	doc, err := render.New().RenderFile(path)
	if err != nil {
		return errorPage(path, err)
	}
	body := inlineImages(doc.HTML, filepath.Dir(path))
	theme := "light"
	if dark {
		theme = "dark"
	}
	var b strings.Builder
	b.Grow(len(body) + len(previewCSS) + 512)
	b.WriteString(`<!DOCTYPE html><html lang="en" data-theme="`)
	b.WriteString(theme)
	b.WriteString(`"><head><meta charset="utf-8">`)
	// No script-src at all: Quick Look does not run scripts in an HTML preview,
	// and saying so explicitly keeps that true if it ever changes.
	b.WriteString(`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; ` +
		`img-src data:; style-src 'unsafe-inline'; font-src data:">`)
	b.WriteString("<title>")
	b.WriteString(html.EscapeString(doc.Title))
	b.WriteString("</title><style>")
	b.WriteString(previewCSS)
	b.WriteString("</style></head><body><article>")
	b.WriteString(body)
	b.WriteString("</article></body></html>")
	return b.String()
}

// DefaultDark reports whether the preview should use the dark theme, following
// the same persisted setting the app uses so a preview does not contradict the
// window the reader is about to open. "system" cannot be resolved here — the
// preview host does not hand us an appearance — so it falls back to light,
// matching theme.css's own default.
func DefaultDark() bool {
	store, err := settings.NewStore()
	if err != nil {
		return false
	}
	s, err := store.Load()
	if err != nil {
		return false
	}
	return s.Theme == "dark"
}

// errorPage is what the reader sees instead of an empty panel.
func errorPage(path string, err error) string {
	return `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><style>` +
		previewCSS + `</style></head><body><article><p class="preview-error">` +
		html.EscapeString(fmt.Sprintf("Cannot preview %s: %v", filepath.Base(path), err)) +
		`</p></article></body></html>`
}

// inlineImages replaces the /doc-asset/ routes the renderer emits — which only
// resolve inside the running app — with data: URIs, so the preview needs no
// asset server and no second request. Anything that cannot be read (the sandbox
// may not grant access to files beside the previewed one) simply loses its src
// and keeps its alt text.
func inlineImages(sanitized, dir string) string {
	if !strings.Contains(sanitized, render.AssetRoutePrefix) {
		return sanitized
	}
	z := xhtml.NewTokenizer(strings.NewReader(sanitized))
	var b strings.Builder
	b.Grow(len(sanitized))
	cache := map[string]string{}
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			if z.Err() == io.EOF {
				return b.String()
			}
			return sanitized
		}
		raw := string(z.Raw())
		if tt == xhtml.StartTagToken || tt == xhtml.SelfClosingTagToken {
			tok := z.Token()
			if tok.Data == "img" || tok.Data == "source" {
				changed := false
				for i, attr := range tok.Attr {
					if attr.Namespace != "" || (attr.Key != "src" && attr.Key != "srcset") {
						continue
					}
					if uri, ok := dataURI(attr.Val, dir, cache); ok {
						tok.Attr[i].Val = uri
						changed = true
					}
				}
				if changed {
					b.WriteString(tok.String())
					continue
				}
			}
		}
		b.WriteString(raw)
	}
}

// dataURI turns one /doc-asset/ route back into a data: URI. It reports false
// for anything else (remote URLs are left alone; the CSP blocks them, which is
// the correct outcome for an offline preview).
func dataURI(value, dir string, cache map[string]string) (string, bool) {
	if !strings.HasPrefix(value, render.AssetRoutePrefix) {
		return "", false
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", false
	}
	abs := u.Query().Get("p")
	if abs == "" {
		return "", false
	}
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(dir, abs)
	}
	if cached, ok := cache[abs]; ok {
		return cached, cached != ""
	}
	data, ok := readCapped(abs)
	if !ok {
		cache[abs] = ""
		return "", false
	}
	ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(abs)))
	if ct == "" {
		ct = "application/octet-stream"
	}
	uri := "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data)
	cache[abs] = uri
	return uri, true
}

// readCapped reads a file, refusing anything over maxInlineImageBytes without
// reading it all first.
func readCapped(path string) ([]byte, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxInlineImageBytes {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, io.LimitReader(f, maxInlineImageBytes)); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}
