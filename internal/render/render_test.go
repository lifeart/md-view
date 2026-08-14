package render

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
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
