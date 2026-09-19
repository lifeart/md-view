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

        // 6. Copying. WebKit's Selection.toString() is built from laid-out text
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
