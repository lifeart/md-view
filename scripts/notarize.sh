#!/bin/sh
# Notarizes a Developer ID-signed bundle or disk image and staples the ticket,
# which is what lets other Macs open it without Gatekeeper's "unidentified
# developer" block. Signing alone is not enough: a Developer ID signature with
# no notarization is assessed as "Unnotarized Developer ID" and rejected.
#
# Usage: scripts/notarize.sh [path/to/md-view.app | path/to/md-view-X.Y.Z.dmg]
#
# Credentials — one of:
#   NOTARY_PROFILE=<name>    a profile stored earlier with
#                            `xcrun notarytool store-credentials <name>`
#                            (asks for Apple ID + app-specific password once)
#   NOTARY_KEY=<path.p8> NOTARY_KEY_ID=<id> NOTARY_ISSUER=<uuid>
#                            an App Store Connect API key (what CI uses)
set -e

TARGET="${1:-build/bin/md-view.app}"
[ -e "$TARGET" ] || { echo "error: $TARGET not found — run wails build && scripts/sign.sh first"; exit 1; }

if [ -n "${NOTARY_PROFILE:-}" ]; then
  set -- --keychain-profile "$NOTARY_PROFILE"
elif [ -n "${NOTARY_KEY:-}" ] && [ -n "${NOTARY_KEY_ID:-}" ] && [ -n "${NOTARY_ISSUER:-}" ]; then
  set -- --key "$NOTARY_KEY" --key-id "$NOTARY_KEY_ID" --issuer "$NOTARY_ISSUER"
else
  echo "error: no notarization credentials. Set NOTARY_PROFILE, or"
  echo "NOTARY_KEY + NOTARY_KEY_ID + NOTARY_ISSUER (see the header of this file)."
  exit 2
fi

# The signature must be a Developer ID one with the hardened runtime, or the
# submission is accepted and then fails on Apple's side minutes later.
codesign -dv --verbose=2 "$TARGET" 2>&1 | grep -q "Authority=Developer ID Application" || {
  echo "error: $TARGET is not signed with a Developer ID Application certificate"
  echo "run: scripts/sign.sh"
  exit 1
}

case "$TARGET" in
*.app)
  # notarytool takes an archive, not a bundle. ditto --keepParent preserves
  # the .app directory structure the way Apple expects.
  UPLOAD="$(mktemp -d)/$(basename "$TARGET" .app).zip"
  /usr/bin/ditto -c -k --keepParent "$TARGET" "$UPLOAD"
  ;;
*)
  UPLOAD="$TARGET"
  ;;
esac

echo "submitting $(basename "$UPLOAD") — Apple usually answers in 1-5 minutes"
xcrun notarytool submit "$UPLOAD" "$@" --wait

# Stapling attaches the ticket to the bundle/dmg so it also validates offline.
xcrun stapler staple "$TARGET"
xcrun stapler validate "$TARGET"

echo "--- Gatekeeper assessment:"
case "$TARGET" in
*.dmg) spctl -a -vvv -t open --context context:primary-signature "$TARGET" ;;
*)     spctl -a -vvv -t exec "$TARGET" ;;
esac
echo "notarized and stapled: $TARGET"
