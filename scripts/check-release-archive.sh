#!/bin/sh
set -eu

archive=${1:-}
if [ -z "$archive" ] || [ ! -f "$archive" ]; then
  echo "usage: $0 release.zip|release.tar.gz" >&2
  exit 1
fi

case "$archive" in
  *.zip) listing=$(unzip -Z1 "$archive"); archive_type=zip ;;
  *.tar.gz|*.tgz) listing=$(tar -tzf "$archive"); archive_type=tar ;;
  *) echo "unsupported release archive: $archive" >&2; exit 1 ;;
esac
root=$(printf '%s\n' "$listing" | head -n 1 | cut -d/ -f1)
if [ -z "$root" ]; then
  echo "release archive has no root directory" >&2
  exit 1
fi
for required in website config.json version.txt DEPENDENCIES.txt README.md CHANGELOG.md UPGRADING.md deploy docs; do
  if ! printf '%s\n' "$listing" | grep -Eq "^${root}/${required}(/|$)"; then
    echo "release archive is missing $required" >&2
    exit 1
  fi
done
if printf '%s\n' "$listing" | grep -Eq "^${root}/(uploads|backups|\.git)(/|$)"; then
  echo "release archive contains private runtime or repository data" >&2
  exit 1
fi

temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT INT TERM
if [ "$archive_type" = zip ]; then
  unzip -qq "$archive" "$root/config.json" "$root/version.txt" -d "$temporary"
else
  tar -xzf "$archive" -C "$temporary" "$root/config.json" "$root/version.txt"
fi
cmp release/config.json "$temporary/$root/config.json"
expected=$(tr -d '[:space:]' < version.txt)
actual=$(tr -d '[:space:]' < "$temporary/$root/version.txt")
if [ "$actual" != "$expected" ]; then
  echo "archive version $actual does not match $expected" >&2
  exit 1
fi
if [ "$archive_type" = zip ]; then
  dependencies=$(unzip -p "$archive" "$root/DEPENDENCIES.txt")
else
  dependencies=$(tar -xOf "$archive" "$root/DEPENDENCIES.txt")
fi
if ! printf '%s\n' "$dependencies" | grep -Fqx "script-php/goweb"; then
  echo "archive dependency manifest does not identify the application module" >&2
  exit 1
fi
echo "release archive valid: $archive"
