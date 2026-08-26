package render

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// GitHub alerts ("callouts") are a blockquote whose first line is a lone
// `[!NOTE]`-style marker:
//
//	> [!WARNING]
//	> Watch out.
//
// GitHub swallows the marker line and styles the quote; goldmark has no
// extension for them, so plain GFM leaves a literal "[!WARNING]" in the text.
// They are pervasive in modern READMEs, so MDv renders them the way GitHub
// does: the blockquote gets `markdown-alert markdown-alert-<kind>` and a
// leading title paragraph, and frontend/src/theme.css paints the rest.

// alertTitles maps the marker keyword to the label GitHub prints. GitHub
// matches the keyword case-insensitively.
var alertTitles = map[string]string{
	"NOTE":      "Note",
	"TIP":       "Tip",
	"IMPORTANT": "Important",
	"WARNING":   "Warning",
	"CAUTION":   "Caution",
}

// alertTransformer runs as a paragraph transformer, i.e. during block parsing
// and before inline parsing. That ordering matters: at this point the marker is
// still a plain source line we can drop wholesale, whereas after inline parsing
// it has already been split into text nodes.
type alertTransformer struct{}

// Transform implements parser.ParagraphTransformer.
func (alertTransformer) Transform(node *ast.Paragraph, reader text.Reader, _ parser.Context) {
	quote, ok := node.Parent().(*ast.Blockquote)
	if !ok || quote.FirstChild() != node {
		return
	}
	lines := node.Lines()
	if lines.Len() == 0 {
		return
	}
	first := lines.At(0)
	kind, ok := alertMarker(first.Value(reader.Source()))
	if !ok {
		return
	}

	quote.SetAttributeString("class",
		[]byte("markdown-alert markdown-alert-"+strings.ToLower(kind)))

	// Drop the marker line. A marker-only blockquote (`> [!NOTE]` and nothing
	// else) leaves an empty paragraph behind, which would render as a stray
	// blank line, so remove the paragraph itself in that case.
	if lines.Len() == 1 {
		quote.RemoveChild(quote, node)
	} else {
		lines.SetSliced(1, lines.Len())
	}

	// The title paragraph is added later, by insertAlertTitles: inline parsing
	// has not run yet, and goldmark walks every child of every block looking
	// for lines to parse, so an inline child attached here would panic.
}

// insertAlertTitles prepends GitHub's visible label ("Note", "Warning", …) to
// each alert. It runs after parsing, once no further block walks will happen.
func insertAlertTitles(root ast.Node) {
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		quote, ok := n.(*ast.Blockquote)
		if !ok {
			return ast.WalkContinue, nil
		}
		class, ok := quote.AttributeString("class")
		if !ok {
			return ast.WalkContinue, nil
		}
		kind, ok := alertKindFromClass(class)
		if !ok {
			return ast.WalkContinue, nil
		}
		title := ast.NewParagraph()
		title.SetAttributeString("class", []byte("markdown-alert-title"))
		title.AppendChild(title, ast.NewString([]byte(alertTitles[kind])))
		if first := quote.FirstChild(); first != nil {
			quote.InsertBefore(quote, first, title)
		} else {
			quote.AppendChild(quote, title)
		}
		return ast.WalkContinue, nil
	})
}

// alertKindFromClass recovers the alert keyword from the class set by
// alertTransformer.
func alertKindFromClass(class any) (string, bool) {
	var value string
	switch v := class.(type) {
	case []byte:
		value = string(v)
	case string:
		value = v
	default:
		return "", false
	}
	_, suffix, found := strings.Cut(value, "markdown-alert-")
	if !found {
		return "", false
	}
	kind := strings.ToUpper(suffix)
	if _, ok := alertTitles[kind]; !ok {
		return "", false
	}
	return kind, true
}

// alertMarker reports the alert keyword when line is exactly a `[!KIND]`
// marker, ignoring surrounding whitespace. Anything trailing on the same line
// disqualifies it, which is also GitHub's rule.
func alertMarker(line []byte) (string, bool) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) < 4 || trimmed[0] != '[' || trimmed[1] != '!' || trimmed[len(trimmed)-1] != ']' {
		return "", false
	}
	kind := strings.ToUpper(string(trimmed[2 : len(trimmed)-1]))
	if _, ok := alertTitles[kind]; !ok {
		return "", false
	}
	return kind, true
}
