#!/bin/sh
# Signs the built app with a stable identity so macOS keeps TCC permissions
# across rebuilds (wails build only ad-hoc-signs, which changes every build).
# Usage: scripts/sign.sh ["identity name or SHA-1"] [path/to/md-view.app]
# Default identity: the first "Apple Development" certificate in the keychain.
set -e

APP="${2:-build/bin/md-view.app}"
IDENTITY="${1:-}"
if [ -z "$IDENTITY" ]; then
  IDENTITY=$(security find-identity -v -p codesigning | awk -F'"' '/Apple Development/ {print $2; exit}')
  [ -n "$IDENTITY" ] || { echo "error: no Apple Development identity found; pass one explicitly"; exit 1; }
fi
[ -d "$APP" ] || { echo "error: $APP not found — run wails build first"; exit 1; }

codesign --force --options runtime --sign "$IDENTITY" "$APP"
codesign --verify --strict "$APP"
echo "signed '$APP' as: $IDENTITY"
echo "note: this does NOT notarize — distributing to other machines still"
echo "needs a 'Developer ID Application' certificate + notarytool."
