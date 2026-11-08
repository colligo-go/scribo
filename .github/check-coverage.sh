#!/usr/bin/env bash
set -euo pipefail

floor_file=.github/coverage-floor

below_floor() { awk -v total="$1" -v floor="$2" 'BEGIN { exit !(total + 0 < floor + 0) }'; }
zero_functions() { awk '$1 != "total:" && $NF == "0.0%"' "$1"; }
weak_functions() { awk '$1 != "total:" { pct = $NF; sub("%", "", pct); if (pct + 0 < 80) print }' "$1"; }
total_of() { awk '$1 == "total:" { pct = $NF; sub("%", "", pct); print pct }' "$1"; }

expect() {
  local want=$1 got=$2 what=$3
  if [[ $got != "$want" ]]; then
    echo "self-test: $what: got $got, want $want"
    return 1
  fi
}

self_test() {
  local failed=0 tmp
  tmp=$(mktemp)
  trap 'rm -f "$tmp"' RETURN
  below_floor 90.9 91.0 && got=fail || got=pass
  expect fail "$got" "90.9 under floor 91.0" || failed=1
  below_floor 91.0 91.0 && got=fail || got=pass
  expect pass "$got" "91.0 at floor 91.0" || failed=1
  below_floor 100.0 95.5 && got=fail || got=pass
  expect pass "$got" "100.0 over floor 95.5" || failed=1
  printf '%s\n' 'a.go:1:	f	100.0%' 'a.go:9:	g	0.0%' 'b.go:3:	h	10.0%' 'total:	(statements)	92.3%' >"$tmp"
  expect 'a.go:9:	g	0.0%' "$(zero_functions "$tmp")" "zero detection" || failed=1
  expect 2 "$(weak_functions "$tmp" | wc -l | tr -d ' ')" "weak detection" || failed=1
  expect 92.3 "$(total_of "$tmp")" "total" || failed=1
  printf '%s\n' 'a.go:1:	f	100.0%' 'total:	(statements)	100.0%' >"$tmp"
  expect '' "$(zero_functions "$tmp")" "no zero" || failed=1
  return $failed
}

if [[ ${1:-} == --self-test ]]; then
  self_test && echo "self-test: ok"
  exit
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/unit" "$work/integ" "$work/merged"
pkg=github.com/colligo-go/scribo/...

go test -count=1 -covermode=atomic -coverpkg="$pkg" ./internal/... ./cmd -args -test.gocoverdir="$work/unit"
SCRIBO_COVERDIR=1 GOCOVERDIR="$work/integ" go test -count=1 -timeout 30m .
go tool covdata merge -i="$work/unit,$work/integ" -o="$work/merged"
go tool covdata textfmt -i="$work/merged" -o="$work/profile.txt"
go tool cover -func="$work/profile.txt" >"$work/func.txt"

total=$(total_of "$work/func.txt")
floor=$(tr -d '[:space:]' <"$floor_file")
echo "combined coverage: ${total}% (floor ${floor}%)"
echo "functions under 80%:"
weak_functions "$work/func.txt" | sed 's/^/  /'

status=0
zeros=$(zero_functions "$work/func.txt")
if [[ -n $zeros ]]; then
  echo "functions no test executes (docs/CONVENTIONS.md, Tests):"
  sed 's/^/  /' <<<"$zeros"
  status=1
fi
if below_floor "$total" "$floor"; then
  echo "combined coverage ${total}% is under the floor ${floor}% in $floor_file"
  status=1
fi
exit $status
