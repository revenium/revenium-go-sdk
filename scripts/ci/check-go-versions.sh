#!/usr/bin/env bash
set -euo pipefail

root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
docs=(README.md llms.txt llms-full.txt)
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
failures=0

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  failures=$((failures + 1))
}

go_directive() {
  sed -n 's/^go[[:space:]][[:space:]]*\([0-9][0-9.]*\).*/\1/p' "$1" | head -n 1 |
    sed 's/^\([0-9]*\.[0-9]*\)\.0$/\1/'
}

table_header='| Module | Minimum Go |'

minimum_go_table() {
  awk -v header="$table_header" '
    $0 == header { inside = 1; next }
    inside && !/^\|/ { inside = 0 }
    inside && match($0, /^\| `[a-z0-9-]+` \| [0-9.]+ \|$/) {
      split($0, cells, "|")
      gsub(/[` ]/, "", cells[2])
      gsub(/ /, "", cells[3])
      print cells[2], cells[3]
    }
  ' "$1"
}

by_version() {
  sort -t. -k1,1n -k2,2n -k3,3n
}

for gomod in "$root"/*/go.mod; do
  module="$(basename "$(dirname "$gomod")")"
  [ "$module" = examples ] && continue
  printf '%s %s\n' "$module" "$(go_directive "$gomod")"
done | sort >"$scratch/go-mod"

[ -s "$scratch/go-mod" ] || fail "no module go.mod files found under $root"

lowest="$(cut -d' ' -f2 "$scratch/go-mod" | by_version | head -n 1)"
highest="$(cut -d' ' -f2 "$scratch/go-mod" | by_version | tail -n 1)"
range="Go ${lowest} to ${highest} by module"

for doc in "${docs[@]}"; do
  path="$root/$doc"
  if [ ! -f "$path" ]; then
    fail "$doc is missing"
    continue
  fi

  tables="$(grep -cFx "$table_header" "$path" || true)"
  [ "$tables" = 1 ] || fail "$doc has $tables '$table_header' tables, expected exactly one"
  minimum_go_table "$path" | sort >"$scratch/documented"
  if ! diff -u "$scratch/go-mod" "$scratch/documented" >"$scratch/diff"; then
    fail "$doc minimum Go table does not match the go.mod files (- go.mod, + $doc):
$(tail -n +3 "$scratch/diff")"
  fi

  grep -Fq "$range" "$path" || fail "$doc does not state the range '$range'"

  if grep -En 'Go:? 1\.[0-9]+(\.[0-9]+)?\+' "$path" >"$scratch/stale"; then
    fail "$doc states a single minimum Go for every module:
$(cat "$scratch/stale")"
  fi
done

badge="badge/Go-${lowest}%20to%20${highest}%20by%20module-"
grep -Fq "$badge" "$root/README.md" || fail "README.md badge does not show '$range'"

workflow="$root/.github/workflows/test.yml"
sed -n '/^  test:/,/^  [a-z]/s/^          - \([a-z0-9-]*\)$/\1/p' "$workflow" | sort >"$scratch/latest-go-job"
sed -n '/^  test-minimum-go:/,/^  [a-z]/s/^        module: \[\(.*\)\]$/\1/p' "$workflow" | tr ',' '\n' | tr -d ' ' | sed '/^$/d' | sort >"$scratch/minimum-go-job"
cut -d' ' -f1 "$scratch/go-mod" >"$scratch/modules"
for job in latest-go-job minimum-go-job; do
  if ! diff -u "$scratch/modules" "$scratch/$job" >"$scratch/diff"; then
    fail "the $job module list in .github/workflows/test.yml does not cover every module (- go.mod, + workflow):
$(tail -n +3 "$scratch/diff")"
  fi
done

workspace="$(go_directive "$root/go.work")"
grep -Fq "Go ${workspace} or later" "$root/CONTRIBUTING.md" ||
  fail "CONTRIBUTING.md does not ask contributors for 'Go ${workspace} or later' (go.work)"

if [ "$failures" -gt 0 ]; then
  printf '%d Go version documentation check(s) failed\n' "$failures" >&2
  exit 1
fi
printf 'Documented minimum Go versions match go.mod (%s)\n' "$range"
