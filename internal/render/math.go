package render

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// GitHub renders LaTeX in three syntaxes: `$…$` inline, `$$…$$` as a display
// block, and a ```math fence. goldmark has none of them, so plain GFM shows the
// TeX source verbatim.
//
// This file only *marks* math; it never typesets it. Both node kinds render to
// `<code class="language-math">` — the same hook GitHub emits — carrying the
// escaped TeX source, and frontend/src/main.ts lazily loads KaTeX to replace
// them. That keeps the Go side free of a TeX engine and, more importantly,
// leaves a readable fallback (the TeX source itself) if KaTeX never loads. The
// ```math fence needs no code here: goldmark already emits
// `<pre><code class="language-math">` for it.

// KindMathInline and KindMathBlock identify the two math nodes.
var (
	KindMathInline = ast.NewNodeKind("MathInline")
	KindMathBlock  = ast.NewNodeKind("MathBlock")
)

// mathInline is `$…$` (or `$`…`$`) inside a paragraph. Value holds raw TeX.
// display marks the `$$…$$` spelling, which GitHub sets in display style even
// mid-paragraph.
type mathInline struct {
	ast.BaseInline
	Value   []byte
	display bool
}

func (n *mathInline) Kind() ast.NodeKind { return KindMathInline }
func (n *mathInline) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{"Value": string(n.Value)}, nil)
}

// mathBlock is a `$$…$$` display block. Its TeX lives in Lines(), except for
// the single-line `$$ x $$` form, which stores it in Value.
type mathBlock struct {
	ast.BaseBlock
	Value  []byte
	inline bool // opened and closed on the same line
}

func (n *mathBlock) Kind() ast.NodeKind { return KindMathBlock }
func (n *mathBlock) IsRaw() bool        { return true }
func (n *mathBlock) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, nil, nil)
}

// ---------- inline `$…$` ----------

type mathInlineParser struct{}

func (mathInlineParser) Trigger() []byte { return []byte{'$'} }

func (mathInlineParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 2 || line[0] != '$' {
		return nil
	}
	// `$$…$$` mid-paragraph. A `$$` that opens a line was already claimed by
	// mathBlockParser, so reaching here means the delimiters sit inside running
	// text; GitHub still sets those in display style. Declining instead would
	// be worse than not parsing at all: the first `$` would be emitted as text
	// and the parser re-triggered on the second, matching `$a+b$` and leaving a
	// stray dollar on each side of the expression.
	if line[1] == '$' {
		end := bytes.Index(line[2:], []byte("$$"))
		if end < 1 || bytes.IndexByte(line[2:2+end], '\n') >= 0 {
			return nil
		}
		value := line[2 : 2+end]
		block.Advance(2 + end + 2)
		return &mathInline{Value: value, display: true}
	}

	// The `` $`…`$ `` form exists so authors can write TeX containing `$`.
	if line[1] == '`' {
		end := bytes.Index(line[2:], []byte("`$"))
		if end < 0 {
			return nil
		}
		value := line[2 : 2+end]
		block.Advance(2 + end + 2)
		return &mathInline{Value: value}
	}

	// GitHub's guard against prose like "it cost $5 and $10 more": the span may
	// not start or end with whitespace, which those never satisfy.
	end := -1
	for i := 1; i < len(line); i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if line[i] == '\n' {
			break
		}
		if line[i] == '$' {
			end = i
			break
		}
	}
	if end < 2 {
		return nil
	}
	value := line[1:end]
	if isSpaceByte(value[0]) || isSpaceByte(value[len(value)-1]) {
		return nil
	}
	block.Advance(end + 1)
	return &mathInline{Value: value}
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// ---------- block `$$…$$` ----------

type mathBlockParser struct{}

func (mathBlockParser) Trigger() []byte { return []byte{'$'} }

func (mathBlockParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || !bytes.HasPrefix(line[pos:], []byte("$$")) {
		return nil, parser.NoChildren
	}
	rest := bytes.TrimRight(line[pos+2:], "\r\n")
	if end := bytes.Index(rest, []byte("$$")); end >= 0 {
		// `$$ x $$` — complete on the opening line.
		if len(bytes.TrimSpace(rest[end+2:])) != 0 {
			return nil, parser.NoChildren
		}
		return &mathBlock{Value: bytes.TrimSpace(rest[:end]), inline: true}, parser.NoChildren
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		// `$$x` with no closer on this line: not a display block. Leave it to
		// the paragraph parser so the text survives verbatim.
		return nil, parser.NoChildren
	}
	_ = segment
	return &mathBlock{}, parser.NoChildren
}

func (mathBlockParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	if node.(*mathBlock).inline {
		return parser.Close
	}
	line, segment := reader.PeekLine()
	if bytes.Equal(bytes.TrimSpace(line), []byte("$$")) {
		reader.Advance(segment.Len() - 1)
		return parser.Close
	}
	node.Lines().Append(segment)
	return parser.Continue | parser.NoChildren
}

func (mathBlockParser) Close(_ ast.Node, _ text.Reader, _ parser.Context) {}

func (mathBlockParser) CanInterruptParagraph() bool { return true }

func (mathBlockParser) CanAcceptIndentedLine() bool { return false }

// ---------- rendering ----------

type mathRenderer struct{}

func (r mathRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindMathInline, r.renderInline)
	reg.Register(KindMathBlock, r.renderBlock)
}

func (mathRenderer) renderInline(
	w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	m := n.(*mathInline)
	if m.display {
		_, _ = w.WriteString(`<code class="language-math math-display">`)
	} else {
		_, _ = w.WriteString(`<code class="language-math math-inline">`)
	}
	_, _ = w.Write(util.EscapeHTML(m.Value))
	_, _ = w.WriteString(`</code>`)
	return ast.WalkContinue, nil
}

func (mathRenderer) renderBlock(
	w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	m := n.(*mathBlock)
	_, _ = w.WriteString(`<pre class="math-display"><code class="language-math">`)
	if m.inline {
		_, _ = w.Write(util.EscapeHTML(m.Value))
	} else {
		lines := m.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			_, _ = w.Write(util.EscapeHTML(seg.Value(src)))
		}
	}
	_, _ = w.WriteString("</code></pre>\n")
	return ast.WalkContinue, nil
}

// mathParserOptions and mathRendererOption wire the pieces into goldmark.
func mathParserOptions() []parser.Option {
	return []parser.Option{
		parser.WithBlockParsers(util.Prioritized(mathBlockParser{}, 701)),
		parser.WithInlineParsers(util.Prioritized(mathInlineParser{}, 501)),
	}
}

func mathRendererOption() renderer.Option {
	return renderer.WithNodeRenderers(util.Prioritized(mathRenderer{}, 501))
}
