// Plain-text serialization of a selection, for the `copy` handler in main.ts.
//
// The handler used to put `Selection.toString()` on the pasteboard. WebKit
// builds that string from the laid-out text runs, and everything a list is
// made of lives outside them: the bullet and the number are CSS markers, the
// nesting is padding, and a list is not separated from what follows it by a
// blank line the way two paragraphs are. Copying
//
//     - first
//       - nested
//     - second
//
//     Next paragraph.
//
// therefore yielded "first\nnested\nsecond\nNext paragraph." — the structure
// gone and the paragraph welded onto the last item, which is exactly the
// reformatting-by-hand this replaces.
//
// So we walk the DOM ourselves, clipped to the selection, and re-emit the
// block structure the reader can see: one blank line between blocks, list
// markers with their nesting, "> " for quotes, code and math verbatim.
// Inline formatting stays plain — bold copies as bold's text, a link as its
// label — because the payload is plain text, not markdown source.

interface Ctx {
  range: Range;
}

// A serialized block, plus whether it is a list: a list attaches to the text
// above it inside a list item without a blank line, or the item turns loose.
interface Block {
  md: string;
  list: boolean;
}

// Elements whose text is chrome, not document: the hover "Copy" button on a
// code fence is inside the content container and would otherwise be copied
// along with the code it copies.
const SKIP_TAGS = new Set([
  'BUTTON', 'IFRAME', 'NOSCRIPT', 'OPTION', 'SCRIPT', 'SELECT', 'STYLE', 'TEMPLATE',
]);

const BLOCK_TAGS = new Set([
  'ADDRESS', 'ARTICLE', 'ASIDE', 'AUDIO', 'BLOCKQUOTE', 'DD', 'DETAILS', 'DIV', 'DL',
  'DT', 'FIELDSET', 'FIGCAPTION', 'FIGURE', 'FOOTER', 'FORM', 'H1', 'H2', 'H3', 'H4',
  'H5', 'H6', 'HEADER', 'HR', 'LI', 'MAIN', 'NAV', 'OL', 'P', 'PRE', 'SECTION',
  'SUMMARY', 'TABLE', 'UL', 'VIDEO',
]);

// Cells are reached through their table, which needs the whole row to lay the
// columns out; a selection that lands on one is lifted to it.
const TABLE_PARTS = new Set(['TBODY', 'TD', 'TFOOT', 'TH', 'THEAD', 'TR']);

// selectionToText renders the part of `root` that `sel` covers as plain text.
// It returns '' when the selection does not reach into the document body (a
// drag across the toolbar, say), which tells the caller to leave the payload
// to the browser.
export function selectionToText(sel: Selection, root: HTMLElement): string {
  const parts: string[] = [];
  for (let i = 0; i < sel.rangeCount; i++) {
    const range = sel.getRangeAt(i);
    if (range.collapsed || !range.intersectsNode(root)) continue;
    const text = serializeRange(range, root);
    if (text) parts.push(text);
  }
  return parts.join('\n\n');
}

function serializeRange(range: Range, root: HTMLElement): string {
  const ctx: Ctx = { range };
  let node: Node = range.commonAncestorContainer;
  // A selection that starts above the article (Cmd+A selects the toolbar too)
  // is clamped to the document: the range walk below keeps the rest honest.
  if (!root.contains(node)) node = root;
  if (node.nodeType === Node.TEXT_NODE) {
    return tidy(collapse(textOf(node as Text, covers(range, node), ctx)));
  }
  if (node.nodeType !== Node.ELEMENT_NODE) return '';
  let el = node as Element;
  if (TABLE_PARTS.has(el.tagName)) el = el.closest('table') ?? el;
  const full = covers(range, el);
  if (isBlock(el)) {
    const block = renderBlock(el, full, ctx);
    return block ? block.md : '';
  }
  return tidy(inlineOf(el, full, ctx));
}

// ---------- walking the selection ----------
//
// Every function below carries a `full` flag: whether the node it was handed
// lies wholly inside the selection. It matters for speed, not just for
// clipping. Asking the Range about each node in turn costs ~14 µs in WebKit,
// which is a fifth of a second of Cmd+C on a 100 KB document; but a selection
// is one contiguous span, so only the two boundary paths are ever in doubt.
// Once a node is known to be fully covered, so are all of its descendants,
// and the walk below never touches the Range again.

// eachChild visits the children of `parent` that the selection reaches,
// telling each whether it in turn is wholly covered. For a covered parent
// that is every child, with no Range queries at all.
function eachChild(
  parent: Node,
  full: boolean,
  ctx: Ctx,
  visit: (child: Node, childFull: boolean) => void,
): void {
  const kids = parent.childNodes;
  if (full) {
    for (let i = 0; i < kids.length; i++) visit(kids[i], true);
    return;
  }
  const bounds = childBounds(parent, ctx.range, kids.length);
  for (let i = bounds.from; i <= bounds.to; i++) {
    visit(kids[i], !(i === bounds.from && bounds.fromPartial) && !(i === bounds.to && bounds.toPartial));
  }
}

interface Bounds {
  from: number;
  to: number;
  fromPartial: boolean;
  toPartial: boolean;
}

// childBounds locates the selection's edges among a partly-covered node's
// children: the first and last child it reaches, and whether those two are
// themselves cut through (in which case they are walked again one level down).
function childBounds(parent: Node, range: Range, count: number): Bounds {
  const bounds: Bounds = { from: 0, to: count - 1, fromPartial: false, toPartial: false };
  if (parent.contains(range.startContainer)) {
    if (range.startContainer === parent) {
      bounds.from = range.startOffset;
    } else {
      bounds.from = indexIn(parent, range.startContainer);
      bounds.fromPartial = true;
    }
  }
  if (parent.contains(range.endContainer)) {
    if (range.endContainer === parent) {
      bounds.to = range.endOffset - 1;
    } else {
      bounds.to = indexIn(parent, range.endContainer);
      bounds.toPartial = true;
    }
  }
  return bounds;
}

// indexIn returns the index, among parent's children, of the one that holds
// `node`.
function indexIn(parent: Node, node: Node): number {
  let child: Node = node;
  while (child.parentNode && child.parentNode !== parent) child = child.parentNode;
  const kids = parent.childNodes;
  for (let i = 0; i < kids.length; i++) {
    if (kids[i] === child) return i;
  }
  return 0;
}

// covers reports whether the range swallows the node whole.
function covers(range: Range, node: Node): boolean {
  const end = node.nodeType === Node.TEXT_NODE
    ? (node as Text).length
    : node.childNodes.length;
  return range.comparePoint(node, 0) === 0 && range.comparePoint(node, end) === 0;
}

// ---------- blocks ----------

function isBlock(el: Element): boolean {
  // math.ts lifts display math out of its <pre> into a <span> so it can sit in
  // a paragraph; it is still a block on screen.
  return BLOCK_TAGS.has(el.tagName) || el.classList.contains('math-display');
}

// blocksOf serializes a container's children, gathering runs of inline nodes
// into implicit paragraphs so that stray text beside a block still survives.
function blocksOf(parent: Node, full: boolean, ctx: Ctx): Block[] {
  const out: Block[] = [];
  let buf = '';
  const flush = (): void => {
    const text = tidy(buf);
    if (text) out.push({ md: text, list: false });
    buf = '';
  };
  eachChild(parent, full, ctx, (child, childFull) => {
    if (child.nodeType === Node.ELEMENT_NODE && isBlock(child as Element)) {
      flush();
      const block = renderBlock(child as Element, childFull, ctx);
      if (block && block.md) out.push(block);
    } else {
      buf += inlineOf(child, childFull, ctx);
    }
  });
  flush();
  return out;
}

function joinBlocks(blocks: Block[]): string {
  return blocks.map((b) => b.md).join('\n\n');
}

function renderBlock(el: Element, full: boolean, ctx: Ctx): Block | null {
  if (SKIP_TAGS.has(el.tagName) || (el as HTMLElement).hidden) return null;
  switch (el.tagName) {
    case 'HR':
      return { md: '---', list: false };
    case 'PRE':
      return { md: verbatim(el, full, ctx), list: false };
    case 'UL':
    case 'OL':
      return renderList(el, full, ctx);
    case 'LI':
      // Only reached when the selection itself is rooted at one item.
      return { md: joinItemBlocks(blocksOf(el, full, ctx)), list: false };
    case 'BLOCKQUOTE':
      return renderQuote(el, full, ctx);
    case 'TABLE':
      return renderTable(el, full, ctx);
    case 'P':
    case 'H1':
    case 'H2':
    case 'H3':
    case 'H4':
    case 'H5':
    case 'H6':
    case 'SUMMARY':
    case 'DT':
    case 'DD':
    case 'FIGCAPTION':
      return { md: tidy(inlineOf(el, full, ctx)), list: false };
    case 'DIV':
      // A drawn diagram is an SVG full of label text that would copy as a
      // jumble; the source it was drawn from is the readable form.
      if (el.classList.contains('mermaid-diagram')) {
        const source = (el as HTMLElement).dataset.source ?? '';
        return { md: source.replace(/\s+$/, ''), list: false };
      }
      break;
    default:
      break;
  }
  // Typeset display math: KaTeX's glyph spans carry no usable text, so copy
  // the TeX source math.ts parked on the host as its accessible name.
  if (el.getAttribute('role') === 'math') {
    return { md: (el.getAttribute('aria-label') ?? '').trim(), list: false };
  }
  return { md: joinBlocks(blocksOf(el, full, ctx)), list: false };
}

function renderList(el: Element, full: boolean, ctx: Ctx): Block | null {
  const ordered = el.tagName === 'OL';
  const reversed = ordered && el.hasAttribute('reversed');
  const items = Array.from(el.children).filter((c) => c.tagName === 'LI');
  const start = Number(el.getAttribute('start') ?? (reversed ? items.length : 1));
  // Numbering follows the document, not the selection, so every item is
  // counted — copying items 3–5 of a list keeps them numbered 3–5.
  const number = new Map<Node, number>();
  let n = Number.isFinite(start) ? start : 1;
  for (const li of items) {
    number.set(li, n);
    n += reversed ? -1 : 1;
  }
  const lines: string[] = [];
  let loose = false;
  eachChild(el, full, ctx, (child, childFull) => {
    if (child.nodeType !== Node.ELEMENT_NODE) return;
    const li = child as Element;
    if (li.tagName !== 'LI') return;
    const blocks = blocksOf(li, childFull, ctx);
    // An item holding more than one paragraph makes the whole list loose, and
    // loose items need the blank line between them to stay separate.
    if (blocks.filter((b) => !b.list).length > 1) loose = true;
    const body = joinItemBlocks(blocks);
    // An item the selection only grazes contributes nothing; a bare marker
    // with no text after it is worse than leaving it out.
    if (!body) return;
    const bullet = ordered ? `${number.get(li) ?? 1}. ` : '- ';
    lines.push(bullet + indentRest(taskMarker(li) + body, ' '.repeat(bullet.length)));
  });
  if (lines.length === 0) return null;
  return { md: lines.join(loose ? '\n\n' : '\n'), list: true };
}

// joinItemBlocks glues a list item's blocks together. A nested list follows
// its item's text directly; anything else is a new paragraph.
function joinItemBlocks(blocks: Block[]): string {
  let out = '';
  for (const block of blocks) {
    if (!out) out = block.md;
    else out += (block.list ? '\n' : '\n\n') + block.md;
  }
  return out;
}

function taskMarker(li: Element): string {
  const box = li.querySelector<HTMLInputElement>(
    ':scope > input[type="checkbox"], :scope > p:first-of-type > input[type="checkbox"]',
  );
  if (!box) return '';
  return box.checked ? '[x] ' : '[ ] ';
}

function indentRest(text: string, pad: string): string {
  return text
    .split('\n')
    .map((line, i) => (i === 0 || line === '' ? line : pad + line))
    .join('\n');
}

function renderQuote(el: Element, full: boolean, ctx: Ctx): Block {
  const body = joinBlocks(blocksOf(el, full, ctx));
  const md = body
    .split('\n')
    .map((line) => (line ? `> ${line}` : '>'))
    .join('\n');
  return { md, list: false };
}

// Tab-separated rows: the same shape WebKit produced, which pastes into a
// spreadsheet as columns.
function renderTable(el: Element, full: boolean, ctx: Ctx): Block | null {
  const rows: string[] = [];
  const readRow = (tr: Node, rowFull: boolean): void => {
    const cells: string[] = [];
    eachChild(tr, rowFull, ctx, (cell, cellFull) => {
      if (cell.nodeType !== Node.ELEMENT_NODE) return;
      const tag = (cell as Element).tagName;
      if (tag !== 'TD' && tag !== 'TH') return;
      cells.push(tidy(inlineOf(cell, cellFull, ctx)).replace(/\n+/g, ' '));
    });
    if (cells.length > 0) rows.push(cells.join('\t'));
  };
  const walk = (node: Node, nodeFull: boolean): void => {
    eachChild(node, nodeFull, ctx, (child, childFull) => {
      if (child.nodeType !== Node.ELEMENT_NODE) return;
      const tag = (child as Element).tagName;
      if (tag === 'TR') readRow(child, childFull);
      else if (tag === 'THEAD' || tag === 'TBODY' || tag === 'TFOOT') walk(child, childFull);
    });
  };
  walk(el, full);
  if (rows.length === 0) return null;
  return { md: rows.join('\n'), list: false };
}

// ---------- inline ----------

function inlineOf(node: Node, full: boolean, ctx: Ctx): string {
  if (node.nodeType === Node.TEXT_NODE) return collapse(textOf(node as Text, full, ctx));
  if (node.nodeType !== Node.ELEMENT_NODE) return '';
  const el = node as Element;
  if (SKIP_TAGS.has(el.tagName) || (el as HTMLElement).hidden) return '';
  switch (el.tagName) {
    case 'BR':
      return '\n';
    // The task-list checkbox is rendered as the item's marker instead.
    case 'INPUT':
      return '';
    case 'IMG':
      return (el.getAttribute('alt') ?? '').trim();
    default:
      break;
  }
  // Typeset inline math, as in renderBlock.
  if (el.getAttribute('role') === 'math') return (el.getAttribute('aria-label') ?? '').trim();
  let out = '';
  eachChild(el, full, ctx, (child, childFull) => {
    out += inlineOf(child, childFull, ctx);
  });
  return out;
}

// ---------- text ----------

// textOf returns the part of a text node the selection covers, so a selection
// that starts or ends mid-word copies mid-word.
function textOf(node: Text, full: boolean, ctx: Ctx): string {
  if (full) return node.data;
  const range = ctx.range;
  const from = node === range.startContainer ? range.startOffset : 0;
  const to = node === range.endContainer ? range.endOffset : node.data.length;
  return node.data.slice(from, to);
}

// collapse does what `white-space: normal` does to a text node: the newlines
// goldmark leaves between tags are layout, not content.
function collapse(text: string): string {
  return text.replace(/\s+/g, ' ');
}

// tidy trims a finished inline run into a block: no space hugging a <br>, no
// runs of blank lines, no leading or trailing whitespace.
function tidy(text: string): string {
  return text
    .replace(/[ \t]*\n[ \t]*/g, '\n')
    .replace(/\n{3,}/g, '\n\n')
    .trim();
}

// verbatim reads a <pre> exactly as written — the one place where whitespace
// is content. Used for code fences and for math and diagram sources before
// their lazy renderers have replaced them.
function verbatim(el: Element, full: boolean, ctx: Ctx): string {
  let out = '';
  const walk = (node: Node, nodeFull: boolean): void => {
    if (node.nodeType === Node.TEXT_NODE) {
      out += textOf(node as Text, nodeFull, ctx);
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    const child = node as Element;
    if (SKIP_TAGS.has(child.tagName)) return;
    if (child.tagName === 'BR') {
      out += '\n';
      return;
    }
    eachChild(child, nodeFull, ctx, walk);
  };
  walk(el, full);
  return out.replace(/\n+$/, '');
}
