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
for target in "${targets[@]}"; do
  name=${target%%:*}
  echo "::group::$name"
  go test -run='^$' -fuzz="^${name}\$" -fuzztime="$duration" "${target#*:}"
  echo "::endgroup::"
done
