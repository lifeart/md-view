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

// TestRenderGitHubFeatures pins the GitHub-only markdown features that plain
// GFM leaves as literal text (alerts, footnotes, math, emoji) and the ones the
// sanitizer used to eat (table alignment, <kbd>, align, ol start, <picture>).
func TestRenderGitHubFeatures(t *testing.T) {
	r := New()
	doc, err := r.RenderFile(testdataPath(t, "gfm.md"))
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	html := doc.HTML

	for _, want := range []struct{ name, substr string }{
		{"note alert", `<blockquote class="markdown-alert markdown-alert-note">`},
		{"tip alert", `markdown-alert-tip`},
		{"important alert", `markdown-alert-important`},
		{"warning alert", `markdown-alert-warning`},
		{"caution alert", `markdown-alert-caution`},
		{"alert title", `<p class="markdown-alert-title">Note</p>`},
		{"footnote reference", `<a href="#fn:1" class="footnote-ref"`},
		{"footnote list", `<li id="fn:1">`},
		{"footnote backref", `class="footnote-backref"`},
		{"inline math", `<code class="language-math math-inline">E = mc^2</code>`},
		{"backtick math keeps dollars", `<code class="language-math math-inline">\$5</code>`},
		{"display math", `<pre class="math-display"><code class="language-math">\frac{n!}`},
		{"math fence", `<pre><code class="language-math">\int_0^\infty`},
		{"mermaid fence", `<code class="language-mermaid">`},
		{"emoji", "\U0001F680"},
		{"table align left", `<th align="left">Left</th>`},
		{"table align center", `<td align="center">`},
		{"table align right", `<th align="right">Right</th>`},
		{"kbd", `<kbd>Cmd</kbd>`},
		{"ins", `<ins>Inserted</ins>`},
		{"mark", `<mark>highlighted</mark>`},
		{"details", `<details>`},
		{"summary", `<summary>A collapsed section</summary>`},
		{"div align", `<div align="center">`},
		{"picture", `<picture>`},
		{"source srcset rewritten", `<source srcset="` + AssetRoutePrefix},
		{"named anchor", `<a name="legacy-anchor">`},
		{"ordered list start", `<ol start="5">`},
		{"front matter table", `<table class="frontmatter">`},
		{"front matter scalar", `<th>title</th><td>GitHub Flavored Markdown</td>`},
		{"front matter sequence", `<th>authors</th><td>Ada, Grace</td>`},
	} {
		if !strings.Contains(html, want.substr) {
			t.Errorf("%s: rendered HTML missing %q", want.name, want.substr)
		}
	}

	for _, unwanted := range []struct{ name, substr string }{
		{"alert marker text", "[!NOTE]"},
		{"raw footnote reference", "[^go]"},
		{"raw emoji shortcode", ":rocket:"},
		{"front matter as thematic break", "<hr>\n<h2"},
		{"style-based table alignment", "text-align:"},
	} {
		if strings.Contains(html, unwanted.substr) {
			t.Errorf("%s: rendered HTML still contains %q", unwanted.name, unwanted.substr)
		}
	}

	// A blockquote without a marker must not become an alert.
	if strings.Contains(html, `markdown-alert">`) {
		t.Error("plain blockquote was turned into an alert")
	}
	// Prices are not math.
	if strings.Contains(html, `math-inline">5 and then $10`) {
		t.Error("prose dollar amounts were parsed as math")
	}
}

func TestFrontMatterTitleComesFromBody(t *testing.T) {
	r := New()
	doc, err := r.RenderFile(testdataPath(t, "gfm.md"))
	if err != nil {
		t.Fatalf("RenderFile: %v", err)
	}
	if doc.Title != "GitHub Flavored Markdown" {
		t.Errorf("Title = %q, want the first H1", doc.Title)
	}
}

func TestSplitFrontMatter(t *testing.T) {
	for _, tc := range []struct {
		name, src, wantYAML, wantRest string
	}{
		{"none", "# Title\n", "", "# Title\n"},
		{"simple", "---\na: 1\n---\n# Title\n", "a: 1\n", "# Title\n"},
		{"dot terminator", "---\na: 1\n...\n# T\n", "a: 1\n", "# T\n"},
		{"unterminated is not front matter", "---\na: 1\n# T\n", "", "---\na: 1\n# T\n"},
		{"thematic break only", "---\n\ntext\n", "", "---\n\ntext\n"},
		{"not at start", "x\n---\na: 1\n---\n", "", "x\n---\na: 1\n---\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			yaml, rest := splitFrontMatter([]byte(tc.src))
			if string(yaml) != tc.wantYAML {
				t.Errorf("yaml = %q, want %q", yaml, tc.wantYAML)
			}
			if string(rest) != tc.wantRest {
				t.Errorf("rest = %q, want %q", rest, tc.wantRest)
			}
		})
	}
}

func TestFrontMatterFallsBackToRawBlock(t *testing.T) {
	// A shape the shallow parser cannot read must be shown, not guessed at.
	html := frontMatterTable([]byte("- just\n- a sequence\n"))
	if !strings.Contains(html, `<pre class="frontmatter">`) {
		t.Errorf("unparseable front matter: got %q, want the raw block", html)
	}
}

func TestSlugifyMatchesGitHub(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Simple Heading", "simple-heading"},
		{"Heading with Punctuation, & Symbols!", "heading-with-punctuation--symbols"},
		{"Ünïcödé Héading", "ünïcödé-héading"},
		{"見出し", "見出し"},
		{"emoji 🎉 here", "emoji--here"},
		{"C++ and C#", "c-and-c"},
		{"snake_case-and-dash", "snake_case-and-dash"},
		{"1. Numbered", "1-numbered"},
		{"", ""},
	} {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHeadingIDsUseTextNotMarkup(t *testing.T) {
	r := New()
	doc, err := r.Render(testdataPath(t, "x.md"), []byte(
		"## A [linked](https://example.com) `code` **word**\n\n## Dup\n\n## Dup\n\n## !!!\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		`id="a-linked-code-word"`,
		`id="dup"`,
		`id="dup-1"`,
		`id="section"`,
	} {
		if !strings.Contains(doc.HTML, want) {
			t.Errorf("missing %s in:\n%s", want, doc.HTML)
		}
	}
}

func TestMathBoundaries(t *testing.T) {
	r := New()
	for _, tc := range []struct {
		name, src string
		wantMath  bool
	}{
		{"inline", "$x+y$\n", true},
		{"leading space rejected", "$ x$\n", false},
		{"trailing space rejected", "$x $\n", false},
		{"prices rejected", "costs $5 and $10 today\n", false},
		{"escaped dollar", "$a \\$ b$\n", true},
		{"display block", "$$\nx\n$$\n", true},
		{"single-line display", "$$x$$\n", true},
		{"unclosed is prose", "$x\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := r.Render(testdataPath(t, "x.md"), []byte(tc.src))
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			got := strings.Contains(doc.HTML, `class="language-math`)
			if got != tc.wantMath {
				t.Errorf("math=%v, want %v; html=%q", got, tc.wantMath, doc.HTML)
			}
		})
	}
}

func TestAlertMarkerStrictness(t *testing.T) {
	r := New()
	for _, tc := range []struct {
		name, src string
		wantAlert bool
	}{
		{"note", "> [!NOTE]\n> body\n", true},
		{"lowercase", "> [!note]\n> body\n", true},
		{"marker only", "> [!TIP]\n", true},
		{"unknown kind", "> [!HINT]\n> body\n", false},
		{"trailing text on marker line", "> [!NOTE] inline\n> body\n", false},
		{"marker not first", "> intro\n>\n> [!NOTE]\n", false},
		{"plain quote", "> body\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := r.Render(testdataPath(t, "x.md"), []byte(tc.src))
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			got := strings.Contains(doc.HTML, "markdown-alert ")
			if got != tc.wantAlert {
				t.Errorf("alert=%v, want %v; html=%q", got, tc.wantAlert, doc.HTML)
			}
			if strings.Contains(doc.HTML, "[!") && tc.wantAlert {
				t.Errorf("marker left in output: %q", doc.HTML)
			}
		})
	}
}

func TestSanitizerStillBlocksScripts(t *testing.T) {
	r := New()
	doc, err := r.Render(testdataPath(t, "x.md"), []byte(
		"<script>alert(1)</script>\n\n"+
			"<div align=\"center\" onclick=\"evil()\" style=\"position:fixed\">x</div>\n\n"+
			"<a name=\"ok\" href=\"javascript:evil()\">link</a>\n\n"+
			"<source srcset=\"javascript:evil()\">\n\n"+
			"<blockquote class=\"markdown-alert\" onmouseover=\"evil()\">q</blockquote>\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, forbidden := range []string{"<script", "onclick", "onmouseover", "javascript:", "style="} {
		if strings.Contains(doc.HTML, forbidden) {
			t.Errorf("sanitized HTML contains %q: %s", forbidden, doc.HTML)
		}
	}
}

func TestSrcsetPolicy(t *testing.T) {
	r := New()
	for _, tc := range []struct {
		name, srcset string
		wantKept     bool
	}{
		{"relative", "logo.png", true},
		{"relative with descriptor", "logo.png 1x, logo@2x.png 2x", true},
		{"absolute path", "/img/logo.png", true},
		{"https", "https://example.com/logo.png 2x", true},
		{"javascript scheme", "javascript:evil()", false},
		{"data uri", "data:image/png;base64,AAAA", false},
		{"protocol relative", "//evil.example/logo.png", false},
		{"one bad candidate poisons the list", "logo.png 1x, javascript:evil() 2x", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := r.Render(testdataPath(t, "x.md"),
				[]byte(`<picture><source srcset="`+tc.srcset+`"></picture>`+"\n"))
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got := strings.Contains(doc.HTML, "srcset"); got != tc.wantKept {
				t.Errorf("srcset kept = %v, want %v; html=%q", got, tc.wantKept, doc.HTML)
			}
		})
	}
}

// TestMathDoesNotLeakIntoProse pins the boundaries where `$` is *not* math.
// The mid-paragraph `$$…$$` case is the one that mattered: declining it left
// the first `$` as text and re-triggered the parser on the second, so
// "text $$a+b$$ more" rendered with a stray dollar on each side of the math.
func TestMathDoesNotLeakIntoProse(t *testing.T) {
	r := New()
	for _, tc := range []struct{ name, src, want string }{
		{"mid-paragraph display", "text $$a+b$$ more\n",
			`text <code class="language-math math-display">a+b</code> more`},
		{"code span is not math", "a `$x$` b\n", "<code>$x$</code>"},
		{"escaped dollars", "costs \\$5 and \\$10\n", "costs $5 and $10"},
		{"prices", "it cost $5 and then $10 today\n", "it cost $5 and then $10 today"},
		{"unclosed display", "text $$a+b more\n", "text $$a+b more"},
		{"display cannot span lines inline", "text $$a\nb$$ more\n", "text $$a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := r.Render(testdataPath(t, "x.md"), []byte(tc.src))
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if !strings.Contains(doc.HTML, tc.want) {
				t.Errorf("got %q, want it to contain %q", doc.HTML, tc.want)
			}
		})
	}

	// A fenced or indented block is code, never math.
	for _, src := range []string{"```\n$$\nx\n$$\n```\n", "    $$x$$\n"} {
		doc, err := r.Render(testdataPath(t, "x.md"), []byte(src))
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if strings.Contains(doc.HTML, "language-math") {
			t.Errorf("code block was parsed as math: %q", doc.HTML)
		}
	}
}

func TestHeadingSlugIncludesMath(t *testing.T) {
	r := New()
	doc, err := r.Render(testdataPath(t, "x.md"), []byte("# The $E=mc^2$ case\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	// GitHub slugs the text content of its math element, so the TeX counts.
	if !strings.Contains(doc.HTML, `id="the-emc2-case"`) {
		t.Errorf("heading slug ignored the math: %q", doc.HTML)
	}
}

// TestMediaElements covers <video>/<audio>, which GitHub renders and
// bluemonday.UGCPolicy drops entirely.
func TestMediaElements(t *testing.T) {
	r := New()
	doc, err := r.Render(testdataPath(t, "x.md"), []byte(
		`<video src="clip.mp4" poster="pixel.png" controls width="320"></video>`+"\n\n"+
			`<video controls><source src="clip.webm" type="video/webm">`+
			`<source src="https://example.com/c.mp4" type="video/mp4"></video>`+"\n\n"+
			`<audio src="track.mp3" controls></audio>`+"\n\n"+
			`<video src="javascript:evil()" autoplay onplay="evil()"></video>`+"\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		`<video src="` + AssetRoutePrefix,
		`poster="` + AssetRoutePrefix,
		`<source src="` + AssetRoutePrefix,
		`<source src="https://example.com/c.mp4"`,
		`<audio src="` + AssetRoutePrefix,
		`controls=""`,
	} {
		if !strings.Contains(doc.HTML, want) {
			t.Errorf("missing %q in %q", want, doc.HTML)
		}
	}
	// A reading app must not let a document start playing, and the usual
	// script vectors stay closed.
	for _, forbidden := range []string{"autoplay", "onplay", "javascript:"} {
		if strings.Contains(doc.HTML, forbidden) {
			t.Errorf("sanitized HTML contains %q: %q", forbidden, doc.HTML)
		}
	}
}

// TestSanitizerDropsUnsupportedEmbeds keeps the elements MDv deliberately does
// not render from sneaking back in with a policy widening.
func TestSanitizerDropsUnsupportedEmbeds(t *testing.T) {
	r := New()
	doc, err := r.Render(testdataPath(t, "x.md"), []byte(
		"<iframe src=\"https://evil.example\"></iframe>\n\n"+
			"<object data=\"x.swf\"></object>\n\n"+
			"<embed src=\"x.swf\">\n\n"+
			"<form action=\"/x\"><input name=\"p\" type=\"password\"></form>\n\n"+
			"<svg onload=\"evil()\"><circle r=\"10\"/></svg>\n"))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, forbidden := range []string{"<iframe", "<object", "<embed", "<form", "<svg", "onload", "password"} {
		if strings.Contains(doc.HTML, forbidden) {
			t.Errorf("sanitized HTML contains %q: %q", forbidden, doc.HTML)
		}
	}
}
