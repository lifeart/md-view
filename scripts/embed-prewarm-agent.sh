#!/bin/zsh
# Puts the prewarm login agent inside the app bundle, where SMAppService
# expects to find it: Contents/Library/LaunchAgents/<Label>.plist.
#
# `wails build` does not copy arbitrary files into the bundle, so this runs
# after it — and before scripts/sign.sh, since the agent is part of what the
# app's signature seals.
#
# The agent itself does nothing unless the reader opts in: MDv registers it
# through SMAppService only when they accept the offer (see prewarm_darwin.go).
# Shipping the file is not the same as enabling it.
#
# Usage: scripts/embed-prewarm-agent.sh [path/to/md-view.app]
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="${1:-$ROOT/build/bin/md-view.app}"
[[ -d "$APP" ]] || { echo "error: $APP not found — run wails build first"; exit 2 }

SRC="$ROOT/prewarm/com.wails.md-view.prewarm.plist"
DEST_DIR="$APP/Contents/Library/LaunchAgents"
mkdir -p "$DEST_DIR"
cp "$SRC" "$DEST_DIR/"
plutil -lint "$DEST_DIR/com.wails.md-view.prewarm.plist" >/dev/null
echo "==> embedded $(basename "$SRC") in ${APP##*/}/Contents/Library/LaunchAgents"
