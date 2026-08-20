#!/bin/sh
# Signs the built app with a stable identity: a "Developer ID Application"
# certificate when one is in the keychain (required for distribution to other
# Macs — see scripts/notarize.sh for the second half), otherwise an
# "Apple Development" one, which is enough locally and keeps TCC permissions
# across rebuilds (wails build only ad-hoc-signs, which changes every build).
# Usage: scripts/sign.sh ["identity name or SHA-1"] [path/to/md-view.app]
set -e

APP="${2:-build/bin/md-view.app}"
IDENTITY="${1:-}"
if [ -z "$IDENTITY" ]; then
  IDENTITY=$(security find-identity -v -p codesigning | awk -F'"' '/Developer ID Application/ {print $2; exit}')
  [ -n "$IDENTITY" ] || IDENTITY=$(security find-identity -v -p codesigning | awk -F'"' '/Apple Development/ {print $2; exit}')
  [ -n "$IDENTITY" ] || { echo "error: no signing identity found; pass one explicitly"; exit 1; }
fi
[ -d "$APP" ] || { echo "error: $APP not found — run wails build first"; exit 1; }

# --timestamp and --options runtime (hardened runtime) are both prerequisites
# for notarization; they cost nothing on a local-only signature.
codesign --force --options runtime --timestamp --sign "$IDENTITY" "$APP"
codesign --verify --deep --strict "$APP"
echo "signed '$APP' as: $IDENTITY"

case "$IDENTITY" in
Developer\ ID\ Application*)
  echo "next: scripts/notarize.sh — Gatekeeper on other Macs needs the app"
  echo "notarized and stapled, not just Developer ID signed."
  ;;
*)
  echo "note: this identity is fine on this Mac only. Distribution to other"
  echo "machines needs a 'Developer ID Application' certificate + notarization."
  ;;
esac
