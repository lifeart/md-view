package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"md-view/internal/links"
)

// newTestApp builds an App without a Wails context. openPath before Ready()
// only buffers + extends scope, so no runtime calls happen.
func newTestApp() *App {
	return NewApp()
}

func testdataAbs(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", rel))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

// TestOpenRenderFlow walks the real user flow at the Go seam: OS delivers a
// path -> openPath extends scope -> frontend calls RenderDocument.
func TestOpenRenderFlow(t *testing.T) {
	app := newTestApp()
	index := testdataAbs(t, "index.md")

	// Before any open, rendering must be refused (scope empty).
	if _, err := app.RenderDocument(index); err == nil {
		t.Fatalf("RenderDocument before open must fail (empty scope)")
	}

	app.openPath(index)

	doc, err := app.RenderDocument(index)
	if err != nil {
		t.Fatalf("RenderDocument after open: %v", err)
	}
	if doc.Title != "md-view Test Document" {
		t.Errorf("Title = %q", doc.Title)
	}
	if !strings.Contains(doc.HTML, "doc-asset") {
		t.Errorf("expected rewritten image route in HTML")
	}

	// Buffered delivery: the path must be pending until Ready() flushes it.
	app.mu.Lock()
	pending := len(app.pending)
	app.mu.Unlock()
	if pending != 1 {
		t.Errorf("pending = %d, want 1 (openPath before Ready must buffer)", pending)
	}
}

// TestNavigationExtendsScope follows a relative link from index.md into sub/
// exactly like the frontend does: ResolveLink then RenderDocument.
func TestNavigationExtendsScope(t *testing.T) {
	app := newTestApp()
	index := testdataAbs(t, "index.md")
	app.openPath(index)

	res, err := app.ResolveLink(index, "sub/page.md#details")
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if res.Kind != links.KindMarkdown || res.Fragment != "details" {
		t.Fatalf("ResolveLink = %+v", res)
	}
	sub, err := app.RenderDocument(res.Path)
	if err != nil {
		t.Fatalf("RenderDocument(sub) after ResolveLink: %v", err)
	}
	if sub.Title != "Sub Page" {
		t.Errorf("sub Title = %q", sub.Title)
	}

	// And back again via ../index.md#some-heading.
	back, err := app.ResolveLink(res.Path, "../index.md#some-heading")
	if err != nil {
		t.Fatalf("ResolveLink back: %v", err)
	}
	if back.Path != index || back.Fragment != "some-heading" {
		t.Errorf("back link = %+v", back)
	}
}

// TestResolveLinkGuards verifies out-of-scope bases and dangerous targets.
func TestResolveLinkGuards(t *testing.T) {
	app := newTestApp()
	index := testdataAbs(t, "index.md")

	// Base document out of scope -> refused.
	if _, err := app.ResolveLink(index, "sub/page.md"); err == nil {
		t.Errorf("ResolveLink with out-of-scope base must fail")
	}

	app.openPath(index)

	// Missing markdown target -> error, scope not extended.
	if _, err := app.ResolveLink(index, "missing.md"); err == nil {
		t.Errorf("ResolveLink to missing file must fail")
	}
	// Traversal to a non-md file classifies as file; opening it is scope-checked.
	res, err := app.ResolveLink(index, "../../../../etc/passwd")
	if err != nil {
		t.Fatalf("ResolveLink traversal: %v", err)
	}
	if res.Kind != links.KindFile {
		t.Fatalf("traversal kind = %q", res.Kind)
	}
	if err := app.OpenWithSystemDefault(res.Path); err == nil {
		t.Errorf("OpenWithSystemDefault(/etc/passwd) must be rejected by scope")
	}
}

// TestRenderDocumentRejectsNonMarkdown ensures the render entry point cannot
// be used to read arbitrary in-scope files.
func TestRenderDocumentRejectsNonMarkdown(t *testing.T) {
	app := newTestApp()
	app.openPath(testdataAbs(t, "index.md"))
	if _, err := app.RenderDocument(testdataAbs(t, "notes.txt")); err == nil {
		t.Errorf("RenderDocument of non-markdown file must fail")
	}
}

// TestOpenExternalSchemeValidation: only http(s) is ever opened.
func TestOpenExternalSchemeValidation(t *testing.T) {
	app := newTestApp()
	for _, bad := range []string{"javascript:alert(1)", "file:///etc/passwd", "ftp://x", "chrome://settings"} {
		if err := app.OpenExternal(bad); err == nil {
			t.Errorf("OpenExternal(%q) must be rejected", bad)
		}
	}
	// http(s) fails here only because there is no wails context yet — it must
	// NOT fail scheme validation.
	if err := app.OpenExternal("https://wails.io"); err == nil ||
		!strings.Contains(err.Error(), "not started") {
		t.Errorf("OpenExternal(https) expected context error, got %v", err)
	}
}

// TestAssetMiddleware exercises the /doc-asset/ route with scope checks.
func TestAssetMiddleware(t *testing.T) {
	app := newTestApp()
	app.openPath(testdataAbs(t, "index.md"))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot) // marker: request passed through
	})
	h := app.assetMiddleware(next)

	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}

	// In-scope image served.
	rec := get("/doc-asset/?p=" + url.QueryEscape(testdataAbs(t, "pixel.png")))
	if rec.Code != http.StatusOK {
		t.Errorf("in-scope asset: status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Errorf("in-scope asset content-type = %q", ct)
	}

	// Out-of-scope file refused.
	if rec := get("/doc-asset/?p=" + url.QueryEscape("/etc/passwd")); rec.Code != http.StatusForbidden {
		t.Errorf("out-of-scope asset: status %d, want 403", rec.Code)
	}
	// Traversal refused.
	traversal := testdataAbs(t, "../../../../../etc/passwd")
	if rec := get("/doc-asset/?p=" + url.QueryEscape(traversal)); rec.Code != http.StatusForbidden {
		t.Errorf("traversal asset: status %d, want 403", rec.Code)
	}
	// Missing p refused.
	if rec := get("/doc-asset/"); rec.Code != http.StatusBadRequest {
		t.Errorf("missing p: status %d, want 400", rec.Code)
	}
	// Non-asset routes pass through untouched.
	if rec := get("/index.html"); rec.Code != http.StatusTeapot {
		t.Errorf("pass-through: status %d, want 418", rec.Code)
	}
	// Non-GET refused.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/doc-asset/?p=x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: status %d, want 405", rec.Code)
	}
}

func TestInitialFileFromArgs(t *testing.T) {
	index := testdataAbs(t, "index.md")
	if got := initialFileFromArgs([]string{index}); got != index {
		t.Errorf("initialFileFromArgs = %q, want %q", got, index)
	}
	if got := initialFileFromArgs([]string{"-flag", "nope.txt", "missing.md"}); got != "" {
		t.Errorf("initialFileFromArgs junk = %q, want empty", got)
	}
}
