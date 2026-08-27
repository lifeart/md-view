#!/bin/sh
# Signs the built app with a stable identity: a "Developer ID Application"
# certificate when one is in the keychain (required for distribution to other
# Macs — see scripts/notarize.sh for the second half), otherwise an
# "Apple Development" one, which is enough locally and keeps TCC permissions
# across rebuilds (wails build only ad-hoc-signs, which changes every build).
# Also signs a .dmg when given one: the disk image people actually download
# should carry its own verifiable signature, and notarization submits it as a
# single signed unit.
# Usage: scripts/sign.sh ["identity name or SHA-1"] [path/to/md-view.app | path/to/x.dmg]
set -e

TARGET="${2:-build/bin/md-view.app}"
IDENTITY="${1:-}"
if [ -z "$IDENTITY" ]; then
  IDENTITY=$(security find-identity -v -p codesigning | awk -F'"' '/Developer ID Application/ {print $2; exit}')
  [ -n "$IDENTITY" ] || IDENTITY=$(security find-identity -v -p codesigning | awk -F'"' '/Apple Development/ {print $2; exit}')
  [ -n "$IDENTITY" ] || { echo "error: no signing identity found; pass one explicitly"; exit 1; }
fi
[ -e "$TARGET" ] || { echo "error: $TARGET not found — run wails build first"; exit 1; }

# --timestamp is a notarization prerequisite everywhere. --options runtime
# (hardened runtime) applies to executable code only — a disk image carries no
# code of its own, and codesign rejects the flag on one.
case "$TARGET" in
*.dmg)
  codesign --force --timestamp --sign "$IDENTITY" "$TARGET"
  codesign --verify --strict "$TARGET"
  ;;
*)
  # Nested code must be signed before the bundle that contains it: the outer
  # signature seals a hash of the inner one, so signing outside-in produces a
  # bundle that verifies today and breaks the moment anything re-reads it.
  # `codesign --deep` is Apple-deprecated for signing (it is fine for
  # verifying), so walk the plug-ins explicitly. Today that is the Quick Look
  # extension from scripts/build-quicklook.sh.
  for nested in "$TARGET"/Contents/PlugIns/*.appex; do
    [ -e "$nested" ] || continue
    echo "signing nested: ${nested##*/}"
    ENTS="$(dirname "$0")/../quicklook/MDvQuickLook/MDvQuickLook.entitlements"
    if [ -f "$ENTS" ]; then
      codesign --force --options runtime --timestamp \
        --entitlements "$ENTS" --sign "$IDENTITY" "$nested"
    else
      codesign --force --options runtime --timestamp --sign "$IDENTITY" "$nested"
    fi
    codesign --verify --strict "$nested"
  done
  codesign --force --options runtime --timestamp --sign "$IDENTITY" "$TARGET"
  codesign --verify --deep --strict "$TARGET"
  ;;
esac
echo "signed '$TARGET' as: $IDENTITY"

case "$IDENTITY" in
Developer\ ID\ Application*)
  echo "next: scripts/notarize.sh — Gatekeeper on other Macs needs this"
  echo "notarized and stapled, not just Developer ID signed."
  ;;
*)
  echo "note: this identity is fine on this Mac only. Distribution to other"
  echo "machines needs a 'Developer ID Application' certificate + notarization."
  ;;
esac
