import './theme.css';
import './chroma.css';

import {
  GetSettings,
  IsWindowHidden,
  OpenExternal,
  OpenFileDialog,
  OpenWithSystemDefault,
  PresentWindow,
  Ready,
  RenderDocument,
  ResolveLink,
  SetSettings,
} from '../wailsjs/go/main/App';
import { ClipboardSetText, EventsOn, WindowSetTitle } from '../wailsjs/runtime/runtime';
import { settings } from '../wailsjs/go/models';

// ---------- helpers ----------

function el<T extends HTMLElement>(id: string): T {
  const e = document.getElementById(id);
  if (!e) {
    throw new Error(`missing element #${id}`);
  }
  return e as T;
}

function errMsg(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

const content = el<HTMLElement>('content');
const docTitle = el<HTMLDivElement>('doc-title');
const btnBack = el<HTMLButtonElement>('btn-back');
const btnForward = el<HTMLButtonElement>('btn-forward');
const btnAppearance = el<HTMLButtonElement>('btn-appearance');
const errorBanner = el<HTMLDivElement>('error-banner');
const errorText = el<HTMLSpanElement>('error-text');
const errorDismiss = el<HTMLButtonElement>('error-dismiss');
const appearanceMenu = el<HTMLDivElement>('appearance-menu');
const themeSelect = el<HTMLSelectElement>('set-theme');
const fontSelect = el<HTMLSelectElement>('set-font');
const sizeMinus = el<HTMLButtonElement>('size-minus');
const sizePlus = el<HTMLButtonElement>('size-plus');
const sizeValue = el<HTMLSpanElement>('size-value');
const widthRange = el<HTMLInputElement>('set-width');
const widthValue = el<HTMLSpanElement>('width-value');
const linkMenu = el<HTMLDivElement>('link-menu');
const linkCopy = el<HTMLButtonElement>('link-copy');

// ---------- error banner ----------

let bannerTimer: number | undefined;

function showBanner(msg: string, notice: boolean): void {
  errorText.textContent = msg;
  errorBanner.classList.toggle('notice', notice);
  errorBanner.hidden = false;
  if (bannerTimer !== undefined) window.clearTimeout(bannerTimer);
  bannerTimer = window.setTimeout(() => {
    errorBanner.hidden = true;
  }, 6000);
}

function showError(msg: string): void {
  showBanner(msg, false);
}

function showNotice(msg: string): void {
  showBanner(msg, true);
}

errorDismiss.addEventListener('click', () => {
  errorBanner.hidden = true;
});

// ---------- settings ----------

let current: settings.Settings = settings.Settings.createFrom({
  theme: 'light',
  fontFamily: '',
  fontSize: 16,
  contentWidth: 72,
});

const darkQuery = window.matchMedia('(prefers-color-scheme: dark)');

function applySettings(): void {
  const root = document.documentElement;
  const resolved =
    current.theme === 'system' ? (darkQuery.matches ? 'dark' : 'light') : current.theme;
  root.dataset.theme = resolved;
  root.style.setProperty('--font-size', `${current.fontSize}px`);
  root.style.setProperty('--content-width', `${current.contentWidth}ch`);
  if (current.fontFamily) {
    root.style.setProperty('--font-family', current.fontFamily);
  } else {
    root.style.removeProperty('--font-family');
  }
  syncAppearanceControls();
}

function syncAppearanceControls(): void {
  themeSelect.value = current.theme;
  if (!Array.from(fontSelect.options).some((o) => o.value === current.fontFamily)) {
    const custom = document.createElement('option');
    custom.value = current.fontFamily;
    custom.textContent = 'Custom';
    fontSelect.appendChild(custom);
  }
  fontSelect.value = current.fontFamily;
  sizeValue.textContent = String(current.fontSize);
  widthRange.value = String(current.contentWidth);
  widthValue.textContent = String(current.contentWidth);
}

function persistSettings(): void {
  SetSettings(current).catch((err) => {
    showError(`Failed to save settings: ${errMsg(err)}`);
  });
}

function changeFontSize(delta: number): void {
  const next = Math.min(40, Math.max(9, current.fontSize + delta));
  if (next === current.fontSize) return;
  current.fontSize = next;
  applySettings();
  persistSettings();
}

darkQuery.addEventListener('change', () => {
  if (current.theme === 'system') applySettings();
});

themeSelect.addEventListener('change', () => {
  current.theme = themeSelect.value;
  applySettings();
  persistSettings();
});

fontSelect.addEventListener('change', () => {
  current.fontFamily = fontSelect.value;
  applySettings();
  persistSettings();
});

sizeMinus.addEventListener('click', () => changeFontSize(-1));
sizePlus.addEventListener('click', () => changeFontSize(+1));

widthRange.addEventListener('input', () => {
  current.contentWidth = Number(widthRange.value);
  applySettings();
});
widthRange.addEventListener('change', persistSettings);

btnAppearance.addEventListener('click', (e) => {
  e.stopPropagation();
  appearanceMenu.hidden = !appearanceMenu.hidden;
});

// ---------- history ----------

interface HistoryEntry {
  path: string;
  scrollY: number;
  anchor: string;
}

const history: HistoryEntry[] = [];
let historyIndex = -1;
let currentPath = '';

function updateNavButtons(): void {
  btnBack.disabled = historyIndex <= 0;
  btnForward.disabled = historyIndex < 0 || historyIndex >= history.length - 1;
}

function captureScroll(): void {
  if (historyIndex >= 0 && historyIndex < history.length) {
    history[historyIndex].scrollY = window.scrollY;
  }
}

function scrollToAnchor(anchor: string, smooth: boolean): void {
  const target = document.getElementById(anchor);
  if (!target) {
    showNotice(`No such section: #${anchor}`);
    return;
  }
  target.scrollIntoView({ behavior: smooth ? 'smooth' : 'auto', block: 'start' });
}

// Monotonic navigation token: two in-flight renders (double-click on a link,
// fast back/forward) can resolve out of order, so only the navigation holding
// the latest token may commit DOM/history/title mutations.
let navSeq = 0;

// Content was cleared while the window was hidden (see the visibilitychange
// handler) and must be restored on the next show.
let contentCleared = false;

// Same-document view transition: the engine snapshots the old state, applies
// the DOM update, and crossfades (50 ms, see theme.css) — an atomic,
// compositor-synced swap with no intermediate frame to flicker, replacing any
// manual opacity choreography. Skipped (plain swap) when unsupported or when
// the document is hidden — a hidden window cannot animate, and its
// presentation is handled by the native alpha-gated reveal instead.
function commitWithTransition(commit: () => void): Promise<void> {
  const doc = document as Document & {
    startViewTransition?: (cb: () => void) => { updateCallbackDone: Promise<void> };
  };
  if (doc.startViewTransition && document.visibilityState === 'visible') {
    return doc.startViewTransition(commit).updateCallbackDone;
  }
  commit();
  return Promise.resolve();
}

async function renderInto(path: string, token: number): Promise<boolean> {
  try {
    const doc = await RenderDocument(path);
    if (token !== navSeq) {
      // A newer navigation started while this render was in flight. Dropping
      // the stale result silently is intentional (not error swallowing): the
      // render succeeded but lost the race, and committing it would clobber
      // the newer document.
      return false;
    }
    await commitWithTransition(() => {
      content.innerHTML = doc.html;
      currentPath = doc.path;
      docTitle.textContent = doc.title;
      docTitle.title = doc.path;
      WindowSetTitle(`${doc.title} — md-view`);
      enhanceCodeBlocks();
    });
    return true;
  } catch (err) {
    if (token === navSeq) {
      showError(`Failed to open ${path}: ${errMsg(err)}`);
    }
    // Stale failures are dropped: the error belongs to an abandoned
    // navigation and a newer one has already taken over the UI.
    return false;
  }
}

async function navigateTo(path: string, anchor = ''): Promise<void> {
  const token = ++navSeq;
  captureScroll();
  const ok = await renderInto(path, token);
  if (!ok) return;
  // drop any forward entries, push the new location
  history.splice(historyIndex + 1);
  history.push({ path, scrollY: 0, anchor });
  historyIndex = history.length - 1;
  updateNavButtons();
  if (anchor) {
    scrollToAnchor(anchor, false);
  } else {
    window.scrollTo(0, 0);
  }
}

async function goToHistoryEntry(index: number): Promise<void> {
  if (index < 0 || index >= history.length) return;
  const token = ++navSeq;
  captureScroll();
  const entry = history[index];
  const ok = await renderInto(entry.path, token);
  if (!ok) return;
  historyIndex = index;
  updateNavButtons();
  // Restore where the user left this entry. A recorded scroll position wins;
  // with none (scrollY 0) fall back to the entry's anchor, which keeps the
  // right section in view even if the document's layout changed meanwhile.
  const anchorTarget =
    entry.scrollY === 0 && entry.anchor ? document.getElementById(entry.anchor) : null;
  if (anchorTarget) {
    anchorTarget.scrollIntoView({ behavior: 'auto', block: 'start' });
  } else {
    window.scrollTo(0, entry.scrollY);
  }
}

function goBack(): void {
  if (historyIndex > 0) void goToHistoryEntry(historyIndex - 1);
}

function goForward(): void {
  if (historyIndex < history.length - 1) void goToHistoryEntry(historyIndex + 1);
}

btnBack.addEventListener('click', goBack);
btnForward.addEventListener('click', goForward);

// ---------- link handling ----------

async function followLink(href: string): Promise<void> {
  if (href.startsWith('#')) {
    const anchor = href.slice(1);
    scrollToAnchor(anchor, true);
    if (historyIndex >= 0) history[historyIndex].anchor = anchor;
    return;
  }
  if (!currentPath) {
    showError('No document open');
    return;
  }
  try {
    const res = await ResolveLink(currentPath, href);
    switch (res.kind) {
      case 'anchor':
        scrollToAnchor(res.fragment, true);
        break;
      case 'markdown':
        await navigateTo(res.path, res.fragment);
        break;
      case 'external':
        await OpenExternal(res.url);
        break;
      case 'file':
        await OpenWithSystemDefault(res.path);
        break;
      default:
        showNotice(`Link not supported: ${href}`);
    }
  } catch (err) {
    showError(errMsg(err));
  }
}

document.addEventListener('click', (e) => {
  // close popovers when clicking outside them
  if (!appearanceMenu.hidden && !appearanceMenu.contains(e.target as Node)) {
    appearanceMenu.hidden = true;
  }
  if (!linkMenu.hidden && !linkMenu.contains(e.target as Node)) {
    linkMenu.hidden = true;
  }
  const a = (e.target as Element).closest?.('a');
  if (!a || !content.contains(a)) return;
  const href = a.getAttribute('href');
  if (!href) return;
  e.preventDefault();
  void followLink(href);
});

// ---------- link context menu (Copy Link Address) ----------

let contextHref = '';

document.addEventListener('contextmenu', (e) => {
  const a = (e.target as Element).closest?.('a');
  if (!a || !content.contains(a)) {
    linkMenu.hidden = true;
    return;
  }
  const href = a.getAttribute('href');
  if (!href) return;
  e.preventDefault();
  contextHref = href;
  linkMenu.style.left = `${Math.min(e.clientX, window.innerWidth - 180)}px`;
  linkMenu.style.top = `${Math.min(e.clientY, window.innerHeight - 44)}px`;
  linkMenu.hidden = false;
});

linkCopy.addEventListener('click', () => {
  linkMenu.hidden = true;
  void copyLinkAddress(contextHref);
});

async function copyLinkAddress(href: string): Promise<void> {
  try {
    let address = href;
    if (href.startsWith('#')) {
      address = `${currentPath}${href}`;
    } else if (currentPath) {
      const res = await ResolveLink(currentPath, href);
      if (res.kind === 'external' || res.kind === 'unsupported') {
        address = res.url || href;
      } else if (res.kind === 'markdown') {
        address = res.fragment ? `${res.path}#${res.fragment}` : res.path;
      } else if (res.kind === 'file') {
        address = res.path;
      }
    }
    const ok = await ClipboardSetText(address);
    if (!ok) {
      showError('Could not copy link address to clipboard');
      return;
    }
    showNotice('Link address copied');
  } catch (err) {
    showError(`Copy failed: ${errMsg(err)}`);
  }
}

// ---------- copy as plain text ----------

document.addEventListener('copy', (e) => {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || !e.clipboardData) return;
  e.clipboardData.setData('text/plain', sel.toString());
  e.preventDefault();
});

// ---------- code block copy buttons ----------

function enhanceCodeBlocks(): void {
  for (const pre of Array.from(content.querySelectorAll('pre'))) {
    if (pre.parentElement?.classList.contains('codeblock')) continue;
    const wrapper = document.createElement('div');
    wrapper.className = 'codeblock';
    pre.replaceWith(wrapper);
    wrapper.appendChild(pre);
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'copy-btn';
    btn.textContent = 'Copy';
    btn.addEventListener('click', () => {
      ClipboardSetText(pre.innerText.replace(/\n$/, ''))
        .then((ok) => {
          if (!ok) {
            showError('Could not copy code to clipboard');
            return;
          }
          btn.textContent = 'Copied';
          window.setTimeout(() => {
            btn.textContent = 'Copy';
          }, 1200);
        })
        .catch((err) => {
          showError(`Copy failed: ${errMsg(err)}`);
        });
    });
    wrapper.appendChild(btn);
  }
}

// ---------- keyboard shortcuts ----------

document.addEventListener('keydown', (e) => {
  const mod = e.metaKey || e.ctrlKey;
  if (e.key === 'Escape') {
    appearanceMenu.hidden = true;
    linkMenu.hidden = true;
    return;
  }
  if (mod && e.key === '[') {
    e.preventDefault();
    goBack();
  } else if (mod && e.key === ']') {
    e.preventDefault();
    goForward();
  } else if (e.altKey && !mod && e.key === 'ArrowLeft') {
    e.preventDefault();
    goBack();
  } else if (e.altKey && !mod && e.key === 'ArrowRight') {
    e.preventDefault();
    goForward();
  } else if (mod && e.key.toLowerCase() === 'o') {
    e.preventDefault();
    void openViaDialog();
  } else if (mod && (e.key === '=' || e.key === '+')) {
    e.preventDefault();
    changeFontSize(+1);
  } else if (mod && e.key === '-') {
    e.preventDefault();
    changeFontSize(-1);
  }
});

// mouse back/forward buttons
document.addEventListener('mouseup', (e) => {
  if (e.button === 3) {
    e.preventDefault();
    goBack();
  } else if (e.button === 4) {
    e.preventDefault();
    goForward();
  }
});

async function openViaDialog(): Promise<void> {
  try {
    const path = await OpenFileDialog();
    if (!path) return; // cancelled
    await navigateTo(path);
  } catch (err) {
    showError(`Open failed: ${errMsg(err)}`);
  }
}

// ---------- startup ----------

async function init(): Promise<void> {
  // Subscribe before Ready() so no buffered open events are lost.
  EventsOn('doc:open', (path: string) => {
    void (async () => {
      // Commit the new document (or the error banner) first, then present:
      // PresentWindow gates the show natively so a hidden window's suspended
      // compositor can never flash the previously displayed content, and its
      // reveal is a 50 ms native alpha fade. Visible-window swaps crossfade
      // via the view transition inside renderInto.
      await navigateTo(path);
      contentCleared = false;
      void PresentWindow();
    })();
  });

  // Clear the document while the window is genuinely off screen (closed to
  // hidden, Cmd+H, minimized): the suspended compositor then holds only the
  // empty shell, so nothing stale can ever be presented, and a large DOM is
  // released. Mere occlusion by another window also reports 'hidden' — the
  // native IsWindowHidden check keeps the content in that case.
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      void (async () => {
        const seqAtHide = navSeq;
        let hidden = false;
        try {
          hidden = await IsWindowHidden();
        } catch (err) {
          showError(`Cannot check window state: ${errMsg(err)}`);
          return;
        }
        // A doc:open may have rendered new content while we asked — never
        // wipe it (it is about to be presented).
        if (hidden && navSeq === seqAtHide && currentPath) {
          captureScroll();
          content.innerHTML = '';
          contentCleared = true;
        }
      })();
    } else if (contentCleared && currentPath) {
      // Shown again without a new document (Dock unhide, deminiaturize):
      // restore the current one with the same fade.
      contentCleared = false;
      void (async () => {
        const token = ++navSeq;
        const ok = await renderInto(currentPath, token);
        if (!ok) return;
        const entry = history[historyIndex];
        if (entry && entry.path === currentPath) {
          window.scrollTo(0, entry.scrollY);
        }
      })();
    }
  });
  EventsOn('app:error', (msg: string) => {
    showError(msg);
  });
  EventsOn('app:notice', (msg: string) => {
    showNotice(msg);
  });

  // Fast path: the Go middleware may have served the shell with the document
  // already rendered into #content (see ARCHITECTURE.md). Hydrate state from
  // it — no RenderDocument call, the HTML is already on screen — and report
  // the inlined path to Ready() so Go does not deliver that open again.
  const inlinedPath = content.dataset.docPath ?? '';
  if (inlinedPath) {
    currentPath = inlinedPath;
    const inlinedTitle = content.dataset.docTitle ?? '';
    docTitle.textContent = inlinedTitle;
    docTitle.title = inlinedPath;
    WindowSetTitle(`${inlinedTitle} — md-view`);
    history.push({ path: inlinedPath, scrollY: 0, anchor: '' });
    historyIndex = 0;
    updateNavButtons();
    enhanceCodeBlocks();
  }

  try {
    current = await GetSettings();
  } catch (err) {
    showError(`Failed to load settings: ${errMsg(err)}`);
  }
  applySettings();

  try {
    await Ready(inlinedPath);
  } catch (err) {
    showError(`Startup handshake failed: ${errMsg(err)}`);
  }
}

void init();
