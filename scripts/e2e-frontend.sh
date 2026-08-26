#!/bin/zsh
# End-to-end check of the built frontend bundle, which has no unit-test seam:
# the Go tests can assert the HTML that leaves internal/render, but everything
# after it — the copy-button pass, the lazy KaTeX and Mermaid passes, anchor
# resolution — only happens once the bundle runs in a browser.
#
# Two bugs that shipped past `go test`, `tsc` and a green build motivated this:
#
#   1. A document reaches the DOM two ways — through renderInto (a doc:open into
#      a live window) and inlined into the shell by the Go middleware on a cold
#      launch. The math/diagram pass was wired into the first only, so every
#      *double-clicked* document showed raw TeX and mermaid source.
#   2. enhanceCodeBlocks runs before that pass and skipped the <pre> holding a
#      $$ block, but not the one holding a ```math fence — so KaTeX replaced the
#      <pre> and left a Copy button floating over nothing.
#
# Neither is visible without running the real bundle, so this drives it: the
# built dist/, headless, with the Wails binding surface stubbed, through both
# entry paths and a theme change.
#
# Usage: scripts/e2e-frontend.sh [path/to/document.md]
# Requires: a Chromium-family browser (skips cleanly if none is installed) and
# a built frontend/dist (run `wails build` or `npm run build` in frontend/).
set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DOC="${1:-$ROOT/testdata/gfm.md}"
DIST="$ROOT/frontend/dist"

[[ -f "$DIST/index.html" ]] || { echo "error: $DIST/index.html not found — build the frontend first"; exit 2; }

CHROME="${CHROME:-}"
if [[ -z "$CHROME" ]]; then
  for candidate in \
    "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
    "/Applications/Chromium.app/Contents/MacOS/Chromium" \
    "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge" \
    "$(command -v google-chrome 2>/dev/null)" \
    "$(command -v chromium 2>/dev/null)"; do
    [[ -n "$candidate" && -x "$candidate" ]] && { CHROME="$candidate"; break }
  done
fi
[[ -n "$CHROME" ]] || { echo "SKIP  no Chromium-family browser found (set CHROME=/path/to/browser)"; exit 0 }

WORK="$(mktemp -d)"
PORT="${PORT:-5210}"
SERVER_PID=""
CHROME_PID=""
# Chrome keeps writing to its profile for a moment after SIGTERM, and rm -rf
# races that with "Directory not empty" — so reap both children first.
cleanup() {
  [[ -n "$CHROME_PID" ]] && kill "$CHROME_PID" 2>/dev/null
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null
  wait 2>/dev/null
  rm -rf "$WORK" 2>/dev/null
}
trap cleanup EXIT

cp -R "$DIST/." "$WORK/"

# Render the document through the real Go pipeline, exactly as the app does.
( cd "$ROOT" && go run ./tools/render-doc "$DOC" ) > "$WORK/doc.json" || {
  echo "error: rendering $DOC failed"; exit 2
}

# The Wails binding surface the bundle talks to, plus the assertions. An
# external file, not an inline script: index.html ships script-src 'self'.
{
  printf 'window.__doc = '
  cat "$WORK/doc.json"
  printf ';\n'
  cat "$ROOT/scripts/e2e-frontend.js"
} > "$WORK/stub.js"

python3 - "$WORK" <<'PY'
import json, re, sys, pathlib
work = pathlib.Path(sys.argv[1])
doc = json.load(open(work / 'doc.json'))
index = (work / 'index.html').read_text()
index = index.replace('<body>', '<body>\n<script src="./stub.js"></script>', 1)
# Inline the rendered document the way app.go's serveShell does on a cold launch.
inlined = ('<article id="content" data-doc-path="%s" data-doc-title="%s">'
           % (doc['path'], doc['title'].replace('"', '&quot;'))) + doc['html'] + '</article>'
new = re.sub(r'<article id="content">.*?</article>', lambda _: inlined, index, flags=re.S)
assert new != index, 'could not find the #content container in index.html'
(work / 'index.html').write_text(new)
PY

# A static server that also accepts the harness's result. Chrome's own
# --dump-dom hangs under --virtual-time-budget on current builds, so the page
# posts its verdict back instead: deterministic, and no guessing how long the
# lazy KaTeX/Mermaid chunks take.
cat > "$WORK/serve.py" <<'PYEOF'
import http.server, os, sys, threading

port, root, out = int(sys.argv[1]), sys.argv[2], sys.argv[3]
done = threading.Event()

class Handler(http.server.SimpleHTTPRequestHandler):
    def __init__(self, *a, **kw):
        super().__init__(*a, directory=root, **kw)

    def do_POST(self):
        if self.path != "/e2e-result":
            self.send_error(404)
            return
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        with open(out, "wb") as f:
            f.write(body)
        self.send_response(204)
        self.end_headers()
        done.set()

    def log_message(self, *a):
        pass

server = http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
done.wait(timeout=float(os.environ.get("E2E_TIMEOUT", "90")))
server.shutdown()
PYEOF

RESULT_FILE="$WORK/result.txt"
python3 "$WORK/serve.py" "$PORT" "$WORK" "$RESULT_FILE" &
SERVER_PID=$!
for _ in {1..40}; do
  curl -sf -o /dev/null "http://127.0.0.1:$PORT/index.html" && break
  sleep 0.25
done

PROFILE="$WORK/chrome-profile"
"$CHROME" --headless --disable-gpu --no-first-run --no-default-browser-check \
  --user-data-dir="$PROFILE" "http://127.0.0.1:$PORT/index.html" \
  >/dev/null 2>"$WORK/chrome.log" &
CHROME_PID=$!

wait "$SERVER_PID" 2>/dev/null
SERVER_PID=""
kill "$CHROME_PID" 2>/dev/null
wait "$CHROME_PID" 2>/dev/null
CHROME_PID=""

if [[ ! -s "$RESULT_FILE" ]]; then
  echo "FAIL  the harness never reported — the bundle did not finish booting"
  tail -20 "$WORK/chrome.log" | sed 's/^/      /'
  exit 1
fi

cat "$RESULT_FILE"
grep -q '^FAIL' "$RESULT_FILE" && exit 1
exit 0
