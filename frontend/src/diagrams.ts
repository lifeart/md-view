// Lazily-loaded Mermaid diagrams. Like math.ts, this module is imported only
// when a document actually contains a ```mermaid fence — mermaid is by far the
// largest dependency in the bundle and most documents never touch it.
//
// Unlike everything else in MDv, diagrams cannot follow a theme change through
// CSS variables: mermaid bakes colours into the SVG it emits. The markdown
// source is therefore kept on the wrapper so applyTheme can ask for a re-render.
import type { MermaidConfig } from 'mermaid';

let mermaidPromise: Promise<typeof import('mermaid').default> | undefined;

function loadMermaid(): Promise<typeof import('mermaid').default> {
  mermaidPromise ??= import('mermaid').then((m) => m.default);
  return mermaidPromise;
}

function config(dark: boolean): MermaidConfig {
  return {
    startOnLoad: false,
    // 'strict' keeps mermaid from honouring HTML inside diagram labels, which
    // would route around the Go-side sanitizer.
    securityLevel: 'strict',
    theme: dark ? 'dark' : 'default',
    fontFamily: 'var(--font-family)',
    // The document is untrusted: bound how much work one diagram can ask for.
    maxTextSize: 100_000,
    maxEdges: 2_000,
  };
}

// prepare swaps each ```mermaid code block for a wrapper that holds the diagram
// source. It runs synchronously so the code block never flashes on screen.
function prepare(root: ParentNode): HTMLElement[] {
  for (const node of Array.from(root.querySelectorAll<HTMLElement>('code.language-mermaid'))) {
    const pre = node.parentElement;
    if (pre?.tagName !== 'PRE') continue;
    const wrapper = document.createElement('div');
    wrapper.className = 'mermaid-diagram';
    wrapper.dataset.source = node.textContent ?? '';
    pre.replaceWith(wrapper);
  }
  return Array.from(root.querySelectorAll<HTMLElement>('.mermaid-diagram'));
}

let seq = 0;

// renderDiagrams typesets every diagram under root. It rejects on the first
// failure so the caller can surface it; diagrams rendered before that point
// stay on screen, and any that failed keep showing their source.
export async function renderDiagrams(root: ParentNode, dark: boolean): Promise<void> {
  const wrappers = prepare(root);
  if (wrappers.length === 0) return;
  const mermaid = await loadMermaid();
  mermaid.initialize(config(dark));
  let failure: unknown;
  for (const wrapper of wrappers) {
    const source = wrapper.dataset.source ?? '';
    try {
      const { svg } = await mermaid.render(`mermaid-${++seq}`, source);
      wrapper.innerHTML = svg;
    } catch (err) {
      // Leave the source visible so the document still communicates, and
      // remember the error for the caller to report once.
      wrapper.textContent = source;
      failure ??= err;
    }
  }
  if (failure !== undefined) throw failure;
}
