#!/bin/sh
set -eu

archive=${1:-}
if [ -z "$archive" ] || [ ! -f "$archive" ]; then
  echo "usage: $0 release.tar.gz" >&2
  exit 1
fi

listing=$(tar -tzf "$archive")
root=$(printf '%s\n' "$listing" | head -n 1 | cut -d/ -f1)
if [ -z "$root" ]; then
  echo "release archive has no root directory" >&2
  exit 1
fi
for required in website config.json version.txt README.md CHANGELOG.md UPGRADING.md deploy docs uploads; do
  if ! printf '%s\n' "$listing" | grep -Eq "^${root}/${required}(/|$)"; then
    echo "release archive is missing $required" >&2
    exit 1
  fi
done
if printf '%s\n' "$listing" | grep -Eq "^${root}/(backups|\.git)(/|$)"; then
  echo "release archive contains private runtime or repository data" >&2
  exit 1
fi

temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT INT TERM
tar -xzf "$archive" -C "$temporary" "$root/config.json" "$root/version.txt"
cmp config.example.json "$temporary/$root/config.json"
expected=$(tr -d '[:space:]' < version.txt)
actual=$(tr -d '[:space:]' < "$temporary/$root/version.txt")
if [ "$actual" != "$expected" ]; then
  echo "archive version $actual does not match $expected" >&2
  exit 1
fi
echo "release archive valid: $archive"
