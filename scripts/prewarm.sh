#!/bin/sh
# Keeps MDv pre-launched (hidden) at login, so even the FIRST open of a session
# is a warm one (~60 ms rather than ~0.4 s).
#
# This is now a thin wrapper: the app registers its own login agent through
# SMAppService, which puts the item in System Settings > General > Login Items
# under MDv's name where it can be seen and revoked. The previous version of
# this script hand-wrote a plist into ~/Library/LaunchAgents, which worked but
# was invisible to that UI — a login item the system cannot attribute is one
# the user cannot reason about. It is removed here if still present.
#
# The same switch lives in the app under Aa > "Keep ready".
#
# Usage: scripts/prewarm.sh install | remove | status
set -e

BIN="/Applications/MDv.app/Contents/MacOS/md-view"
LEGACY="$HOME/Library/LaunchAgents/com.wails.md-view.prewarm.plist"

[ -x "$BIN" ] || { echo "error: $BIN not found — install the app first"; exit 1; }

# SMAppService checks the calling bundle's signature against the plist it ships,
# so this has to run the installed app, not a copy or a build directory.
if [ -f "$LEGACY" ]; then
  echo "removing the old hand-written login agent"
  launchctl unload "$LEGACY" 2>/dev/null || true
  rm -f "$LEGACY"
fi

case "$1" in
install) exec "$BIN" --prewarm on ;;
remove)  exec "$BIN" --prewarm off ;;
status|"") exec "$BIN" --prewarm status ;;
*)
  echo "usage: $0 install|remove|status" >&2
  exit 2
  ;;
esac
