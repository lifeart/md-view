#!/bin/zsh
# End-to-end check of the warm-open presentation path, which has no unit-test
# seam: the OS un-hides the app before it delivers a file open, so the
# frontend's clear-on-hide / restore-on-show logic races the doc:open, and
# the only way to see who won is to run the real app and read its trace.
#
# Drives the built bundle through: cold launch with A; open B into the
# visible window; N rounds of "hide the app (what closing the window does),
# then open the other file"; and hide + plain un-hide (Dock-click style).
# Each round must end with the expected document committed.
#
# Usage: scripts/e2e-warm-open.sh [path/to/md-view.app] [rounds]
# Requires: a macOS desktop session, swiftc (Xcode CLT). No Accessibility or
# Screen Recording permission is needed — hiding goes through
# NSRunningApplication, and the result is read from the MDVIEW_TRACE file.
# Any running instance of that bundle is quit first and again at the end.
set -u

APP="${1:-build/bin/md-view.app}"
ROUNDS="${2:-4}"
APP="$(cd "$(dirname "$APP")" && pwd)/$(basename "$APP")"
[[ -d "$APP" ]] || { echo "error: $APP not found — run wails build first"; exit 2; }

WORK="$(mktemp -d)"
trap 'pkill -f "$APP/Contents/MacOS/" 2>/dev/null; rm -rf "$WORK"' EXIT
TRACE="$WORK/trace.log"
mkdir -p "$WORK/docs"
printf '# FILE AAA\n\nThis is document **A**.\n' > "$WORK/docs/a.md"
printf '# FILE BBB\n\nThis is document **B**.\n' > "$WORK/docs/b.md"

# Hide / un-hide the app the way the window's close button does
# ([NSApp hide:]) — NSRunningApplication needs no extra permissions.
cat > "$WORK/apphide.swift" <<'EOF'
import Cocoa
let mode = CommandLine.arguments[1]
let bundle = CommandLine.arguments[2]
for a in NSWorkspace.shared.runningApplications where a.bundleURL?.path == bundle {
    if mode == "hide" { _ = a.hide() } else { _ = a.unhide() }
}
EOF
swiftc -O -o "$WORK/apphide" "$WORK/apphide.swift" 2>/dev/null || { echo "error: swiftc failed (Xcode command line tools needed)"; exit 2; }

fail=0
mark() { wc -l < "$TRACE" | tr -d ' '; }
# last document committed by the frontend since trace line $1 ("" if none)
last_commit() { tail -n +"$(( $1 + 1 ))" "$TRACE" | awk '/fe: renderInto committed/ {p=$5} END {print p}' | xargs -r basename 2>/dev/null; }
check() { # name expected since-line
  local got; got="$(last_commit "$3")"
  if [[ "$got" == "$2" ]]; then echo "PASS  $1 -> $got"; else echo "FAIL  $1 -> '${got:-nothing}' (expected $2)"; fail=1; tail -n +"$(( $3 + 1 ))" "$TRACE" | sed 's/^/      /'; fi
}

pkill -f "$APP/Contents/MacOS/" 2>/dev/null; sleep 1
open --env MDVIEW_TRACE="$TRACE" -a "$APP" "$WORK/docs/a.md"; sleep 3
if grep -q 'Ready(".*a.md")' "$TRACE"; then echo "PASS  cold launch inlined a.md"; else echo "FAIL  cold launch did not inline a.md"; fail=1; fi

n=$(mark); open -a "$APP" "$WORK/docs/b.md"; sleep 2.5
check "visible window: open b.md" b.md "$n"

cur=b
for i in $(seq 1 "$ROUNDS"); do
  if [[ $cur == b ]]; then nxt=a; else nxt=b; fi
  n=$(mark); "$WORK/apphide" hide "$APP"; sleep 1.5
  open -a "$APP" "$WORK/docs/$nxt.md"; sleep 2.5
  check "round $i: hide, open $nxt.md (was $cur.md)" "$nxt.md" "$n"
  cur=$nxt
done

n=$(mark); "$WORK/apphide" hide "$APP"; sleep 1.5; "$WORK/apphide" unhide "$APP"; sleep 2.5
check "hide, then plain un-hide restores $cur.md" "$cur.md" "$n"

if [[ $fail -eq 0 ]]; then echo "all passed"; else echo "FAILURES (full trace: see above)"; fi
exit $fail
