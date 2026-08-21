/* MDv landing page — progressive enhancement only.
 *
 * Nothing here is load-bearing: with JavaScript off the page still reads, the
 * screenshots still show, and the download button is still a direct link to
 * the latest notarized DMG. This file adds
 *   1. the page light/dark switch,
 *   2. the demo frame's light/dark/sepia switch (a data-theme flip, exactly
 *      what the app does — no re-render),
 *   3. the code block's hover Copy button (a port of enhanceCodeBlocks() in
 *      frontend/src/main.ts),
 *   4. version/size/date next to the download button, from the GitHub API.
 */
(function () {
  'use strict';

  var root = document.documentElement;
  var STORAGE_KEY = 'mdv-page-theme';
  var darkQuery = window.matchMedia('(prefers-color-scheme: dark)');

  // ---------- page theme ----------

  var pageSelect = document.getElementById('page-theme');

  function currentPageTheme() {
    var value = root.getAttribute('data-page-theme');
    return value === 'light' || value === 'dark' ? value : 'system';
  }

  function resolvedPageTheme() {
    var value = currentPageTheme();
    if (value !== 'system') return value;
    return darkQuery.matches ? 'dark' : 'light';
  }

  if (pageSelect) {
    pageSelect.value = currentPageTheme();
    pageSelect.addEventListener('change', function () {
      root.setAttribute('data-page-theme', pageSelect.value);
      try {
        if (pageSelect.value === 'system') {
          localStorage.removeItem(STORAGE_KEY);
        } else {
          localStorage.setItem(STORAGE_KEY, pageSelect.value);
        }
      } catch (err) {
        // Storage blocked: the choice still applies for this visit, it just
        // will not be remembered. Worth a log, not worth interrupting anyone.
        console.warn('MDv: could not persist the theme preference:', err);
      }
      syncFrameTheme();
    });
  }

  // ---------- demo frame theme ----------
  //
  // The frame carries its own data-theme, which is all the app does too:
  // theme.css redefines the custom properties for the scope and chroma.css
  // redefines the token colors for the same scope. No markup is touched.

  var frame = document.getElementById('demo-frame');
  var themeButtons = document.querySelectorAll('[data-set-theme]');
  var framePinned = false;

  function setFrameTheme(theme) {
    if (!frame) return;
    frame.setAttribute('data-theme', theme);
    for (var i = 0; i < themeButtons.length; i++) {
      var btn = themeButtons[i];
      btn.setAttribute('aria-pressed', String(btn.dataset.setTheme === theme));
    }
  }

  function syncFrameTheme() {
    if (!framePinned) setFrameTheme(resolvedPageTheme());
  }

  for (var i = 0; i < themeButtons.length; i++) {
    themeButtons[i].addEventListener('click', function (event) {
      framePinned = true;
      setFrameTheme(event.currentTarget.dataset.setTheme);
    });
  }

  syncFrameTheme();

  if (typeof darkQuery.addEventListener === 'function') {
    darkQuery.addEventListener('change', syncFrameTheme);
  }

  // ---------- code block copy button ----------

  var demoBody = document.getElementById('demo-body');
  if (demoBody) {
    var blocks = demoBody.querySelectorAll('pre');
    for (var b = 0; b < blocks.length; b++) {
      (function (pre) {
        var wrapper = document.createElement('div');
        wrapper.className = 'codeblock';
        pre.replaceWith(wrapper);
        wrapper.appendChild(pre);

        var btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'copy-btn';
        btn.textContent = 'Copy';
        btn.addEventListener('click', function () {
          var text = pre.innerText.replace(/\n$/, '');
          var done = navigator.clipboard
            ? navigator.clipboard.writeText(text)
            : Promise.reject(new Error('clipboard unavailable in this context'));
          done.then(
            function () {
              btn.textContent = 'Copied';
              window.setTimeout(function () {
                btn.textContent = 'Copy';
              }, 1200);
            },
            function (err) {
              // Same contract as the app: never fail silently.
              btn.textContent = 'Copy failed';
              console.warn('MDv: copy failed:', err);
              window.setTimeout(function () {
                btn.textContent = 'Copy';
              }, 2000);
            }
          );
        });
        wrapper.appendChild(btn);
      })(blocks[b]);
    }
  }

  // ---------- release metadata ----------
  //
  // Label only. The button's href is a permanent redirect to the newest
  // release's asset (…/releases/latest/download/MDv.dmg) and is never
  // rewritten from here, so a rate-limited or offline API leaves a working
  // download behind — just without the version line.

  var meta = document.getElementById('release-meta');
  if (meta && 'fetch' in window) {
    fetch('https://api.github.com/repos/lifeart/md-view/releases/latest', {
      headers: { Accept: 'application/vnd.github+json' }
    })
      .then(function (response) {
        if (!response.ok) throw new Error('GitHub API responded ' + response.status);
        return response.json();
      })
      .then(function (release) {
        var assets = Array.isArray(release.assets) ? release.assets : [];
        var dmg = null;
        for (var a = 0; a < assets.length; a++) {
          if (assets[a].name === 'MDv.dmg') { dmg = assets[a]; break; }
          if (!dmg && /\.dmg$/.test(assets[a].name || '')) dmg = assets[a];
        }

        var parts = [];
        if (release.tag_name) parts.push({ strong: true, text: String(release.tag_name) });
        if (dmg && dmg.size) parts.push({ text: (dmg.size / 1e6).toFixed(1) + ' MB' });
        if (release.published_at) {
          var when = new Date(release.published_at);
          if (!isNaN(when.getTime())) {
            parts.push({
              text: when.toLocaleDateString(undefined, {
                year: 'numeric',
                month: 'short',
                day: 'numeric'
              })
            });
          }
        }
        if (!parts.length) throw new Error('release carried no usable metadata');

        // Built as text nodes, never innerHTML: this is remote data.
        meta.textContent = '';
        parts.forEach(function (part, index) {
          if (index) meta.appendChild(document.createTextNode(' · '));
          if (part.strong) {
            var strong = document.createElement('strong');
            strong.textContent = part.text;
            meta.appendChild(strong);
          } else {
            meta.appendChild(document.createTextNode(part.text));
          }
        });
        meta.appendChild(document.createTextNode(' · notarized DMG'));
      })
      .catch(function (err) {
        // The API is rate-limited and may simply be unavailable. The generic
        // label stays on screen and the download link keeps working; log the
        // reason so it is diagnosable rather than mysterious.
        console.warn(
          'MDv: could not read the latest release metadata (' +
            err.message +
            '); the download link is unaffected.'
        );
      });
  }
})();
