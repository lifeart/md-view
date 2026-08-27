#!/bin/zsh
# Builds the Quick Look preview extension and embeds it in the app bundle.
#
# `wails build` knows nothing about app extensions, so this runs after it and
# adds Contents/PlugIns/MDvQuickLook.appex. Run scripts/sign.sh afterwards:
# nested code must be signed before the bundle that contains it, and sign.sh
# does that in the right order.
#
# The extension renders through MDv's own pipeline rather than a second
# markdown implementation: quicklook/bridge is compiled to a C static library
# (internal/quicklook -> internal/render) and the Swift extension links it.
#
# Usage: scripts/build-quicklook.sh [path/to/md-view.app]
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="${1:-$ROOT/build/bin/md-view.app}"
[[ -d "$APP" ]] || { echo "error: $APP not found — run wails build first"; exit 2 }
APP="$(cd "$(dirname "$APP")" && pwd)/$(basename "$APP")"

# Match the host app's version: macOS rejects a nested bundle whose version
# disagrees with its container in some validation paths, and a stale version in
# a plug-in is a confusing thing to debug.
VERSION="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$APP/Contents/Info.plist")"
DEPLOY_TARGET=12.0

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> Go: libmdvpreview.a (the render pipeline as a C archive)"
# Go does not read MACOSX_DEPLOYMENT_TARGET for cgo; the minimum has to reach
# clang directly, or the archive is stamped with the SDK's version and the
# linker warns that it is newer than the extension being built.
( cd "$ROOT" && \
  CGO_CFLAGS="-mmacosx-version-min=$DEPLOY_TARGET" \
  CGO_LDFLAGS="-mmacosx-version-min=$DEPLOY_TARGET" \
  MACOSX_DEPLOYMENT_TARGET="$DEPLOY_TARGET" \
    go build -buildmode=c-archive -o "$WORK/libmdvpreview.a" ./quicklook/bridge )

echo "==> Swift: the extension executable"
mkdir -p "$WORK/appex/Contents/MacOS"
# -parse-as-library: there is no main.swift. The entry point of an app
# extension is Foundation's NSExtensionMain, which Xcode supplies via this same
# linker flag.
MACOSX_DEPLOYMENT_TARGET="$DEPLOY_TARGET" swiftc \
  -O -parse-as-library \
  -target "arm64-apple-macosx$DEPLOY_TARGET" \
  -module-name MDvQuickLook \
  -import-objc-header "$WORK/libmdvpreview.h" \
  -o "$WORK/appex/Contents/MacOS/MDvQuickLook" \
  "$ROOT/quicklook/MDvQuickLook/PreviewProvider.swift" \
  "$WORK/libmdvpreview.a" \
  -framework Foundation -framework CoreGraphics -framework QuickLookUI \
  -Xlinker -e -Xlinker _NSExtensionMain

echo "==> assembling MDvQuickLook.appex"
cp "$ROOT/quicklook/MDvQuickLook/Info.plist" "$WORK/appex/Contents/Info.plist"
for key in CFBundleShortVersionString CFBundleVersion; do
  /usr/libexec/PlistBuddy -c "Set :$key $VERSION" "$WORK/appex/Contents/Info.plist"
done
plutil -lint "$WORK/appex/Contents/Info.plist" >/dev/null

DEST="$APP/Contents/PlugIns/MDvQuickLook.appex"
mkdir -p "$APP/Contents/PlugIns"
rm -rf "$DEST"
mv "$WORK/appex" "$DEST"

# Ad-hoc sign so the extension is loadable straight after a plain
# `wails build`; scripts/sign.sh replaces this with the Developer ID signature.
codesign --force --sign - --timestamp=none \
  --entitlements "$ROOT/quicklook/MDvQuickLook/MDvQuickLook.entitlements" "$DEST" 2>/dev/null

SIZE="$(du -sh "$DEST" | cut -f1)"
echo "==> $DEST ($SIZE, version $VERSION)"
echo
echo "Next: scripts/sign.sh (signs the extension before the app), then install"
echo "the bundle. macOS only loads a Quick Look extension from an app it has"
echo "registered, so the app must live somewhere LaunchServices scans"
echo "(/Applications) — previewing from a build directory will not work."
