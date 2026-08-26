package render

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/ast"
)

// Heading anchors follow GitHub's slugger rather than goldmark's built-in
// auto-heading-id, so that a table of contents copied from a README
// (`[Setup](#setup)`) resolves to the same anchors it does on github.com.
//
// Two things differ from goldmark's generator and both break real documents:
// goldmark drops every non-ASCII rune (so `## Über uns` becomes `#ber-uns`
// instead of GitHub's `#über-uns`), and it slugs the *raw* heading line, so
// markup syntax leaks into the id (`## [Docs](x)` becomes `#docsx` instead of
// `#docs`). We slug the heading's text content instead, matching what GitHub
// hashes.

// slugify reproduces github-slugger: lower-case, drop everything that is not a
// letter, a decimal digit, a combining mark, `-` or `_`, and turn spaces into
// dashes. Note that only U+0020 becomes a dash — other whitespace is dropped —
// and that no trimming happens, which is why `## 1. Alerts` keeps its double
// dash on GitHub too.
func slugify(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_':
			b.WriteRune(r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) ||
			unicode.In(r, unicode.Mn, unicode.Mc, unicode.Me):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// slugger allocates unique ids within one document, appending `-1`, `-2`, … to
// repeats exactly as GitHub does.
type slugger struct {
	seen map[string]int
}

func newSlugger() *slugger { return &slugger{seen: map[string]int{}} }

func (s *slugger) slug(text string) string {
	base := slugify(text)
	if base == "" {
		// GitHub emits an empty id here; an empty id is useless as a link
		// target, so fall back to a stable positional name.
		base = "section"
	}
	slug := base
	for n, taken := s.seen[slug]; taken; n, taken = s.seen[slug] {
		s.seen[base] = n + 1
		slug = base + "-" + strconv.Itoa(n+1)
	}
	s.seen[slug] = 0
	return slug
}

// assignHeadingIDs sets the `id` attribute on every heading in the tree. It
// replaces parser.WithAutoHeadingID(), which must therefore stay off.
func assignHeadingIDs(root ast.Node, src []byte) {
	s := newSlugger()
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		h.SetAttributeString("id", []byte(s.slug(string(nodeText(h, src)))))
		return ast.WalkSkipChildren, nil
	})
}
