# Rendering Notes

Markdown is parsed and highlighted in Go — `goldmark` for GFM, `chroma` for
code, `bluemonday` for sanitization — and the finished HTML is inlined into the
shell before the webview paints, so the **first paint is the finished document**.

## Budgets

| Path | Budget | Measured |
|---|---|---|
| Cold launch to window | < 250 ms | ~200 ms |
| Warm open (resident) | < 200 ms | ~150 ms |
| In-app navigation | < 50 ms | ~20 ms |

## Highlighting

Chroma emits class-based spans, so code themes flip with the same CSS variables
as the rest of the page — changing the theme re-renders nothing:

```go
func (a *App) RenderDocument(path string) (render.Doc, error) {
	resolved, err := a.scope.Check(path)
	if err != nil {
		return render.Doc{}, fmt.Errorf("outside scope: %w", err)
	}
	return a.renderer.RenderFile(resolved)
}
```

> Any single fence over 50 KB skips highlighting and renders as plain escaped
> `<pre><code>`. Chroma tokenization costs seconds per megabyte, and a bounded
> worst case matters more than colored tokens in a generated dump.

## Reading defaults

- White background, system font, ~72ch column, auto-hiding toolbar
- Light / dark / sepia / follow-system themes, persisted between launches
- Font family, size (`Cmd+=` / `Cmd+-`) and column width adjustable
- Relative links to other markdown files open in-app, with back/forward

This document is the source for the screenshots in this directory; see the
README for how they are produced.
