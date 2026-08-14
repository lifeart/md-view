package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

// TestThemeBackgroundMatchesCSS pins the Go-side window background colours to
// the --bg values in theme.css. The window is ordered front before the webview
// paints (see showWindow), so a drift here shows up as a coloured flash on
// every launch — and nothing else would catch it.
func TestThemeBackgroundMatchesCSS(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("frontend", "src", "theme.css"))
	if err != nil {
		t.Fatalf("read theme.css: %v", err)
	}
	// The first --bg after each theme's selector ( :root is the light theme ).
	for _, c := range []struct{ theme, selector string }{
		{"light", ":root {"},
		{"dark", `[data-theme="dark"] {`},
		{"sepia", `[data-theme="sepia"] {`},
	} {
		block := strings.Index(string(css), c.selector)
		if block < 0 {
			t.Fatalf("theme.css has no %q block", c.selector)
		}
		rest := string(css)[block:]
		i := strings.Index(rest, "--bg: #")
		if i < 0 || i+14 > len(rest) {
			t.Fatalf("theme.css %q block has no --bg", c.selector)
		}
		want := strings.ToLower(rest[i+len("--bg: #") : i+len("--bg: #")+6])
		bg, ok := themeBackground(c.theme)
		if !ok {
			t.Errorf("themeBackground(%q) not mapped", c.theme)
			continue
		}
		got := fmt.Sprintf("%02x%02x%02x", bg.R, bg.G, bg.B)
		if got != want {
			t.Errorf("themeBackground(%q) = #%s, theme.css --bg = #%s", c.theme, got, want)
		}
		if bg.A != 255 {
			t.Errorf("themeBackground(%q) alpha = %d, want opaque", c.theme, bg.A)
		}
	}
	if _, ok := themeBackground("system"); ok {
		t.Errorf(`themeBackground("system") must stay unmapped: only the webview can resolve it`)
	}
	// showWindow skips the override when the theme already matches the
	// configured window colour, so those two must agree.
	if light, _ := themeBackground("light"); light != defaultBackground {
		t.Errorf("defaultBackground = %+v, light theme = %+v", defaultBackground, light)
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

// TestOpenWithSystemDefaultBlocksExecutables: F3 — executables are never
// handed to the OS default handler; they are revealed in the file manager
// with a user-visible notice instead. Non-executable documents still open.
func TestOpenWithSystemDefaultBlocksExecutables(t *testing.T) {
	// EvalSymlinks: scope.Check returns symlink-resolved paths, and macOS
	// temp dirs live behind the /var -> /private/var symlink.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}

	writeFile := func(name string, mode os.FileMode) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("payload"), mode); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}
	md := writeFile("doc.md", 0o644)
	pdf := writeFile("report.pdf", 0o644)
	execFile := writeFile("tool.bin", 0o755)     // +x bit, innocuous extension
	pkgFile := writeFile("installer.pkg", 0o644) // denylisted extension, no +x

	app := newTestApp()
	var launched [][]string
	app.launch = func(name string, arg ...string) error {
		launched = append(launched, append([]string{name}, arg...))
		return nil
	}
	app.openPath(md) // brings dir into scope

	// Plain document: opened with the system default.
	launched = nil
	if err := app.OpenWithSystemDefault(pdf); err != nil {
		t.Fatalf("OpenWithSystemDefault(pdf): %v", err)
	}
	if len(launched) != 1 || launched[0][0] != "open" || launched[0][1] != pdf {
		t.Errorf("pdf launch = %v, want [open %s]", launched, pdf)
	}

	// Executable permission bit: rejected, revealed instead (open -R on macOS).
	launched = nil
	if err := app.OpenWithSystemDefault(execFile); err != nil {
		t.Fatalf("OpenWithSystemDefault(+x): %v", err)
	}
	if len(launched) != 1 || launched[0][0] != "open" || launched[0][1] != "-R" || launched[0][2] != execFile {
		t.Errorf("+x launch = %v, want [open -R %s]", launched, execFile)
	}

	// Denylisted extension without +x: also rejected and revealed.
	launched = nil
	if err := app.OpenWithSystemDefault(pkgFile); err != nil {
		t.Fatalf("OpenWithSystemDefault(pkg): %v", err)
	}
	if len(launched) != 1 || launched[0][1] != "-R" || launched[0][2] != pkgFile {
		t.Errorf("pkg launch = %v, want [open -R %s]", launched, pkgFile)
	}
}

func TestIsBlockedForSystemOpen(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string, mode os.FileMode) (string, os.FileInfo) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), mode); err != nil {
			t.Fatalf("write: %v", err)
		}
		// WriteFile does not change the mode of a pre-existing file; chmod so
		// repeated names with different modes behave as declared.
		if err := os.Chmod(p, mode); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		return p, info
	}
	for _, c := range []struct {
		name    string
		mode    os.FileMode
		blocked bool
	}{
		{"a.pdf", 0o644, false},
		{"a.png", 0o644, false},
		{"a.pdf", 0o755, true},  // +x bit wins even with a benign extension
		{"run.sh", 0o644, true}, // denylisted extension without +x
		{"RUN.SH", 0o644, true}, // case-insensitive extension match
		{"do.command", 0o644, true},
		{"x.applescript", 0o644, true},
		{"pkg.jar", 0o644, true},
		{"setup.exe", 0o644, true}, // Windows-runnable: extension-only (+x is inert there)
		{"script.ps1", 0o644, true},
		{"page.hta", 0o644, true},
	} {
		p, info := mk(c.name, c.mode)
		if got := isBlockedForSystemOpen(p, info); got != c.blocked {
			t.Errorf("isBlockedForSystemOpen(%s, %v) = %v, want %v", c.name, c.mode, got, c.blocked)
		}
	}

	// macOS bundle directories never trip the regular-file +x check; the
	// extension denylist must catch them.
	bundle := filepath.Join(dir, "Evil.prefPane")
	if err := os.Mkdir(bundle, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	info, err := os.Stat(bundle)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !isBlockedForSystemOpen(bundle, info) {
		t.Errorf("bundle directory %s must be blocked", bundle)
	}
	plain := filepath.Join(dir, "assets")
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	info, err = os.Stat(plain)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if isBlockedForSystemOpen(plain, info) {
		t.Errorf("plain directory %s must not be blocked", plain)
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
