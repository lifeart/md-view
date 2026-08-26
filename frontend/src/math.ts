// Lazily-loaded math typesetting. main.ts imports this module only when the
// rendered document actually contains math, so KaTeX and its fonts cost
// nothing for the documents (the vast majority) that have none.
//
// internal/render/math.go marks math as <code class="language-math"> — the
// same hook GitHub emits — with the TeX source as its text. Until this module
// runs, that source is what the reader sees, which is a legible fallback.
import 'katex/dist/katex.min.css';
import katex from 'katex';

// renderMath replaces every math placeholder under root with typeset output.
// A `<pre>` parent means display math; anything else is inline.
export function renderMath(root: ParentNode): void {
  for (const node of Array.from(root.querySelectorAll<HTMLElement>('code.language-math'))) {
    const pre = node.parentElement;
    // Display style for a $$…$$ block or a ```math fence (both arrive wrapped
    // in <pre>), and for mid-paragraph $$…$$, which the Go side marks with
    // math-display but leaves inline so the <p> stays valid.
    const display = pre?.tagName === 'PRE' || node.classList.contains('math-display');
    const source = node.textContent ?? '';
    // Always a <span>: KaTeX's own .katex-display wrapper carries display:block,
    // so a block-level host would only risk splitting the surrounding <p>.
    const host = document.createElement('span');
    host.className = display ? 'math-display' : 'math-inline';
    // The rendered glyphs carry no text for assistive tech, and MathML output
    // would duplicate the expression into copied text — so keep the TeX source
    // as the accessible name instead.
    host.setAttribute('role', 'math');
    host.setAttribute('aria-label', source);
    katex.render(source, host, {
      displayMode: display,
      output: 'html',
      // A malformed expression renders as its own source in the error colour
      // rather than aborting the whole document.
      throwOnError: false,
      errorColor: 'currentColor',
      strict: false,
      // The document is untrusted input. These are KaTeX's defaults, pinned
      // explicitly because they are the ones that matter: `trust: false`
      // refuses \href, \url and \includegraphics (a link or image KaTeX
      // would build itself, downstream of the Go sanitizer), maxExpand bounds
      // macro recursion, and maxSize bounds \rule-style layout blowups.
      trust: false,
      maxExpand: 1000,
      maxSize: 50,
    });
    (display ? (pre as HTMLElement) : node).replaceWith(host);
  }
}
