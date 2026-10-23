#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_dir/../.." && pwd)"
checker="${script_dir}/check-go-versions.sh"
fixtures="$(mktemp -d)"
trap 'rm -rf "$fixtures"' EXIT

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

new_fixture() {
  local fixture="$fixtures/$1"
  mkdir -p "$fixture"
  cp "$repo_root"/{README.md,llms.txt,llms-full.txt,CONTRIBUTING.md,go.work} "$fixture/"
  mkdir -p "$fixture/.github/workflows"
  cp "$repo_root/.github/workflows/test.yml" "$fixture/.github/workflows/test.yml"
  for gomod in "$repo_root"/*/go.mod; do
    mkdir -p "$fixture/$(basename "$(dirname "$gomod")")"
    cp "$gomod" "$fixture/$(basename "$(dirname "$gomod")")/go.mod"
  done
  printf '%s' "$fixture"
}

replace_in() {
  local file="$1" from="$2" to="$3"
  grep -Fq -- "$from" "$file" || fail "fixture setup: '$from' not found in $file"
  FROM="$from" TO="$to" perl -0pi -e 's/\Q$ENV{FROM}\E/$ENV{TO}/' "$file"
}

expect_pass() {
  "$checker" "$2" >/dev/null 2>&1 || fail "$1: expected the check to pass"
}

expect_failure() {
  local name="$1" fixture="$2" message="$3"
  if "$checker" "$fixture" >"$fixtures/$name.out" 2>&1; then
    fail "$name: expected the check to fail"
  fi
  grep -Fq -- "$message" "$fixtures/$name.out" ||
    fail "$name: expected '$message' in $(cat "$fixtures/$name.out")"
}

go_version_of() {
  sed -n 's/^go[[:space:]][[:space:]]*\([0-9][0-9.]*\).*/\1/p' "$1" | head -n 1
}

short_version() {
  sed 's/^\([0-9]*\.[0-9]*\)\.0$/\1/' <<<"$1"
}

next_minor() {
  awk -F. '{ printf "%d.%d", $1, $2 + 1 }' <<<"$1"
}

set_go_version() {
  VERSION="$2" perl -0pi -e 's/^go[ \t]+\S+/go $ENV{VERSION}/m' "$1"
}

modules=()
for gomod in "$repo_root"/*/go.mod; do
  module="$(basename "$(dirname "$gomod")")"
  [ "$module" = examples ] || modules+=("$module")
done
[ "${#modules[@]}" -gt 0 ] || fail "no modules found under $repo_root"
module="${modules[0]}"
module_version="$(short_version "$(go_version_of "$repo_root/$module/go.mod")")"
module_row="| \`$module\` | $module_version |"
highest="$(for m in "${modules[@]}"; do short_version "$(go_version_of "$repo_root/$m/go.mod")"; done |
  sort -t. -k1,1n -k2,2n -k3,3n | tail -n 1)"

expect_pass "repository as committed" "$(new_fixture clean)"

fixture="$(new_fixture raised-go-mod)"
set_go_version "$fixture/$module/go.mod" "$(next_minor "$highest")"
expect_failure raised-go-mod "$fixture" "README.md minimum Go table does not match"

fixture="$(new_fixture changed-row)"
replace_in "$fixture/llms-full.txt" "$module_row" "| \`$module\` | 0.1 |"
expect_failure changed-row "$fixture" "llms-full.txt minimum Go table does not match"

fixture="$(new_fixture missing-row)"
replace_in "$fixture/llms.txt" "$module_row"$'\n' ''
expect_failure missing-row "$fixture" "llms.txt minimum Go table does not match"

fixture="$(new_fixture unrelated-two-column-table)"
printf '\n| Setting | Value |\n|---|---|\n| `%s` | 0.1 |\n' "$module" >>"$fixture/README.md"
expect_pass unrelated-two-column-table "$fixture"

fixture="$(new_fixture second-minimum-go-table)"
printf '\n| Module | Minimum Go |\n|---|---|\n%s\n' "$module_row" >>"$fixture/README.md"
expect_failure second-minimum-go-table "$fixture" "README.md has 2 '| Module | Minimum Go |' tables"

fixture="$(new_fixture single-floor-claim)"
printf '\n**Requirements:** Go 1.22+\n' >>"$fixture/README.md"
expect_failure single-floor-claim "$fixture" "README.md states a single minimum Go for every module"

fixture="$(new_fixture stale-badge)"
badge="$(grep -o 'badge/Go-[^)]*by%20module-' "$fixture/README.md" | head -n 1)"
[ -n "$badge" ] || fail "fixture setup: no minimum Go badge in README.md"
replace_in "$fixture/README.md" "$badge" "badge/Go-1.22%2B-"
expect_failure stale-badge "$fixture" "README.md badge does not show"

fixture="$(new_fixture new-floor-range)"
set_go_version "$fixture/$module/go.mod" "1.1"
expect_failure new-floor-range "$fixture" "does not state the range 'Go 1.1 to $highest by module'"

fixture="$(new_fixture workspace-bump)"
workspace_next="$(next_minor "$(go_version_of "$repo_root/go.work")")"
set_go_version "$fixture/go.work" "$workspace_next"
expect_failure workspace-bump "$fixture" "CONTRIBUTING.md does not ask contributors for 'Go $workspace_next or later'"

fixture="$(new_fixture module-missing-from-latest-go-job)"
replace_in "$fixture/.github/workflows/test.yml" "          - $module"$'\n' ''
expect_failure module-missing-from-latest-go-job "$fixture" "the latest-go-job module list"

fixture="$(new_fixture module-missing-from-minimum-go-job)"
MODULE="$module" perl -pi -e 's/^(        module: \[)(.*)\]$/$1 . join(", ", grep { $_ ne $ENV{MODULE} } split(m{,\s*}, $2)) . "]"/e' \
  "$fixture/.github/workflows/test.yml"
expect_failure module-missing-from-minimum-go-job "$fixture" "the minimum-go-job module list"

printf 'check-go-versions tests passed\n'
