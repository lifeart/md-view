#!/bin/sh
# Points the Homebrew cask at a release: rewrites the `version` and `sha256`
# lines of Casks/md-view.rb in a checkout of lifeart/homebrew-tap. The release
# workflow runs it after the notarized DMG is published, with the hash of the
# DMG as GitHub serves it — the bytes `brew` will download, not the local file.
#
# Only those two lines change; the URL is derived from `version` inside the
# cask, so anything else (zap paths, depends_on) stays the tap's to edit.
# Fails rather than half-writes: each line must appear exactly once, and the
# result must contain exactly the values asked for.
# Usage: scripts/update-cask.sh path/to/Casks/md-view.rb <version> <sha256>
set -e

CASK="$1"
VERSION="$2"
SHA256="$3"
[ -f "$CASK" ] && [ -n "$VERSION" ] && [ -n "$SHA256" ] || {
  echo "usage: $0 path/to/Casks/md-view.rb <version> <sha256>"
  exit 2
}

# The version lands inside a Ruby string and a URL; keep it to what a v* tag
# minus its "v" looks like (0.4.0, 1.0.0-rc.1).
echo "$VERSION" | grep -Eq '^[0-9]+(\.[0-9]+)*(-[0-9A-Za-z.]+)?$' \
  || { echo "error: '$VERSION' does not look like a release version"; exit 1; }
echo "$SHA256" | grep -Eq '^[0-9a-f]{64}$' \
  || { echo "error: '$SHA256' is not a lowercase hex SHA-256"; exit 1; }

for stanza in version sha256; do
  n=$(grep -Ec "^  $stanza \"[^\"]*\"\$" "$CASK" || true)
  [ "$n" = 1 ] || { echo "error: expected one top-level '$stanza \"...\"' line in $CASK, found $n"; exit 1; }
done

# sed -i differs between BSD (macOS) and GNU (the Linux runner); a temp file
# works the same on both.
TMP="$CASK.tmp.$$"
trap 'rm -f "$TMP"' EXIT
sed -E \
  -e "s/^  version \"[^\"]*\"\$/  version \"$VERSION\"/" \
  -e "s/^  sha256 \"[^\"]*\"\$/  sha256 \"$SHA256\"/" \
  "$CASK" > "$TMP"

grep -qx "  version \"$VERSION\"" "$TMP" && grep -qx "  sha256 \"$SHA256\"" "$TMP" \
  || { echo "error: rewrite of $CASK did not take"; exit 1; }
mv "$TMP" "$CASK"
echo "$CASK -> $VERSION ($SHA256)"
