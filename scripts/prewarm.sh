#!/bin/sh
# Keeps md-view pre-launched (hidden) at login, so even the FIRST double-click
# of a markdown file is a warm open (~10x faster than a cold launch).
# Usage: scripts/prewarm.sh install | remove
set -e

PLIST="$HOME/Library/LaunchAgents/com.wails.md-view.prewarm.plist"
BIN="/Applications/MD View.app/Contents/MacOS/md-view"

case "$1" in
install)
  [ -x "$BIN" ] || { echo "error: $BIN not found — install the app first"; exit 1; }
  mkdir -p "$HOME/Library/LaunchAgents"
  cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.wails.md-view.prewarm</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN</string>
    <string>--hidden</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
EOF
  launchctl unload "$PLIST" 2>/dev/null || true
  launchctl load "$PLIST"
  echo "installed: md-view will prewarm at login (and was started now)"
  ;;
remove)
  launchctl unload "$PLIST" 2>/dev/null || true
  rm -f "$PLIST"
  echo "removed"
  ;;
*)
  echo "usage: $0 install|remove" >&2
  exit 2
  ;;
esac
