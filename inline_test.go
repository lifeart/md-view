package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"md-view/internal/render"
	"md-view/internal/settings"
)

// shellHandler serves the real committed shell (frontend/index.html) the way
// the embedded asset server does. Vite copies the body markup verbatim into
// frontend/dist, so testing against the source shell also pins the injection
// markers against drift.
func shellHandler(t *testing.T) http.Handler {
	t.Helper()
	shell, err := os.ReadFile("frontend/index.html")
	if err != nil {
		t.Fatalf("read shell: %v", err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(shell)
	})
}

func getShell(t *testing.T, app *App, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	app.assetMiddleware(shellHandler(t)).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// TestReadyDeliveries: F2/F5 — the Ready(inlinedPath) dedupe handshake.
func TestReadyDeliveries(t *testing.T) {
	for _, c := range []struct {
		name       string
		inlined    string
		pending    []string
		currentDoc string
		want       []string
	}{
		{"nothing inlined flushes pending", "", []string{"/a.md"}, "/a.md", []string{"/a.md"}},
		{"nothing inlined, nothing pending, no current", "", nil, "", nil},
		{"reload fallback re-delivers current doc", "", nil, "/cur.md", []string{"/cur.md"}},
		{"inlined + same pending is deduped", "/a.md", []string{"/a.md"}, "/a.md", nil},
		{"inlined + different pending still delivered", "/a.md", []string{"/b.md"}, "/b.md", []string{"/b.md"}},
		{"inlined + same then later open", "/a.md", []string{"/a.md", "/b.md"}, "/b.md", []string{"/b.md"}},
		{"only first occurrence of inlined is skipped", "/a.md", []string{"/a.md", "/b.md", "/a.md"}, "/a.md", []string{"/b.md", "/a.md"}},
		{"inlined current doc on reload, empty pending", "/cur.md", nil, "/cur.md", nil},
	} {
		got := readyDeliveries(c.inlined, c.pending, c.currentDoc)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: readyDeliveries(%q, %v, %q) = %v, want %v",
				c.name, c.inlined, c.pending, c.currentDoc, got, c.want)
		}
	}
}

// TestServeShellInlinesDocAndSettings: F2 + F4 — with a buffered open and a
// dark theme persisted, the served initial HTML already carries the rendered
// document, its data-doc attributes, and data-theme="dark".
func TestServeShellInlinesDocAndSettings(t *testing.T) {
	app := newTestApp()
	app.store = settings.NewStoreAt(filepath.Join(t.TempDir(), "settings.json"))
	if err := app.store.Save(settings.Settings{Theme: "dark", FontFamily: "", FontSize: 18, ContentWidth: 80}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	index := testdataAbs(t, "index.md")
	app.openPath(index)

	rec := getShell(t, app, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("shell status = %d", rec.Code)
	}
	body := rec.Body.String()

	// F4: theme + variables inlined for first paint.
	if !strings.Contains(body, `data-theme="dark"`) {
		t.Errorf("served shell missing data-theme=\"dark\":\n%.400s", body)
	}
	if !strings.Contains(body, "--font-size:18px") || !strings.Contains(body, "--content-width:80ch") {
		t.Errorf("served shell missing inlined font variables")
	}

	// F2: document inlined with hydration attributes and title.
	if !strings.Contains(body, fmt.Sprintf(`data-doc-path="%s"`, index)) {
		t.Errorf("served shell missing data-doc-path")
	}
	if !strings.Contains(body, fmt.Sprintf(`data-doc-dir="%s"`, filepath.Dir(index))) {
		t.Errorf("served shell missing data-doc-dir")
	}
	if !strings.Contains(body, `id="md-view-test-document"`) {
		t.Errorf("served shell missing rendered document content")
	}
	if !strings.Contains(body, "<title>md-view Test Document — MD View</title>") {
		t.Errorf("served shell missing inlined window title")
	}
	if strings.Contains(body, "No document open.") {
		t.Errorf("empty state must be replaced by the inlined document")
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(rec.Body.Bytes())) {
		t.Errorf("Content-Length = %s, body = %d", got, len(rec.Body.Bytes()))
	}

	// Handshake: the frontend hydrated the inlined path; the buffered open for
	// it must not be delivered again (Ready with zero deliveries is safe to
	// call without a Wails context).
	app.Ready(index)
	app.mu.Lock()
	pending := len(app.pending)
	current := app.currentDoc
	app.mu.Unlock()
	if pending != 0 {
		t.Errorf("pending after Ready = %d, want 0", pending)
	}
	if current != index {
		t.Errorf("currentDoc = %q, want %q", current, index)
	}
}

// TestServeShellWithoutDoc: no document known — the shell keeps its empty
// state, carries no data-doc-path, but still inlines the (default) settings.
func TestServeShellWithoutDoc(t *testing.T) {
	app := newTestApp()
	app.store = settings.NewStoreAt(filepath.Join(t.TempDir(), "settings.json"))

	rec := getShell(t, app, "/index.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("shell status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "data-doc-path") {
		t.Errorf("no document open, but shell carries data-doc-path")
	}
	if !strings.Contains(body, "No document open.") {
		t.Errorf("empty state missing from docless shell")
	}
	if !strings.Contains(body, `data-theme="light"`) {
		t.Errorf("default settings not inlined")
	}
}

// TestServeShellBypassesConditionalCache: a caching client revalidating the
// static shell must not get a 304 — that would replay a stale, un-injected
// copy. The middleware strips conditional headers and forbids caching.
func TestServeShellBypassesConditionalCache(t *testing.T) {
	app := newTestApp()
	app.store = settings.NewStoreAt(filepath.Join(t.TempDir(), "settings.json"))

	// next behaves like vite / http.ServeContent: matching If-None-Match → 304.
	const etag = `"shell-v1"`
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		shell, err := os.ReadFile("frontend/index.html")
		if err != nil {
			t.Errorf("read shell: %v", err)
		}
		_, _ = w.Write(shell)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("If-None-Match", etag)
	req.Header.Set("If-Modified-Since", "Mon, 01 Jan 2024 00:00:00 GMT")
	rec := httptest.NewRecorder()
	app.assetMiddleware(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("conditional shell request status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "data-theme=") {
		t.Errorf("revalidated shell response is missing injected settings")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if rec.Header().Get("ETag") != "" || rec.Header().Get("Last-Modified") != "" {
		t.Errorf("injected shell must not carry cache validators")
	}
}

// TestServeShellAfterReload: F5 — after the initial handshake consumed
// pending, a reloaded webview still gets the current document inlined.
func TestServeShellAfterReload(t *testing.T) {
	app := newTestApp()
	app.store = settings.NewStoreAt(filepath.Join(t.TempDir(), "settings.json"))
	index := testdataAbs(t, "index.md")
	app.openPath(index)
	app.Ready(index) // initial handshake: pending consumed, nothing delivered

	rec := getShell(t, app, "/") // webview reload
	body := rec.Body.String()
	if !strings.Contains(body, fmt.Sprintf(`data-doc-path="%s"`, index)) {
		t.Errorf("reloaded shell must inline the current document")
	}
	// And the reload handshake must not re-deliver it either.
	app.Ready(index)
	app.mu.Lock()
	pending := len(app.pending)
	app.mu.Unlock()
	if pending != 0 {
		t.Errorf("pending after reload Ready = %d, want 0", pending)
	}
}

// TestServeShellAgainstBuiltDist runs the same F2/F4 assertions against the
// actual built shell (frontend/dist/index.html) that ships embedded in the
// binary. Skipped when the frontend has not been built yet (fresh clone);
// the source-shell tests above cover the markers in that case.
func TestServeShellAgainstBuiltDist(t *testing.T) {
	shell, err := os.ReadFile("frontend/dist/index.html")
	if err != nil {
		t.Skipf("frontend/dist not built (%v); run wails build first", err)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(shell)
	})

	app := newTestApp()
	app.store = settings.NewStoreAt(filepath.Join(t.TempDir(), "settings.json"))
	if err := app.store.Save(settings.Settings{Theme: "dark", FontFamily: "", FontSize: 16, ContentWidth: 72}); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	index := testdataAbs(t, "index.md")
	app.openPath(index)

	rec := httptest.NewRecorder()
	app.assetMiddleware(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("built shell status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-theme="dark"`,
		fmt.Sprintf(`data-doc-path="%s"`, index),
		`id="md-view-test-document"`,
		"<title>md-view Test Document — MD View</title>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("built shell missing %q", want)
		}
	}
	if strings.Contains(body, "No document open.") {
		t.Errorf("built shell still contains the empty state")
	}
	if i := strings.Index(body, "<html"); i >= 0 {
		t.Logf("served shell head: %.300s", body[i:])
	}
}

// TestInjectInitialStateEscaping: injected values are user-controlled strings
// and must be HTML-escaped; hostile font families must not reach the style
// attribute at all.
func TestInjectInitialStateEscaping(t *testing.T) {
	shell := []byte(`<html lang="en"><head><title>MD View</title></head><body>` +
		`<div id="doc-title" title=""></div>` +
		`<article id="content"><div class="empty-state">empty</div></article></body></html>`)

	doc := &render.Doc{
		HTML:  "<p>ok</p>",
		Title: `<b>"evil" & title</b>`,
		Path:  `/tmp/a"b<c>.md`,
		Dir:   "/tmp",
	}
	s := settings.Settings{Theme: "dark", FontFamily: `x";</style><script>alert(1)</script>`, FontSize: 16, ContentWidth: 72}
	out, inlined := injectInitialState(shell, doc, s)
	body := string(out)
	if !inlined {
		t.Fatalf("doc not inlined")
	}
	for _, banned := range []string{"<script", `x";`, `title="<b>`, `data-doc-path="/tmp/a"b`} {
		if strings.Contains(body, banned) {
			t.Errorf("unescaped injection %q in:\n%s", banned, body)
		}
	}
	if strings.Contains(body, "--font-family") {
		t.Errorf("hostile font family must be dropped from the inline style")
	}
	if !strings.Contains(body, `data-doc-path="/tmp/a&#34;b&lt;c&gt;.md"`) {
		t.Errorf("doc path not escaped as expected:\n%s", body)
	}
	if !strings.Contains(body, "<p>ok</p>") {
		t.Errorf("sanitized doc HTML must be injected verbatim")
	}
	if strings.Contains(body, "empty-state") {
		t.Errorf("empty state not replaced by the injected document")
	}

	// A benign font family is inlined, escaped for the attribute context.
	s2 := settings.Settings{Theme: "sepia", FontFamily: "Georgia, 'Times New Roman', serif", FontSize: 16, ContentWidth: 72}
	out2, _ := injectInitialState(shell, nil, s2)
	if !strings.Contains(string(out2), "--font-family:Georgia, &#39;Times New Roman&#39;, serif") {
		t.Errorf("benign font family missing from inline style:\n%s", string(out2))
	}
	if !strings.Contains(string(out2), `data-theme="sepia"`) {
		t.Errorf("theme not inlined")
	}
}
