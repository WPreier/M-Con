#!/bin/sh
# Usage: scripts/bump-version.sh patch|minor|major
set -eu
KIND=${1:?patch, minor, or major}
PREV=$(tr -d '[:space:]' < VERSION)
major=$(printf '%s' "$PREV" | cut -d. -f1)
minor=$(printf '%s' "$PREV" | cut -d. -f2)
patch=$(printf '%s' "$PREV" | cut -d. -f3)
case "$KIND" in
  major) major=$((major + 1)); minor=0; patch=0 ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
  *) echo "unknown kind $KIND" >&2; exit 1 ;;
esac
NEXT="$major.$minor.$patch"
printf '%s\n' "$NEXT" > VERSION
DATE=$(date -u +%Y-%m-%d)
TMP=$(mktemp)
awk -v ver="$NEXT" -v date="$DATE" '
  /^## \[Unreleased\]/ {
    print
    print ""
    print "## [" ver "] - " date
    print ""
    print "### Changed"
    print ""
    print "- " ver
    next
  }
  { print }
' CHANGELOG.md > "$TMP"
mv "$TMP" CHANGELOG.md
echo "$NEXT"
