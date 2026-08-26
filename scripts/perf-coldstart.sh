#!/bin/zsh
# Cold-start breakdown for a built MDv bundle, measured the way a user
# experiences it: wall clock from `open` (a LaunchServices launch, the same
# path a Finder double-click takes) to each milestone the app traces.
#
# Every performance claim in ARCHITECTURE.md should come from this script, so
# that "faster" always means the same measurement. Two things it does that a
# naive timing does not:
#
#   * It starts the clock BEFORE `open`, so the ~90-110 ms of LaunchServices +
#     exec + dyld + Go runtime init that happens before the app's own first
#     trace line is included. Timing from the trace's first line alone flatters
#     the result by about a third.
#   * It reports the median of N runs. Cold launches vary by tens of ms with
#     page cache and system load; a single run is not evidence.
#
# Usage: scripts/perf-coldstart.sh [app-bundle] [document.md] [runs]
#   e.g. scripts/perf-coldstart.sh /Applications/MDv.app testdata/gfm.md 7
#
# Requires a macOS desktop session. Any running instance of the bundle is quit
# before each run and at the end.
set -u

APP="${1:-build/bin/md-view.app}"
DOC="${2:-testdata/index.md}"
RUNS="${3:-5}"

[[ -d "$APP" ]] || { echo "error: $APP not found — run wails build first"; exit 2 }
APP="$(cd "$(dirname "$APP")" && pwd)/$(basename "$APP")"
[[ -f "$DOC" ]] || { echo "error: $DOC not found"; exit 2 }
DOC="$(cd "$(dirname "$DOC")" && pwd)/$(basename "$DOC")"

LOG="$(mktemp)"
trap 'pkill -f "$APP/Contents/MacOS/" 2>/dev/null; rm -f "$LOG"' EXIT

echo "app: $APP"
echo "doc: $DOC ($(wc -c < "$DOC" | tr -d " ") bytes), $RUNS runs"

for _ in $(seq 1 "$RUNS"); do
  W="$(mktemp -d)"
  pkill -f "$APP/Contents/MacOS/" 2>/dev/null
  sleep 1.5
  # The clock starts here, before LaunchServices is even asked.
  START="$(python3 -c 'import time; print(time.time()*1000)')"
  open --env MDVIEW_TRACE="$W/trace.log" -a "$APP" "$DOC"
  sleep 3.5
  { echo "START $START"; cat "$W/trace.log" 2>/dev/null; echo "===" } >> "$LOG"
  rm -rf "$W"
done

python3 - "$LOG" <<'PY'
import statistics, sys

runs, cur, start = [], [], None
for line in open(sys.argv[1]):
    if line.startswith("==="):
        if cur:
            runs.append((start, cur))
        cur, start = [], None
        continue
    if line.startswith("START "):
        start = float(line.split()[1])
        continue
    t, _, msg = line.partition(" ")
    try:
        cur.append((float(t), msg.strip()))
    except ValueError:
        pass
if cur:
    runs.append((start, cur))

runs = [r for r in runs if r[0] and r[1]]
if not runs:
    sys.exit("no usable runs — did the app launch? (a GUI session is required)")

# Milestones in the order the launch path produces them. Each is matched by
# prefix because most carry a path or a byte count.
STAGES = [
    ("process start",    "process start",   "LaunchServices + exec + dyld + Go init"),
    ("main entry",       "main entry",      "Go runtime handed off to main()"),
    ("OnStartup",        "OnStartup",       "wails.Run: app + WKWebView setup"),
    ("window shown",     "window shown",    ""),
    ("OnFileOpen",       "OnFileOpen",      "LaunchServices delivered the document"),
    ("shell requested",  "shell requested", "WKWebView asked for the shell"),
    ("shell served",     "shell served",    "document HTML handed to the webview"),

    ("frontend ready",   "fe: Ready",       "bundle booted"),
]

def at(events, prefix):
    return next((t for t, m in events if m.startswith(prefix)), None)


# First paint is reported by the frontend as an absolute epoch timestamp, not
# as the moment its trace line was written: the PerformanceObserver callback
# runs ~30 ms after the paint it describes.
def paint_at(events):
    for _, m in events:
        if m.startswith("fe: paint first-contentful-paint at epoch "):
            try:
                return float(m.rsplit(" ", 1)[1])
            except ValueError:
                return None
    return None

print(f"\n{'milestone':18s} {'from open':>11s} {'delta':>9s}   {'note'}")
print("-" * 78)
prev_med = 0.0
STAGES = STAGES + [("FIRST PAINT", None, "the reader sees the document")]
for label, prefix, note in STAGES:
    vals = []
    for start, events in runs:
        t = paint_at(events) if prefix is None else at(events, prefix)
        if t is not None:
            vals.append(t - start)
    if not vals:
        continue
    med = statistics.median(vals)
    print(f"{label:18s} {med:9.1f}ms {med - prev_med:8.1f}ms   {note}")
    prev_med = med

# The headline is first paint, not "shell served" — the latter is only when Go
# handed the bytes over, ~15 ms before anything is on screen.
totals, fallback = [], False
for start, events in runs:
    t = paint_at(events)
    if t is None:
        t, fallback = at(events, "shell served"), True
    if t is not None:
        totals.append(t - start)
if totals:
    print("-" * 78)
    label = "shell served (NO PAINT MARK — old build?)" if fallback else "first paint"
    print(f"cold start to {label}: median {statistics.median(totals):.1f} ms  "
          f"(min {min(totals):.1f}, max {max(totals):.1f}, n={len(totals)})")

# The fast path (document inlined into the shell) depends on the odoc Apple
# Event beating WebKit's first navigation. Nothing enforces that ordering, so
# report it rather than assume it.
inlined = sum(1 for _, events in runs
              if any("document inlined: true" in m for _, m in events))
served = sum(1 for _, events in runs
             if any(m.startswith("shell served") for _, m in events))
if served:
    note = "" if inlined == served else "   <-- FELL BACK to event delivery"
    print(f"fast path (document inlined into the shell): {inlined}/{served} runs{note}")
PY
