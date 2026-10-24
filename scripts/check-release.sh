#!/bin/sh
set -eu

version=$(tr -d '[:space:]' < version.txt)
if ! printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "version.txt must contain exactly major.minor.patch" >&2
  exit 1
fi
if ! grep -Fqx "## $version" CHANGELOG.md; then
  echo "CHANGELOG.md has no release heading for $version" >&2
  exit 1
fi
if ! grep -Fqx "module script-php/goweb" go.mod; then
  echo "go.mod does not use the canonical script-php/goweb module path" >&2
  exit 1
fi

tag=${RELEASE_TAG:-${GITHUB_REF_NAME:-}}
if [ -n "$tag" ] && [ "$tag" != "v$version" ]; then
  echo "release tag $tag does not match version.txt (expected v$version)" >&2
  exit 1
fi

printf 'release metadata valid: v%s\n' "$version"
