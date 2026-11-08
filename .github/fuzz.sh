#!/usr/bin/env bash
set -euo pipefail

duration=${1:?usage: fuzz.sh DURATION}
targets=(
  FuzzRecordKey:./internal/history
  FuzzCanonicalIsStable:./internal/db
  FuzzParseKeys:./cmd
  FuzzParseAt:./cmd
  FuzzParseWhere:./internal/validate
  FuzzEscape:./internal/format
  FuzzMultiQCRead:./internal/multiqc
  FuzzSchemaTSV:./internal/schema
)
declared=$(grep -rhoE '^func Fuzz[A-Za-z0-9_]+' --include='*_test.go' . | sed 's/^func //' | sort)
listed=$(printf '%s\n' "${targets[@]}" | cut -d: -f1 | sort)
if [[ $declared != "$listed" ]]; then
  echo "fuzz targets in the code and in .github/fuzz.sh differ:"
  diff <(echo "$declared") <(echo "$listed") || true
  exit 1
fi
fuzz() {
  go test -run='^$' -fuzz="^$1\$" -fuzztime="$duration" "$2" 2>&1 | tee "$log"
}
log=$(mktemp)
trap 'rm -f "$log"' EXIT
for target in "${targets[@]}"; do
  name=${target%%:*}
  echo "::group::$name"
  if ! fuzz "$name" "${target#*:}"; then
    if grep -q 'Failing input written to' "$log" || ! grep -q 'context deadline exceeded' "$log"; then
      exit 1
    fi
    echo "$name: the fuzzer stopped on its own deadline without a failing input; running it once more"
    fuzz "$name" "${target#*:}"
  fi
  echo "::endgroup::"
done
