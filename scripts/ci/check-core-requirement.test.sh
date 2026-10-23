#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
checker="${script_dir}/check-core-requirement.sh"
fixtures="$(mktemp -d)"
trap 'rm -rf "$fixtures"' EXIT

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

new_fixture() {
  local fixture="$fixtures/$1"
  mkdir -p "$fixture"
  cp "$repo_root/CHANGELOG.md" "$fixture/"
  for gomod in "$repo_root"/*/go.mod; do
    mkdir -p "$fixture/$(basename "$(dirname "$gomod")")"
    cp "$gomod" "$fixture/$(basename "$(dirname "$gomod")")/go.mod"
  done
  printf '%s' "$fixture"
}

expect_pass() {
  "$checker" "$2" >/dev/null 2>&1 || fail "$1: expected the check to pass"
}

expect_failure() {
  local name="$1" fixture="$2" message="$3"
  if CORE_RELEASE_VERSION="${4:-}" "$checker" "$fixture" >"$fixtures/$name.out" 2>&1; then
    fail "$name: expected the check to fail"
  fi
  grep -Fq -- "$message" "$fixtures/$name.out" ||
    fail "$name: expected '$message' in: $(cat "$fixtures/$name.out")"
}

set_core_requirement() {
  VERSION="$2" perl -pi -e 's{^(\s*(?:require\s+)?github\.com/revenium/revenium-go-sdk/core\s+)v\S+}{$1$ENV{VERSION}}' "$1"
}

provider=""
for gomod in "$repo_root"/*/go.mod; do
  module="$(basename "$(dirname "$gomod")")"
  case "$module" in core | examples) continue ;; esac
  if grep -q '^[[:space:]]*github.com/revenium/revenium-go-sdk/core[[:space:]]' "$gomod"; then
    provider="$module"
    break
  fi
done
[ -n "$provider" ] || fail "no provider module requires core under $repo_root"

expect_pass "repository as committed" "$(new_fixture clean)"

fixture="$(new_fixture provider-behind)"
set_core_requirement "$fixture/$provider/go.mod" v1.0.0
expect_failure provider-behind "$fixture" "$provider/go.mod requires github.com/revenium/revenium-go-sdk/core v1.0.0"

fixture="$(new_fixture new-release-heading)"
perl -0pi -e 's/^## \[/## [98.0.0] - Unreleased\n\n### Fixed\n\n- nothing\n\n## [/m' "$fixture/CHANGELOG.md"
expect_failure new-release-heading "$fixture" "but this release is v98.0.0"

fixture="$(new_fixture release-tag-ahead)"
expect_failure release-tag-ahead "$fixture" "but this release is v99.0.0" v99.0.0

fixture="$(new_fixture examples-ignored)"
set_core_requirement "$fixture/examples/go.mod" v1.0.0
expect_pass examples-ignored "$fixture"

printf 'check-core-requirement tests passed\n'
