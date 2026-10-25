#!/bin/sh
set -eu

dist=${1:-dist}
if [ "$#" -gt 0 ]; then shift; fi
target_os=${1:-linux}
if [ "$#" -gt 0 ]; then shift; fi
if [ "$#" -eq 0 ]; then
  set -- amd64 arm64
fi

version=$(tr -d '[:space:]' < version.txt)
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT INT TERM
mkdir -p "$dist"
dist=$(cd "$dist" && pwd)

archives=""
for arch in "$@"; do
  package="goweb-${version}-${target_os}-${arch}"
  root="$temporary/$package"
  archive="$dist/$package.zip"
  mkdir -p "$root"
  GOOS="$target_os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$root/website" .
  cp release/config.json "$root/config.json"
  cp README.md CHANGELOG.md UPGRADING.md version.txt "$root/"
  go list -m all > "$root/DEPENDENCIES.txt"
  cp -R deploy docs "$root/"
  rm -f "$archive"
  (cd "$temporary" && zip -qr "$archive" "$package")
  ./scripts/check-release-archive.sh "$archive"
  archives="$archives $package.zip"
done

(
  cd "$dist"
  # shellcheck disable=SC2086
  sha256sum $archives > SHA256SUMS
)
printf 'release files ready in %s\n' "$dist"
