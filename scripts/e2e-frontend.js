/* Assertions for scripts/e2e-frontend.sh. Loaded before the bundle's module
 * script (which is deferred), so the stubs are in place when it boots.
 *
 * window.__doc is prepended by the harness: the real Doc that
 * internal/render produced for the document under test.
 */
(function () {
  'use strict';

  var doc = window.__doc;
  var traced = [];
  var consoleErrors = [];

  window.addEventListener('error', function (e) {
    consoleErrors.push(String(e.message));
  });
  window.addEventListener('unhandledrejection', function (e) {
    consoleErrors.push('unhandled rejection: ' + String(e.reason));
  });

  window.go = { main: { App: {
    GetSettings: async function () {
      // prewarmAsked: true keeps the one-time offer out of the way of the
      // assertions below; it is exercised on its own further down.
      return {
        theme: 'light', fontFamily: '', fontSize: 16, contentWidth: 72,
        prewarmAsked: true,
      };
    },
    SetSettings: async function () {},
    IsWindowHidden: async function () { return false; },
    PresentWindow: async function () { window.__presented = true; },
    Ready: async function () { window.__ready = true; },
    Trace: async function (m) { traced.push(m); },
    OpenExternal: async function () {},
    OpenFileDialog: async function () { return ''; },
    OpenWithSystemDefault: async function () {},
    ResolveLink: async function () { return { kind: 'anchor', fragment: '' }; },
    GetPrewarmState: async function () {
      return { supported: true, enabled: false, needsApproval: false };
    },
    SetPrewarm: async function (on) {
      return { supported: true, enabled: on, needsApproval: false };
    },
    MarkPrewarmAsked: async function () {},
    RenderDocument: async function (p) {
      if (p !== doc.path) throw new Error('unexpected path');
      return doc;
    },
  } } };

  var events = {};
  window.runtime = {
    EventsOnMultiple: function (n, cb) { events[n] = cb; return function () {}; },
    EventsOn: function (n, cb) { events[n] = cb; return function () {}; },
    EventsOff: function () {}, EventsEmit: function () {},
    WindowSetTitle: function (t) { window.__title = t; },
    ClipboardSetText: async function (t) { window.__clipboard = t; return true; },
    LogPrint: function () {},
  };

  var checks = [];
  function check(name, ok, detail) {
    checks.push((ok ? 'PASS  ' : 'FAIL  ') + name + (ok || detail === undefined ? '' : ' -> ' + detail));
  }
  var wait = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };

  // Everything the enhancement passes are supposed to leave behind.
  function survey(content) {
    var wrappers = Array.prototype.slice.call(content.querySelectorAll('.codeblock'));
    return {
      katex: document.querySelectorAll('.katex').length,
      mathLeft: content.querySelectorAll('code.language-math').length,
      diagrams: content.querySelectorAll('.mermaid-diagram svg').length,
      mermaidLeft: content.querySelectorAll('code.language-mermaid').length,
      copyButtons: content.querySelectorAll('.codeblock .copy-btn').length,
      orphanWrappers: wrappers.filter(function (w) { return !w.querySelector('pre'); }).length,
    };
  }

  // What the document should produce, derived from the rendered HTML itself so
  // the fixture can grow without the script needing to know its contents.
  function expected() {
    var probe = document.createElement('div');
    probe.innerHTML = doc.html;
    var fences = Array.prototype.slice.call(probe.querySelectorAll('pre'));
    var mathSource = probe.querySelector('pre > code.language-math');
    var diagramSource = probe.querySelector('code.language-mermaid');
    var numbered = probe.querySelector('ol[start]');
    return {
      math: probe.querySelectorAll('code.language-math').length,
      diagrams: probe.querySelectorAll('code.language-mermaid').length,
      // The sources the lazy renderers consume. Once they have run the DOM
      // holds KaTeX glyph spans and an SVG, neither of which copies as text —
      // so the copy pass has to reach back to these.
      mathSource: mathSource ? mathSource.textContent.trim() : '',
      diagramSource: diagramSource ? diagramSource.textContent.trim() : '',
      listStart: numbered ? numbered.getAttribute('start') : '',
      // Only plain code fences get a copy button; math and diagram sources are
      // replaced by typeset output, so wrapping them would strand the button.
      codeFences: fences.filter(function (pre) {
        return !pre.querySelector('code.language-math, code.language-mermaid');
      }).length,
    };
  }

  function verify(label, want, content) {
    var got = survey(content);
    check(label + ': math typeset', got.katex === want.math, got.katex + '/' + want.math);
    check(label + ': no math left as source', got.mathLeft === 0, String(got.mathLeft));
    check(label + ': diagrams drawn', got.diagrams === want.diagrams, got.diagrams + '/' + want.diagrams);
    check(label + ': no diagram left as source', got.mermaidLeft === 0, String(got.mermaidLeft));
    check(label + ': copy button per code fence',
      got.copyButtons === want.codeFences, got.copyButtons + '/' + want.codeFences);
    check(label + ': no stranded copy button', got.orphanWrappers === 0, String(got.orphanWrappers));
  }

  // copyOf selects a node's contents and runs the real `copy` handler over it,
  // returning what would have landed on the pasteboard.
  function copyOf(node) {
    var sel = window.getSelection();
    var range = document.createRange();
    range.selectNodeContents(node);
    sel.removeAllRanges();
    sel.addRange(range);
    var data = new DataTransfer();
    var event = new ClipboardEvent('copy', {
      clipboardData: data, bubbles: true, cancelable: true,
    });
    if (event.clipboardData !== data) throw new Error('ClipboardEvent ignored clipboardData');
    document.dispatchEvent(event);
    return data.getData('text/plain');
  }

  // Post the verdict back to the harness server, which is waiting on it. The
  // shell CSP allows connect-src 'self', which this is.
  function report() {
    var body = checks.join('\n') + '\n';
    try {
      navigator.sendBeacon('/e2e-result', new Blob([body], { type: 'text/plain' }));
    } catch (e) {
      fetch('/e2e-result', { method: 'POST', body: body });
    }
  }

  window.addEventListener('DOMContentLoaded', function () {
    (async function () {
      try {
        var content = document.getElementById('content');
        var want = expected();
        check('document has something to enhance', want.math > 0 && want.diagrams > 0,
          'math=' + want.math + ' diagrams=' + want.diagrams);

        // 1. Cold launch: the shell arrives with the document already inlined,
        //    so init() — not renderInto — has to start the enhancement passes.
        await wait(3000);
        check('cold launch: handshake completed', window.__ready === true);
        check('cold launch: window title set', /— MDv$/.test(window.__title || ''), window.__title);
        verify('cold launch', want, content);

        // 2. Every in-document link must resolve the way followLink does:
        //    directly, percent-decoded, or as a legacy <a name> target.
        var broken = Array.prototype.slice.call(content.querySelectorAll('a[href^="#"]'))
          .map(function (a) { return a.getAttribute('href').slice(1); })
          .filter(function (h) {
            var d = h;
            try { d = decodeURIComponent(h); } catch (e) { /* keep the raw form */ }
            return !document.getElementById(h) && !document.getElementById(d) &&
                   !content.querySelector('a[name="' + CSS.escape(d) + '"]');
          });
        check('every in-document anchor resolves', broken.length === 0, broken.join(', '));

        // 3. Warm open: the same document delivered as a doc:open event, which
        //    goes through renderInto and replaces the DOM wholesale.
        check('doc:open subscribed', typeof events['doc:open'] === 'function');
        if (events['doc:open']) {
          events['doc:open'](doc.path);
          await wait(3000);
          check('warm open: window presented', window.__presented === true);
          verify('warm open', want, content);
        }

        // 4. A theme change cannot re-flow a diagram through CSS — mermaid bakes
        //    its palette into the SVG — so it has to be drawn again, once.
        var before = content.querySelector('.mermaid-diagram svg');
        var beforeId = before && before.id;
        var themeSelect = document.getElementById('set-theme');
        themeSelect.value = 'dark';
        themeSelect.dispatchEvent(new Event('change'));
        await wait(3000);
        var after = content.querySelector('.mermaid-diagram svg');
        check('theme change: still exactly one svg per diagram',
          content.querySelectorAll('.mermaid-diagram svg').length === want.diagrams);
        check('theme change: diagram redrawn', !!after && after.id !== beforeId,
          beforeId + ' -> ' + (after && after.id));
        check('theme change: math survives',
          document.querySelectorAll('.katex').length === want.math);

        // 5. The copy button is the one piece of the reading UI with a backend
        //    call behind it.
        var btn = content.querySelector('.codeblock .copy-btn');
        if (btn) {
          btn.click();
          await wait(300);
          check('copy button copies the fence', typeof window.__clipboard === 'string' &&
            window.__clipboard.length > 0, JSON.stringify(window.__clipboard));
        }

        // 6. Find in page. The webview has no find bar of its own, so all of
        //    it is the bundle's: the shortcut, the search across inline
        //    markup, the highlights, and keeping them true while the document
        //    changes underneath (math and diagrams land after first paint, and
        //    a navigation replaces everything).
        var findBar = document.getElementById('find-bar');
        var findInput = document.getElementById('find-input');
        var findCount = document.getElementById('find-count');
        var key = function (target, init) {
          init.bubbles = true;
          init.cancelable = true;
          var ev = new KeyboardEvent('keydown', init);
          target.dispatchEvent(ev);
          return ev;
        };
        var type = function (query) {
          findInput.value = query;
          findInput.dispatchEvent(new Event('input', { bubbles: true }));
        };
        var marked = function (name) {
          var highlight = CSS.highlights.get(name);
          return highlight ? Array.from(highlight) : [];
        };
        var textIn = function (range) {
          var live = document.createRange();
          live.setStart(range.startContainer, range.startOffset);
          live.setEnd(range.endContainer, range.endOffset);
          return live.toString();
        };
        var same = function (a, b) {
          return a.replace(/\s+/g, ' ').toLowerCase() === b.replace(/\s+/g, ' ').toLowerCase();
        };
        // Ranges are made afresh each time one is painted, so compare where
        // they point rather than which object they are.
        var sameSpot = function (a, b) {
          return !!a && !!b && a.startContainer === b.startContainer && a.startOffset === b.startOffset;
        };
        var position = function () {
          var m = /^(\d+) of (\d+)$/.exec(findCount.textContent);
          return m ? Number(m[1]) : 0;
        };

        var shortcut = key(document, { key: 'f', metaKey: true });
        check('find: Cmd+F opens the bar', !findBar.hidden && shortcut.defaultPrevented);
        check('find: the field takes the keyboard', document.activeElement === findInput);

        var heading = content.querySelector('h2');
        var title = heading ? heading.textContent.trim() : '';
        if (title) {
          type(title.toUpperCase());
          var found = marked('find-match');
          var active = marked('find-current');
          check('find: a heading is found whatever its case',
            found.length >= 1 && active.length === 1 && same(textIn(active[0]), title),
            found.length + ' matches for ' + JSON.stringify(title));
          check('find: the count places the current match',
            position() >= 1 && findCount.textContent === position() + ' of ' + found.length,
            findCount.textContent);
        }

        // A phrase that is one run of words on screen but two nodes in the
        // DOM, inside a <details> the reader has not opened.
        var closed = content.querySelector('details:not([open])');
        var inline = closed && closed.querySelector('strong, em, code');
        var lead = inline && inline.previousSibling;
        if (lead && lead.nodeType === Node.TEXT_NODE && /\S\s+$/.test(lead.data)) {
          var phrase = lead.data.trim().split(/\s+/).pop() + ' ' + inline.textContent;
          type(phrase);
          var across = marked('find-current');
          check('find: a phrase matches across inline markup',
            across.length === 1 && across[0].startContainer !== across[0].endContainer &&
              same(textIn(across[0]), phrase),
            JSON.stringify(phrase) + ' -> ' + findCount.textContent);
          check('find: a match in a collapsed <details> opens it', closed.open === true);
        }

        // The Copy button and a diagram's stylesheet are inside #content and
        // are not the document.
        var chromeSkipped = ['Copy', '#mermaid'].every(function (query) {
          type(query);
          return marked('find-match').every(function (range) {
            return !range.startContainer.parentElement.closest('button, style');
          });
        });
        check('find: chrome inside the document is not searched', chromeSkipped);

        type('⁣ no such text');
        check('find: a miss says so',
          findCount.textContent === 'No results' && marked('find-match').length === 0 &&
            document.getElementById('find-next').disabled, findCount.textContent);

        type('e');
        var total = marked('find-match').length;
        check('find: a common letter has many matches', total >= 2, String(total));
        if (total >= 2) {
          var startAt = position();
          var firstHit = marked('find-current')[0];
          key(findInput, { key: 'Enter' });
          var secondHit = marked('find-current')[0];
          check('find: Enter moves to the next match',
            !sameSpot(secondHit, firstHit) && position() === (startAt % total) + 1, findCount.textContent);
          key(findInput, { key: 'Enter', shiftKey: true });
          check('find: Shift+Enter moves back',
            sameSpot(marked('find-current')[0], firstHit) && position() === startAt, findCount.textContent);
          key(document, { key: 'g', metaKey: true });
          check('find: Cmd+G moves to the next match', sameSpot(marked('find-current')[0], secondHit));

          // The same document again, as a doc:open: every node the ranges
          // pointed into is replaced, and then replaced again in part when the
          // math and the diagram land.
          if (events['doc:open']) {
            events['doc:open'](doc.path);
            await wait(3000);
            var fresh = marked('find-match');
            check('find: the highlights follow a re-rendered document',
              fresh.length === total && fresh.every(function (range) {
                return range.startContainer.isConnected && range.endContainer.isConnected;
              }), fresh.length + '/' + total);
            check('find: no match is current until the reader picks one again',
              marked('find-current').length === 0 && findCount.textContent === total + ' matches',
              findCount.textContent);
            key(findInput, { key: 'Enter' });
            check('find: the next step starts from what is on screen', position() >= 1,
              findCount.textContent);
          }
        }

        var lastHit = marked('find-current')[0];
        var lastText = lastHit ? textIn(lastHit) : '';
        key(findInput, { key: 'Escape' });
        check('find: Escape closes the bar and clears the highlights',
          findBar.hidden && !CSS.highlights.has('find-match') && !CSS.highlights.has('find-current'));
        check('find: closing leaves the current match selected',
          lastText !== '' && window.getSelection().toString() === lastText,
          JSON.stringify(window.getSelection().toString()));

        // 7. Copying. WebKit's Selection.toString() is built from laid-out text
        //    runs, so it drops everything a list is made of — the bullet, the
        //    number, the indent, and the blank line that separates the list
        //    from the next block. Copied lists arrived as a run of bare lines
        //    that had to be re-formatted by hand; copy.ts walks the DOM
        //    instead. These run last because they leave a selection behind.
        var checkbox = content.querySelector('li input[type="checkbox"]');
        var taskList = checkbox && checkbox.closest('ul');
        if (taskList) {
          var items = copyOf(taskList);
          check('copy: task list keeps its markers', /^- \[[ x]\] \S/m.test(items),
            JSON.stringify(items));
          check('copy: nested item stays indented', /\n {2,}- /.test(items),
            JSON.stringify(items));
        }

        var all = copyOf(content);
        check('copy: a list is followed by a blank line', /\n- [^\n]+\n\n/.test(all),
          JSON.stringify(all.slice(0, 400)));
        if (want.listStart) {
          check('copy: an ol keeps the document numbering',
            all.indexOf('\n' + want.listStart + '. ') >= 0, want.listStart);
        }
        check('copy: typeset math copies as its source',
          !want.mathSource || all.indexOf(want.mathSource) >= 0, want.mathSource);
        check('copy: a drawn diagram copies as its source',
          !want.diagramSource || all.indexOf(want.diagramSource) >= 0, want.diagramSource);
        check('copy: the fence copy button is not copied too', !/^Copy$/m.test(all));

        check('no uncaught errors', consoleErrors.length === 0, consoleErrors.join(' | '));
      } catch (err) {
        check('harness ran to completion', false, String(err && err.stack || err));
      }
      report();
    })();
  });
})();
