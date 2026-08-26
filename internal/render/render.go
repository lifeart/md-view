// Package render implements the markdown rendering pipeline:
// goldmark (GFM) -> chroma (class-based syntax highlighting) -> bluemonday (sanitization).
package render

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	emoji "github.com/yuin/goldmark-emoji"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	xhtml "golang.org/x/net/html"
)

// AssetRoutePrefix is the in-app URL prefix that serves scope-checked local files
// (images referenced by documents). The absolute file path travels in the "p"
// query parameter, URL-encoded.
const AssetRoutePrefix = "/doc-asset/"

// maxHighlightFenceBytes caps syntax highlighting: any single code fence whose
// content is larger than this renders as a plain escaped <pre><code> block
// instead of going through chroma. Chroma tokenization costs seconds per
// megabyte, which blows the performance budget on pathological documents
// (see ARCHITECTURE.md, performance budget).
const maxHighlightFenceBytes = 50 << 10 // 50 KB

// Doc is a rendered markdown document delivered to the frontend.
type Doc struct {
	HTML  string `json:"html"`
	Title string `json:"title"`
	Path  string `json:"path"`
	Dir   string `json:"dir"`
}

// Renderer converts markdown files into sanitized HTML documents.
type Renderer struct {
	md     goldmark.Markdown
	policy *bluemonday.Policy
}

// New builds the render pipeline described in ARCHITECTURE.md.
func New() *Renderer {
	parserOpts := []parser.Option{
		// GitHub alerts ("> [!NOTE]"); see alerts.go.
		parser.WithParagraphTransformers(util.Prioritized(alertTransformer{}, 100)),
	}
	// $…$ / $$…$$ math; see math.go. Heading ids are assigned after parsing by
	// assignHeadingIDs (slug.go), so parser.WithAutoHeadingID stays off.
	parserOpts = append(parserOpts, mathParserOptions()...)

	md := goldmark.New(
		goldmark.WithExtensions(
			// extension.GFM minus Table, which is re-added with alignment
			// rendered as an `align` attribute: goldmark's default emits
			// `style="text-align:…"`, and bluemonday strips `style`, so every
			// column alignment in every GFM table was being silently dropped.
			extension.NewTable(
				extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute),
			),
			extension.Strikethrough,
			extension.Linkify,
			extension.TaskList,
			// GitHub renders [^1] footnotes; plain GFM leaves them as text.
			extension.Footnote,
			// :tada: and friends, as unicode rather than an <img> so they cost
			// no network and copy as text.
			emoji.New(emoji.WithRenderingMethod(emoji.Unicode)),
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(
					chromahtml.WithClasses(true),
				),
			),
		),
		goldmark.WithParserOptions(parserOpts...),
		// Raw HTML is passed through here and neutralized by bluemonday below.
		// It must never reach the webview unsanitized.
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
			mathRendererOption(),
		),
	)
	return &Renderer{md: md, policy: buildPolicy()}
}

// srcsetPattern accepts a srcset attribute — `url [descriptor], …` — only when
// every candidate URL is safe: an absolute http(s) URL, or a relative path with
// no scheme (no colon) and no protocol-relative `//host` prefix.
var srcsetPattern = func() *regexp.Regexp {
	candidate := `(?:https?://[^\s,]+|/?[^/\s,:][^\s,:]*)(?:\s+[0-9.]+[wxWX])?`
	return regexp.MustCompile(`^\s*` + candidate + `(?:\s*,\s*` + candidate + `)*\s*$`)
}()

// idPattern matches the anchor ids we emit (GitHub-style heading slugs, which
// keep unicode letters, and the footnote ids goldmark generates).
var idPattern = regexp.MustCompile(`^[\p{L}\p{N}\-_:.]+$`)

func buildPolicy() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// Classes carry meaning downstream and must survive sanitization: chroma
	// highlighting (span/code/pre/div), GitHub alerts (blockquote/p), footnote
	// links and back-links (a/sup/section/ol/li), math hooks (code/pre) and the
	// front-matter table.
	p.AllowAttrs("class").OnElements(
		"span", "code", "pre", "div", "blockquote", "p", "a", "sup", "section",
		"ol", "ul", "li", "table", "img", "h1", "h2", "h3", "h4", "h5", "h6",
	)
	// Anchor targets: heading slugs (slug.go) and footnote/back-link ids.
	p.AllowAttrs("id").Matching(idPattern).OnElements(
		"h1", "h2", "h3", "h4", "h5", "h6", "a", "li", "sup", "section", "div",
	)
	// `<a name="x">` is how pre-slug READMEs mark link targets; GitHub still
	// honours them, and without this the anchor vanishes and the link dead-ends.
	p.AllowAttrs("name").Matching(idPattern).OnElements("a")
	// Landmark roles goldmark's footnote renderer emits (doc-endnotes,
	// doc-noteref, doc-backlink). Purely assistive, but free to keep.
	p.AllowAttrs("role").Matching(regexp.MustCompile(`^[a-zA-Z-]+$`)).
		OnElements("a", "section", "li", "div", "sup")
	// <kbd> is styled by theme.css and is the standard way to write shortcuts;
	// UGCPolicy drops it, so `<kbd>Cmd</kbd>+<kbd>K</kbd>` rendered as "Cmd+K".
	p.AllowElements("kbd", "picture", "source")
	// <picture>/<source> is how READMEs ship a light and a dark logo. bluemonday
	// does not recognise srcset as a URL-bearing attribute — it policies only
	// src/href — so the pattern has to do that job: every candidate must be
	// either an http(s) URL or a scheme-less, non-protocol-relative path.
	p.AllowAttrs("srcset").Matching(srcsetPattern).OnElements("source", "img")
	p.AllowAttrs("sizes", "media", "type").OnElements("source")
	// GitHub renders <video>/<audio> in markdown, and a local document can
	// reasonably point at a clip next to it. src/poster are URL-policed by
	// bluemonday and rewritten to the scope-checked asset route below.
	// `autoplay` is deliberately not allowed: this is a reading app, and a
	// document should not be able to start playing on open.
	p.AllowElements("video", "audio")
	p.AllowAttrs("src", "poster").OnElements("video", "audio")
	p.AllowAttrs("src").OnElements("source")
	p.AllowAttrs("controls", "loop", "muted", "playsinline").
		Matching(regexp.MustCompile(`^(|controls|loop|muted|playsinline|true)$`)).
		OnElements("video", "audio")
	p.AllowAttrs("preload").Matching(regexp.MustCompile(`^(none|metadata|auto)$`)).
		OnElements("video", "audio")
	p.AllowAttrs("width", "height").Matching(bluemonday.NumberOrPercent).
		OnElements("video")
	// `<div align="center">` (and friends) is the standard README centering
	// idiom; GitHub allows align on these.
	p.AllowAttrs("align").Matching(bluemonday.CellAlign).
		OnElements("div", "p", "table", "h1", "h2", "h3", "h4", "h5", "h6")
	// `5. first` must keep numbering from 5, as it does on GitHub.
	p.AllowAttrs("start").Matching(regexp.MustCompile(`^-?[0-9]+$`)).OnElements("ol")
	p.AllowAttrs("reversed").Matching(regexp.MustCompile(`^(|reversed)$`)).OnElements("ol")
	// GFM task-list checkboxes.
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").Matching(regexp.MustCompile(`^(|checked|disabled)$`)).OnElements("input")
	// Image alt text and link/image titles: UGCPolicy allows these attributes
	// but matches them against bluemonday.Paragraph, which rejects colons and
	// em dashes — so `![MDv: the viewer](x.png)` silently lost its alt text
	// entirely, which is exactly the caption a screen reader needs. Widen the
	// pattern to any single-line text without angle brackets; bluemonday
	// HTML-escapes attribute values on output, so the restriction was never
	// what made them safe.
	altText := regexp.MustCompile(`^[^<>\r\n]*$`)
	p.AllowAttrs("alt").Matching(altText).OnElements("img")
	p.AllowAttrs("title").Matching(altText).OnElements("img", "a")
	// UGCPolicy already allows img[src] with relative URLs, which covers the
	// rewritten /doc-asset/ routes, and a[href], tables, blockquotes, del, etc.
	return p
}

// RenderFile reads path from disk and renders it. The caller is responsible
// for scope-checking path before calling this.
func (r *Renderer) RenderFile(path string) (Doc, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Doc{}, fmt.Errorf("resolve path: %w", err)
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		return Doc{}, fmt.Errorf("read %s: %w", abs, err)
	}
	return r.Render(abs, src)
}

// Render converts markdown source into a sanitized Doc. path must be the
// absolute path of the document; relative image sources are resolved against
// its directory and rewritten to the /doc-asset/ route.
func (r *Renderer) Render(path string, src []byte) (Doc, error) {
	dir := filepath.Dir(path)

	// YAML front matter is not markdown; peel it off before parsing so it does
	// not become an <hr> plus a setext heading, and show it as GitHub does.
	frontMatter, body := splitFrontMatter(src)

	root := r.md.Parser().Parse(text.NewReader(body))
	title := extractTitle(root, body)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	insertAlertTitles(root)
	assignHeadingIDs(root, body)
	rewriteImageSources(root, dir)
	capOversizedFences(root)

	var buf bytes.Buffer
	if len(frontMatter) > 0 {
		buf.WriteString(frontMatterTable(frontMatter))
	}
	if err := r.md.Renderer().Render(&buf, body, root); err != nil {
		return Doc{}, fmt.Errorf("render %s: %w", path, err)
	}

	sanitized := r.policy.SanitizeReader(&buf).String()
	// Markdown-syntax images were rewritten on the AST above; raw-HTML <img>
	// tags pass through goldmark untouched, so rewrite them here, after
	// sanitization (bluemonday has already policed schemes and attributes).
	sanitized = rewriteRawImageSources(sanitized, dir)
	return Doc{HTML: sanitized, Title: title, Path: path, Dir: dir}, nil
}

// rewriteRawImageSources rewrites relative <img src> values in already-
// sanitized HTML to the /doc-asset/ route, resolved against the document
// directory — the same treatment markdown-syntax images get on the AST.
// It is a token walk (never a regex over HTML): every token except a
// rewritten <img> tag is copied through byte-for-byte, and rewritten tags are
// re-serialized with escaped attribute values. Absolute http(s)/data URIs and
// already-rewritten /doc-asset/ routes are left untouched (RewriteAssetURL
// declines them).
func rewriteRawImageSources(sanitized, dir string) string {
	if !strings.Contains(sanitized, "<img") && !strings.Contains(sanitized, "<source") &&
		!strings.Contains(sanitized, "<video") && !strings.Contains(sanitized, "<audio") {
		return sanitized
	}
	z := xhtml.NewTokenizer(strings.NewReader(sanitized))
	var b strings.Builder
	b.Grow(len(sanitized))
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			if z.Err() == io.EOF {
				return b.String()
			}
			// Tokenizer failed mid-stream (should not happen on bluemonday
			// output). Serve the sanitized HTML unmodified rather than a
			// truncated document — images then simply keep their original src.
			return sanitized
		}
		raw := string(z.Raw())
		if tt == xhtml.StartTagToken || tt == xhtml.SelfClosingTagToken {
			tok := z.Token()
			if mediaElements[tok.Data] {
				changed := false
				for i, attr := range tok.Attr {
					if attr.Namespace != "" {
						continue
					}
					switch attr.Key {
					case "src", "poster":
						if rewritten, ok := RewriteAssetURL(attr.Val, dir); ok {
							tok.Attr[i].Val = rewritten
							changed = true
						}
					case "srcset":
						if rewritten, ok := rewriteSrcset(attr.Val, dir); ok {
							tok.Attr[i].Val = rewritten
							changed = true
						}
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

// mediaElements are the tags whose URL attributes point at files next to the
// document and therefore need the /doc-asset/ route.
var mediaElements = map[string]bool{"img": true, "source": true, "video": true, "audio": true}

// rewriteSrcset rewrites the candidate URLs of a srcset attribute, the form
// `url [descriptor], url [descriptor], …`, leaving descriptors and any remote
// candidates untouched. <picture>/<source> is how a README ships separate light
// and dark logos, and those candidates are relative paths like every other
// image in the document.
func rewriteSrcset(value, dir string) (string, bool) {
	parts := strings.Split(value, ",")
	changed := false
	for i, part := range parts {
		lead := part[:len(part)-len(strings.TrimLeft(part, " \t\n\r\f"))]
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		rewritten, ok := RewriteAssetURL(fields[0], dir)
		if !ok {
			continue
		}
		fields[0] = rewritten
		parts[i] = lead + strings.Join(fields, " ")
		changed = true
	}
	if !changed {
		return "", false
	}
	return strings.Join(parts, ","), true
}

// extractTitle returns the text of the first level-1 heading, or "".
func extractTitle(root ast.Node, src []byte) string {
	var title string
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if h, ok := n.(*ast.Heading); ok && h.Level == 1 {
			title = string(nodeText(h, src))
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(title)
}

// nodeText returns the plain text content of an inline subtree — what a reader
// sees, with markup syntax and raw HTML tags removed. It backs both the window
// title and the heading slugs, so it has to agree with the text GitHub hashes.
func nodeText(n ast.Node, src []byte) []byte {
	var buf bytes.Buffer
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Text:
			buf.Write(t.Segment.Value(src))
		case *ast.String:
			buf.Write(t.Value)
		case *ast.AutoLink:
			buf.Write(t.URL(src))
		case *mathInline:
			// GitHub hashes the text content of its math element, which is the
			// TeX source — so `## The $E=mc^2$ case` slugs the same either way.
			buf.Write(t.Value)
		case *ast.RawHTML:
			// Tags contribute no text; GitHub strips them from slugs too.
		default:
			buf.Write(nodeText(c, src))
		}
	}
	return buf.Bytes()
}

// rewriteImageSources rewrites relative <img> destinations to the scope-checked
// asset route, resolved against the document directory. Absolute http(s)/data
// URLs are left untouched.
func rewriteImageSources(root ast.Node, dir string) {
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		img, ok := n.(*ast.Image)
		if !ok {
			return ast.WalkContinue, nil
		}
		dest := string(img.Destination)
		if rewritten, ok := RewriteAssetURL(dest, dir); ok {
			img.Destination = []byte(rewritten)
		}
		return ast.WalkContinue, nil
	})
}

// capOversizedFences strips the language info from fenced code blocks larger
// than maxHighlightFenceBytes. Without a language (and with language guessing
// off, the default), goldmark-highlighting falls back to a plain escaped
// <pre><code> block — no chroma tokenization, so render time stays bounded.
func capOversizedFences(root ast.Node) {
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		fence, ok := n.(*ast.FencedCodeBlock)
		if !ok || fence.Info == nil {
			return ast.WalkContinue, nil
		}
		size := 0
		lines := fence.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			size += seg.Len()
			if size > maxHighlightFenceBytes {
				fence.Info = nil
				break
			}
		}
		return ast.WalkContinue, nil
	})
}

// RewriteAssetURL converts a local image reference into a /doc-asset/ route.
// It returns ok=false when the URL should be left as-is (remote, data:, or
// already an asset route).
func RewriteAssetURL(dest, dir string) (string, bool) {
	if dest == "" || strings.HasPrefix(dest, AssetRoutePrefix) || strings.HasPrefix(dest, "#") {
		return "", false
	}
	if u, err := url.Parse(dest); err == nil && (u.Scheme != "" || u.Host != "") {
		// http(s)://, data:, file:, etc. — leave remote/schemed URLs alone.
		return "", false
	}
	// Local path: strip any fragment/query-ish suffix conservatively (rare in
	// image refs), decode percent-encoding, resolve against the document dir.
	pathPart := dest
	if i := strings.IndexAny(pathPart, "#?"); i >= 0 {
		pathPart = pathPart[:i]
	}
	if decoded, err := url.PathUnescape(pathPart); err == nil {
		pathPart = decoded
	}
	if pathPart == "" {
		return "", false
	}
	abs := pathPart
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(dir, abs)
	}
	abs = filepath.Clean(abs)
	return AssetRoutePrefix + "?p=" + url.QueryEscape(abs), true
}
