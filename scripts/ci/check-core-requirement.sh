#!/usr/bin/env bash
set -euo pipefail

# Consumers ignore the replace directive that points every provider at ../core,
# so a provider module tagged while still requiring an older core builds against
# that older core and misses whatever the release added to it.

root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
core_module="github.com/revenium/revenium-go-sdk/core"
failures=0

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  failures=$((failures + 1))
}

release_version() {
  sed -n 's/^## \[\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\)\].*/\1/p' "$root/CHANGELOG.md" | head -n 1
}

core_requirement() {
  awk -v core="$core_module" '
    /^replace/ { next }
    $1 == "require" && $2 == core { print $3; exit }
    $1 == core { print $2; exit }
  ' "$1"
}

version="${CORE_RELEASE_VERSION:-$(release_version)}"
version="${version#v}"
[ -n "$version" ] || {
  printf 'FAIL: no release heading such as "## [1.2.3]" found in %s/CHANGELOG.md\n' "$root" >&2
  exit 1
}

checked=0
for gomod in "$root"/*/go.mod; do
  module="$(basename "$(dirname "$gomod")")"
  case "$module" in core | examples) continue ;; esac
  required="$(core_requirement "$gomod")"
  [ -n "$required" ] || continue
  checked=$((checked + 1))
  [ "$required" = "v$version" ] ||
    fail "$module/go.mod requires $core_module $required, but this release is v$version; raise the require line to v$version"
done

[ "$checked" -gt 0 ] || fail "no provider go.mod under $root requires $core_module"

if [ "$failures" -gt 0 ]; then
  exit 1
fi
printf 'Every provider module requires core v%s (%d modules)\n' "$version" "$checked"
