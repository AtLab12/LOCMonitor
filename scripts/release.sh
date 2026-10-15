#!/bin/sh
# Package a tagged release without network access or third-party dependencies.
set -eu
version=${1:?usage: scripts/release.sh v0.1.0}
if ! printf '%s\n' "$version" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo 'Expected a version such as v0.1.0' >&2; exit 1
fi
tag_commit=$(git rev-parse --verify "refs/tags/$version^{commit}")
if [ "$tag_commit" != "$(git rev-parse HEAD)" ] || [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  echo 'Build releases from a clean checkout of the requested tag' >&2; exit 1
fi
mkdir -p dist
# Archive the tag, not a potentially dirty working tree.
git archive --format=tar --prefix="locmonitor-${version#v}/" --output="dist/source.tar" "$tag_commit"
gzip -n -c dist/source.tar > "dist/locmonitor-${version#v}-source.tar.gz"
rm dist/source.tar
for target in darwin/arm64 darwin/amd64 linux/arm64 linux/amd64; do
  goos=${target%/*}
  goarch=${target#*/}
  stage=$(mktemp -d)
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$stage/locmonitor" ./cmd/locmonitor
  cp LICENSE README.md "$stage/"
  tar -czf "dist/locmonitor-${version#v}-$goos-$goarch.tar.gz" -C "$stage" locmonitor LICENSE README.md
  rm -rf "$stage"
done
(cd dist && shasum -a 256 ./*.tar.gz > checksums.txt)
source_sha=$(shasum -a 256 "dist/locmonitor-${version#v}-source.tar.gz" | cut -d ' ' -f 1)
# The release formula can replace the HEAD-only formula in the tap.
awk -v version="${version#v}" -v sha="$source_sha" '
  /^  (url|sha256) / { next }
  /  head / {
    print "  url \"https://github.com/AtLab12/LOCMonitor/releases/download/v" version "/locmonitor-" version "-source.tar.gz\""
    print "  sha256 \"" sha "\""
  }
  { print }
' Formula/locmonitor.rb > dist/locmonitor.rb
