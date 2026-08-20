package render

import (
	"bytes"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testdataPath(t *testing.T, rel string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", rel))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func TestRenderGFMFeatures(t *testing.T) {
	r := New()
	doc, err := r.RenderFile(testdataPath(t, "index.md"))
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	html := doc.HTML

	for _, want := range []struct{ name, substr string }{
		{"table", "<table>"},
		{"table header cell", "<th>Feature</th>"},
		{"strikethrough", "<del>strikethrough</del>"},
		{"task list checkbox", `type="checkbox"`},
		{"checked task", "checked"},
		{"disabled checkbox", "disabled"},
		{"blockquote", "<blockquote>"},
		{"hr", "<hr"},
		{"chroma pre class", `<pre class="chroma"`},
		{"chroma token span", `<span class="`},
	} {
		if !strings.Contains(html, want.substr) {
			t.Errorf("%s: rendered HTML missing %q", want.name, want.substr)
		}
	}
}

func TestRenderHeadingAnchors(t *testing.T) {
	r := New()
	doc, err := r.RenderFile(testdataPath(t, "index.md"))
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	for _, id := range []string{`id="code-samples"`, `id="some-heading"`, `id="md-view-test-document"`} {
		if !strings.Contains(doc.HTML, id) {
			t.Errorf("heading anchor missing: %s", id)
		}
	}
	if !strings.Contains(doc.HTML, `href="#code-samples"`) {
		t.Errorf("in-doc anchor link missing")
	}
}

func TestRenderTitle(t *testing.T) {
	r := New()
	doc, err := r.RenderFile(testdataPath(t, "index.md"))
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	if doc.Title != "md-view Test Document" {
		t.Errorf("Title = %q, want %q", doc.Title, "md-view Test Document")
	}

	// No h1 → file name.
	doc2, err := r.Render("/tmp/x/untitled.md", []byte("just a paragraph"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if doc2.Title != "untitled" {
		t.Errorf("fallback Title = %q, want %q", doc2.Title, "untitled")
	}
}

func TestRenderImageRewrite(t *testing.T) {
	r := New()
	doc, err := r.RenderFile(testdataPath(t, "index.md"))
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	wantAbs := testdataPath(t, "pixel.png")
	want := AssetRoutePrefix + "?p=" + url.QueryEscape(wantAbs)
	if !strings.Contains(doc.HTML, want) {
		t.Errorf("img src not rewritten: want %q in HTML", want)
	}

	// Parent-relative image from the sub page resolves through ".." correctly.
	sub, err := r.RenderFile(testdataPath(t, "sub/page.md"))
	if err != nil {
		t.Fatalf("RenderFile sub: %v", err)
	}
	if !strings.Contains(sub.HTML, want) {
		t.Errorf("parent-relative img src not rewritten: want %q", want)
	}
}

// TestRawHTMLImageRewrite: F9 — <img> tags written as raw HTML in markdown
// (not markdown image syntax) must get the same /doc-asset/ src rewriting,
// while absolute http(s) sources stay untouched.
func TestRawHTMLImageRewrite(t *testing.T) {
	r := New()
	src := []byte("# Doc\n\n" +
		`<img src="img/local.png" alt="local">` + "\n\n" +
		`<p>inline <img src="../up.png" alt="up"> here</p>` + "\n\n" +
		`<img src="https://example.com/remote.png" alt="remote">` + "\n")
	doc, err := r.Render("/docs/project/a.md", src)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	wantLocal := AssetRoutePrefix + "?p=" + url.QueryEscape("/docs/project/img/local.png")
	if !strings.Contains(doc.HTML, wantLocal) {
		t.Errorf("raw-HTML relative img not rewritten: want %q in\n%s", wantLocal, doc.HTML)
	}
	wantUp := AssetRoutePrefix + "?p=" + url.QueryEscape("/docs/up.png")
	if !strings.Contains(doc.HTML, wantUp) {
		t.Errorf("raw-HTML parent-relative img not rewritten: want %q in\n%s", wantUp, doc.HTML)
	}
	if !strings.Contains(doc.HTML, `src="https://example.com/remote.png"`) {
		t.Errorf("raw-HTML remote img src must be preserved, got:\n%s", doc.HTML)
	}
	// Surrounding markup survives the token walk byte-for-byte.
	if !strings.Contains(doc.HTML, "inline ") || !strings.Contains(doc.HTML, " here") {
		t.Errorf("surrounding content damaged by rewrite:\n%s", doc.HTML)
	}
}

// TestRawHTMLImageRewriteKeepsSanitization: the rewrite pass runs after
// bluemonday and must not reintroduce anything the sanitizer stripped.
func TestRawHTMLImageRewriteKeepsSanitization(t *testing.T) {
	r := New()
	src := []byte(`<img src="x.png" onerror="alert(1)"><script>alert(2)</script>`)
	doc, err := r.Render("/docs/project/a.md", src)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, banned := range []string{"onerror", "<script", "alert("} {
		if strings.Contains(doc.HTML, banned) {
			t.Errorf("rewrite pass reintroduced %q:\n%s", banned, doc.HTML)
		}
	}
	want := AssetRoutePrefix + "?p=" + url.QueryEscape("/docs/project/x.png")
	if !strings.Contains(doc.HTML, want) {
		t.Errorf("sanitized raw img still not rewritten: want %q in\n%s", want, doc.HTML)
	}
}

func TestRenderRemoteImageUntouched(t *testing.T) {
	r := New()
	doc, err := r.Render("/tmp/x/a.md", []byte(`![remote](https://example.com/a.png)`))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(doc.HTML, `src="https://example.com/a.png"`) {
		t.Errorf("remote image src must be preserved, got: %s", doc.HTML)
	}
}

func TestSanitization(t *testing.T) {
	r := New()
	doc, err := r.RenderFile(testdataPath(t, "evil.md"))
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	html := doc.HTML

	for _, banned := range []string{"<script", "onerror", "onclick", "<iframe", "javascript:", "alert("} {
		if strings.Contains(html, banned) {
			t.Errorf("sanitizer let %q through:\n%s", banned, html)
		}
	}
	if !strings.Contains(html, "Normal paragraph that must survive sanitization.") {
		t.Errorf("benign content was over-stripped")
	}
	// The heading id must still be present (policy allows ids on headings).
	if !strings.Contains(html, `id="evil-document"`) {
		t.Errorf("heading id stripped by sanitizer")
	}
}

// TestLargeCodeFenceSkipsHighlighting: F1 regression — a single fence above
// the 50 KB cap must render as a plain escaped code block (no chroma spans)
// and must stay fast; a normal-sized fence in the same document must still be
// highlighted.
func TestLargeCodeFenceSkipsHighlighting(t *testing.T) {
	var src bytes.Buffer
	src.WriteString("# Big\n\n```go\nfunc small() { return }\n```\n\n```go\n")
	for src.Len() < maxHighlightFenceBytes+4096 {
		src.WriteString("if x := compute(); x > 0 { fmt.Println(\"<script>alert(1)</script>\", x) }\n")
	}
	src.WriteString("```\n")

	r := New()
	start := time.Now()
	doc, err := r.Render("/tmp/x/big.md", src.Bytes())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// The small fence keeps chroma highlighting.
	if !strings.Contains(doc.HTML, `<pre class="chroma"`) {
		t.Errorf("normal fence lost chroma highlighting")
	}
	// The oversized fence renders as a plain <pre><code> block: exactly one
	// chroma block in the document, and the raw HTML inside the big fence is
	// escaped, not tokenized into spans.
	if got := strings.Count(doc.HTML, `<pre class="chroma"`); got != 1 {
		t.Errorf("chroma block count = %d, want 1 (oversized fence must not be highlighted)", got)
	}
	if !strings.Contains(doc.HTML, "<pre><code>") {
		t.Errorf("oversized fence missing plain <pre><code> fallback")
	}
	if !strings.Contains(doc.HTML, "&lt;script&gt;") {
		t.Errorf("oversized fence content not escaped")
	}
	if strings.Contains(doc.HTML, "<script") {
		t.Errorf("unescaped script tag leaked through")
	}
	// Chroma on a fence this size costs hundreds of ms; the plain path is a
	// few ms. A generous bound still catches a reintroduced chroma path.
	if elapsed > time.Second {
		t.Errorf("oversized fence render took %v, want well under 1s", elapsed)
	}
}

// TestFenceUnderCapStillHighlighted: content just below the cap keeps
// highlighting (the cap only bites above 50 KB).
func TestFenceUnderCapStillHighlighted(t *testing.T) {
	var src bytes.Buffer
	src.WriteString("```go\n")
	for src.Len() < maxHighlightFenceBytes-4096 {
		src.WriteString("func f() int { return 42 } // filler line to grow the fence\n")
	}
	src.WriteString("```\n")
	r := New()
	doc, err := r.Render("/tmp/x/under.md", src.Bytes())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(doc.HTML, `<pre class="chroma"`) {
		t.Errorf("fence under the cap must still be highlighted")
	}
}

func TestRewriteAssetURL(t *testing.T) {
	dir := "/docs/project"
	cases := []struct {
		dest string
		want string
		ok   bool
	}{
		{"img/a.png", AssetRoutePrefix + "?p=" + url.QueryEscape("/docs/project/img/a.png"), true},
		{"../a.png", AssetRoutePrefix + "?p=" + url.QueryEscape("/docs/a.png"), true},
		{"/abs/b.png", AssetRoutePrefix + "?p=" + url.QueryEscape("/abs/b.png"), true},
		{"with%20space.png", AssetRoutePrefix + "?p=" + url.QueryEscape("/docs/project/with space.png"), true},
		{"https://example.com/x.png", "", false},
		{"data:image/png;base64,AAAA", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := RewriteAssetURL(c.dest, dir)
		if ok != c.ok || got != c.want {
			t.Errorf("RewriteAssetURL(%q) = (%q, %v), want (%q, %v)", c.dest, got, ok, c.want, c.ok)
		}
	}
}

// TestAltTextSurvivesPunctuation: alt text and titles carrying ordinary
// punctuation must reach the webview. bluemonday's UGCPolicy matches these
// attributes against its Paragraph pattern, which rejects colons and em
// dashes and then drops the whole attribute — silently removing exactly the
// caption a screen reader reads out (see buildPolicy).
func TestAltTextSurvivesPunctuation(t *testing.T) {
	r := New()
	src := []byte("# Doc\n\n" +
		`![MDv: a document — open](img/shot.png "Screenshot: the reading view")` + "\n\n" +
		`[link](https://example.com "Docs: the manual")` + "\n")
	doc, err := r.Render("/docs/a.md", src)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		`alt="MDv: a document — open"`,
		`title="Screenshot: the reading view"`,
		`title="Docs: the manual"`,
	} {
		if !strings.Contains(doc.HTML, want) {
			t.Errorf("sanitizer dropped %s from:\n%s", want, doc.HTML)
		}
	}
	// The widened pattern must still not admit markup into the attribute.
	hostile := []byte("# Doc\n\n" + `<img src="img/x.png" alt="a<script>b">` + "\n")
	hostileDoc, err := r.Render("/docs/a.md", hostile)
	if err != nil {
		t.Fatalf("Render hostile: %v", err)
	}
	if strings.Contains(hostileDoc.HTML, "<script") {
		t.Errorf("script tag survived sanitization:\n%s", hostileDoc.HTML)
	}
}
