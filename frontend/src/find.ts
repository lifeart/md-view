// Find in page (Cmd+F), for main.ts.
//
// A system webview has no find bar of its own — WKWebView leaves that to the
// host app — so until this existed Cmd+F did nothing at all.
//
// Matches are painted with the CSS Custom Highlight API rather than by
// wrapping them in <mark>. The document's DOM has other readers: copy.ts
// serializes it, KaTeX and Mermaid replace parts of it after first paint, a
// theme change redraws the diagrams. A highlight is a range registered with
// the engine, so every one of them keeps seeing the tree Go rendered — and
// typing a query never reflows the document.
//
// The search runs over one string: every text node under the root, in order,
// verbatim. An offset into it is therefore a node's start plus an offset into
// that node, with no mapping table to build or keep in sync. Two things make
// the string say what the reader sees:
//
//   - a match may cross inline markup ("hidden **content**" is one phrase on
//     screen) but not a block boundary, so blocks are separated by a character
//     no query can contain;
//   - whitespace in the query matches any run of whitespace, because the
//     newline goldmark leaves inside a wrapped paragraph renders as a space.

import { BLOCK_TAGS, SKIP_TAGS, TABLE_PARTS } from './copy';

export interface FindElements {
  bar: HTMLElement;
  input: HTMLInputElement;
  count: HTMLElement;
  prev: HTMLButtonElement;
  next: HTMLButtonElement;
  close: HTMLButtonElement;
}

export interface Find {
  open(): void;
  close(): void;
  step(dir: 1 | -1): void;
}

interface Index {
  text: string;
  nodes: Text[];
  // starts[i] is where nodes[i].data begins in text.
  starts: number[];
}

// Every match of the query, as where it begins and ends in the index text.
// Two numbers each rather than a range: a letter typed into a megabyte of
// prose matches a hundred thousand times, and only the matches near the
// window ever need to become ranges (see PAINT_LIMIT).
interface Matches {
  index: Index;
  from: number[];
  to: number[];
}

const NONE: Matches = { index: { text: '', nodes: [], starts: [] }, from: [], to: [] };

// Separates blocks in the index. The HTML parser never lets U+0000 into a text
// node and nobody can type one into the field, so no match can straddle it.
const BLOCK_BREAK = '\u0000';

// A drawn diagram is SVG, which has no blocks: each label is its own group.
const SVG_BREAKS = new Set(['FOREIGNOBJECT', 'G', 'TEXT']);

// What <audio> and <video> contain is fallback text for engines that cannot
// play them. It is never on screen, so a match in it would lead nowhere.
const UNRENDERED = new Set(['AUDIO', 'VIDEO']);

// How many matches are registered for painting at once. WebKit checks every
// registered range against each run of text it paints, on every frame: with
// 10,000 of them a scrolled frame of a 1 MB document took 80–220 ms instead
// of 16, and with 250 there was nothing to measure. So past this number only
// the matches around the window are registered, and the set follows the
// scroll position.
const PAINT_LIMIT = 1000;

// Keeps the current match clear of the window's edges when scrolling to it.
const EDGE = 48;

function buildIndex(root: HTMLElement): Index {
  const parts: string[] = [];
  const nodes: Text[] = [];
  const starts: number[] = [];
  let length = 0;
  // Whether anything was added since the last break, so that nested blocks
  // (li > p, blockquote > p) do not pile up separators.
  let dirty = false;
  const add = (text: string): void => {
    parts.push(text);
    length += text.length;
  };
  const breakBlock = (): void => {
    if (!dirty) return;
    add(BLOCK_BREAK);
    dirty = false;
  };
  const walk = (parent: Node): void => {
    for (let node = parent.firstChild; node; node = node.nextSibling) {
      if (node.nodeType === Node.TEXT_NODE) {
        const text = node as Text;
        if (text.length === 0) continue;
        nodes.push(text);
        starts.push(length);
        add(text.data);
        dirty = true;
        continue;
      }
      if (node.nodeType !== Node.ELEMENT_NODE) continue;
      const el = node as Element;
      // SVG keeps its tag names in lower case; HTML reports them in upper.
      const tag = el.tagName.toUpperCase();
      if (SKIP_TAGS.has(tag) || UNRENDERED.has(tag) || (el as HTMLElement).hidden) continue;
      if (tag === 'BR') {
        // A line break is whitespace to a phrase that continues past it. No
        // node holds this character; toRange() allows for that.
        add('\n');
        continue;
      }
      const block = BLOCK_TAGS.has(tag) || TABLE_PARTS.has(tag) || SVG_BREAKS.has(tag);
      if (block) breakBlock();
      walk(el);
      if (block) breakBlock();
    }
  };
  walk(root);
  return { text: parts.join(''), nodes, starts };
}

// toPattern turns what was typed into a literal, case-insensitive search in
// which any whitespace stands for any run of whitespace.
function toPattern(query: string): RegExp {
  const literal = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return new RegExp(literal.replace(/\s+/g, '\\s+'), 'giu');
}

function collect(index: Index, pattern: RegExp): Matches {
  const found: Matches = { index, from: [], to: [] };
  for (let m = pattern.exec(index.text); m; m = pattern.exec(index.text)) {
    found.from.push(m.index);
    found.to.push(m.index + m[0].length);
  }
  return found;
}

// lowerBound returns the first position in a sorted list holding `value` or
// more — the list's length when there is none.
function lowerBound(sorted: number[], value: number): number {
  let lo = 0;
  let hi = sorted.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (sorted[mid] < value) lo = mid + 1;
    else hi = mid;
  }
  return lo;
}

// nodeAt finds the last text node that starts at or before `offset`.
function nodeAt(index: Index, offset: number): number {
  return Math.max(lowerBound(index.starts, offset + 1) - 1, 0);
}

function clamp(offset: number, node: Text): number {
  return Math.min(Math.max(offset, 0), node.length);
}

// toRange maps a match back onto the DOM. Every match holds at least one
// character that is not whitespace, so it always lands on real text; only its
// edges can fall on the newline of a <br>, which sits between two text nodes
// and belongs to neither — hence the clamping.
function toRange(index: Index, from: number, to: number): StaticRange {
  const { nodes, starts } = index;
  let k = nodeAt(index, from);
  let startContainer = nodes[k];
  let startOffset = from - starts[k];
  if (startOffset >= startContainer.length && k + 1 < nodes.length) {
    startContainer = nodes[k + 1];
    startOffset = 0;
  }
  k = nodeAt(index, to - 1);
  const endContainer = nodes[k];
  return new StaticRange({
    startContainer,
    startOffset: clamp(startOffset, startContainer),
    endContainer,
    endOffset: clamp(to - starts[k], endContainer),
  });
}

function liveRange(range: StaticRange): Range {
  const live = document.createRange();
  live.setStart(range.startContainer, range.startOffset);
  live.setEnd(range.endContainer, range.endOffset);
  return live;
}

export function createFind(root: HTMLElement, ui: FindElements): Find {
  // Null on an engine without the Highlight API (WebKit before Safari 17.2,
  // so macOS 14.1 and older); see outline().
  const marks = 'highlights' in CSS
    ? { all: new Highlight(), current: new Highlight() }
    : null;
  // Both highlights hold the current match; this one has to win.
  if (marks) marks.current.priority = 1;

  // The index is dropped whenever the document changes and rebuilt by the
  // next search, so it is only ever as old as the DOM it was read from.
  let index: Index | null = null;
  let found = NONE;
  // -1 when there are matches but none of them is the current one: the
  // document changed under the search, and the next step starts from what is
  // on screen rather than from a position in a document that is gone.
  let current = -1;
  let currentRange: StaticRange | null = null;
  // The slice of `found` registered for painting.
  let painted = { from: 0, to: 0 };

  function rangeAt(i: number): StaticRange {
    return toRange(found.index, found.from[i], found.to[i]);
  }

  // Matches are in document order, so where they sit on screen is sorted too
  // (near enough), and a binary search finds the first one at or below the top
  // of the window in a dozen layout reads however many matches there are.
  function firstInView(): number {
    let lo = 0;
    let hi = found.from.length;
    while (lo < hi) {
      const mid = (lo + hi) >> 1;
      if (liveRange(rangeAt(mid)).getBoundingClientRect().bottom <= 0) lo = mid + 1;
      else hi = mid;
    }
    return lo < found.from.length ? lo : 0;
  }

  // paintMatches registers the matches to highlight: all of them when they
  // are few, otherwise the ones around the window. With `force` unset it only
  // checks that the window has not scrolled out of what is registered.
  function paintMatches(force: boolean): void {
    if (!marks) return;
    const n = found.from.length;
    let from = 0;
    let to = n;
    if (n > PAINT_LIMIT) {
      const first = firstInView();
      const covered = first >= painted.from &&
        (painted.to === n || first + PAINT_LIMIT / 2 <= painted.to);
      if (!force && covered) return;
      from = Math.max(first - PAINT_LIMIT / 4, 0);
      to = Math.min(from + PAINT_LIMIT, n);
    } else if (!force) {
      return;
    }
    marks.all.clear();
    for (let i = from; i < to; i++) marks.all.add(rangeAt(i));
    painted = { from, to };
  }

  let scrollQueued = false;
  function onScroll(): void {
    if (scrollQueued || found.from.length <= PAINT_LIMIT) return;
    scrollQueued = true;
    requestAnimationFrame(() => {
      scrollQueued = false;
      if (!ui.bar.hidden) paintMatches(false);
    });
  }

  // search recomputes the matches. `typed` is whether the reader changed the
  // query: only then may the view move. The other caller is the document
  // changing on its own (math landing, a diagram redrawn for a new theme, a
  // navigation), which must not scroll anything.
  function search(typed: boolean): void {
    const query = ui.input.value;
    const before = current >= 0 ? found.from[current] : -1;
    const beforeRange = currentRange;
    const sameText = index !== null;
    // Nothing but spaces would match between every two words of the document.
    if (query.trim()) {
      index ??= buildIndex(root);
      found = collect(index, toPattern(query));
    } else {
      found = NONE;
    }
    current = -1;
    const n = found.from.length;
    if (n > 0 && typed) {
      // Typing one more letter narrows the match the reader is on instead of
      // jumping back to the top; with no match to narrow, start from what is
      // on screen.
      current = before >= 0 && sameText ? lowerBound(found.from, before) : firstInView();
      if (current >= n) current = 0;
    } else if (n > 0 && beforeRange) {
      // Same text node, same place in it: the current match survived whatever
      // changed around it.
      const k = found.index.nodes.indexOf(beforeRange.startContainer as Text);
      if (k >= 0) {
        const offset = found.index.starts[k] + beforeRange.startOffset;
        const i = lowerBound(found.from, offset);
        if (found.from[i] === offset) current = i;
      }
    }
    if (typed && current >= 0) reveal();
    paintMatches(true);
    showCurrent();
  }

  function showCurrent(): void {
    currentRange = current >= 0 ? rangeAt(current) : null;
    if (marks) {
      marks.current.clear();
      if (currentRange) marks.current.add(currentRange);
    } else {
      outline(currentRange);
    }
    const n = found.from.length;
    const asked = ui.input.value.trim() !== '';
    ui.prev.disabled = n === 0;
    ui.next.disabled = n === 0;
    ui.count.classList.toggle('none', asked && n === 0);
    if (!asked) ui.count.textContent = '';
    else if (n === 0) ui.count.textContent = 'No results';
    else if (current >= 0) ui.count.textContent = `${current + 1} of ${n}`;
    else ui.count.textContent = n === 1 ? '1 match' : `${n} matches`;
  }

  // reveal brings the current match on screen, wherever it is hiding.
  function reveal(): void {
    const range = liveRange(rangeAt(current));
    const parent = range.startContainer.parentElement;
    // A collapsed <details> has no box for its contents to scroll to.
    for (let el = parent; el && el !== root; el = el.parentElement) {
      if (el instanceof HTMLDetailsElement && !el.open) el.open = true;
    }
    // Long code lines and wide tables scroll sideways inside their own box.
    for (let el = parent; el && el !== root; el = el.parentElement) {
      if (el.scrollWidth <= el.clientWidth) continue;
      const box = el.getBoundingClientRect();
      const at = range.getBoundingClientRect();
      if (at.left < box.left || at.right > box.right) {
        el.scrollLeft += at.left - box.left - el.clientWidth / 2;
      }
    }
    let at = range.getBoundingClientRect();
    if (at.top < EDGE || at.bottom > window.innerHeight - EDGE) {
      window.scrollBy(0, at.top + at.height / 2 - window.innerHeight / 2);
      at = range.getBoundingClientRect();
    }
    // The bar floats over the document's top right corner, and a match on the
    // first lines cannot be scrolled out from under it — so the bar moves.
    ui.bar.style.removeProperty('--find-dodge');
    const bar = ui.bar.getBoundingClientRect();
    if (at.bottom > bar.top && at.top < bar.bottom && at.right > bar.left && at.left < bar.right) {
      ui.bar.style.setProperty('--find-dodge', `${Math.ceil(at.bottom - bar.top) + 8}px`);
    }
  }

  // outline is the fallback for engines that cannot paint a range: it draws
  // boxes over the current match instead. Only the current one — positioned
  // boxes have to be laid out again whenever the document reflows, which is
  // affordable for one match and not for thousands.
  let overlay: HTMLElement | null = null;
  function outline(range: StaticRange | null): void {
    overlay?.remove();
    overlay = null;
    if (!range) return;
    overlay = document.createElement('div');
    overlay.className = 'find-overlay';
    for (const rect of Array.from(liveRange(range).getClientRects())) {
      const box = document.createElement('div');
      box.style.left = `${rect.left + window.scrollX}px`;
      box.style.top = `${rect.top + window.scrollY}px`;
      box.style.width = `${rect.width}px`;
      box.style.height = `${rect.height}px`;
      overlay.appendChild(box);
    }
    document.body.appendChild(overlay);
  }
  if (!marks) {
    window.addEventListener('resize', () => outline(currentRange));
  }

  // Anything that changes the document's text invalidates every offset held
  // here. Watching only while the bar is open keeps find free when unused.
  const observer = new MutationObserver(() => {
    index = null;
    search(false);
  });

  function open(): void {
    if (ui.bar.hidden) {
      ui.bar.hidden = false;
      if (marks) {
        CSS.highlights.set('find-match', marks.all);
        CSS.highlights.set('find-current', marks.current);
      }
      observer.observe(root, { childList: true, subtree: true, characterData: true });
      window.addEventListener('scroll', onScroll, { passive: true });
      window.addEventListener('resize', onScroll);
      // The last query is still in the field: show its matches, but leave
      // the view where it is until the reader asks for one.
      search(false);
    }
    ui.input.focus();
    ui.input.select();
  }

  function close(): void {
    if (ui.bar.hidden) return;
    observer.disconnect();
    window.removeEventListener('scroll', onScroll);
    window.removeEventListener('resize', onScroll);
    const last = currentRange ? liveRange(currentRange) : null;
    index = null;
    found = NONE;
    current = -1;
    currentRange = null;
    if (marks) {
      marks.all.clear();
      marks.current.clear();
      CSS.highlights.delete('find-match');
      CSS.highlights.delete('find-current');
    } else {
      outline(null);
    }
    ui.bar.style.removeProperty('--find-dodge');
    ui.bar.hidden = true;
    ui.input.blur();
    // Leave the match selected, as browsers do: it marks the place once the
    // highlight is gone, and Cmd+C copies it.
    if (last) {
      const sel = window.getSelection();
      sel?.removeAllRanges();
      sel?.addRange(last);
    }
  }

  function step(dir: 1 | -1): void {
    // Cmd+G with the bar closed brings it back with the last query.
    if (ui.bar.hidden) open();
    const n = found.from.length;
    if (n === 0) return;
    if (current < 0) {
      const first = firstInView();
      current = dir > 0 ? first : (first - 1 + n) % n;
    } else {
      current = (current + dir + n) % n;
    }
    reveal();
    paintMatches(false);
    showCurrent();
  }

  ui.input.addEventListener('input', () => search(true));
  ui.input.addEventListener('keydown', (e) => {
    // Enter that confirms an input-method composition is not a command.
    // WebKit ends the composition before this keydown, so isComposing is
    // already false there; keyCode 229 is how it still marks the key.
    if (e.key !== 'Enter' || e.isComposing || e.keyCode === 229) return;
    e.preventDefault();
    step(e.shiftKey ? -1 : 1);
  });
  ui.prev.addEventListener('click', () => step(-1));
  ui.next.addEventListener('click', () => step(1));
  ui.close.addEventListener('click', close);

  return { open, close, step };
}
