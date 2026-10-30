#!/usr/bin/env bash
set -euo pipefail

types='feat|fix|test|docs|refactor|perf|ci|build|chore|style|revert'
branch_re="^(${types})/[a-z0-9]+(-[a-z0-9]+)*$"
subject_re="^(${types})(\\([a-z0-9-]+\\))?!?: [^ ].*[^. ]$"
body_types_re="^(feat|fix)(\\(|!|:)"
trailer_re='^(Co-Authored-By|Refs|BREAKING CHANGE|Signed-off-by):'

valid_branch() { [[ ${#1} -le 50 && $1 =~ $branch_re ]]; }
valid_subject() { [[ ${#1} -le 72 && $1 =~ $subject_re ]]; }
needs_body() { [[ $1 =~ $body_types_re ]]; }
has_body() { grep -v -E "$trailer_re" <<<"$1" | grep -q '[^[:space:]]'; }

expect() {
  local want=$1 check=$2 value=$3
  if $check "$value"; then got=accept; else got=reject; fi
  if [[ $got != "$want" ]]; then
    echo "self-test: $check should $want: $value"
    return 1
  fi
}

self_test() {
  local failed=0
  for b in feat/pair-missing fix/12-bgzf-bsize ci/commit-and-branch-names docs/translate-to-english; do
    expect accept valid_branch "$b" || failed=1
  done
  for b in session-5a-pairs wip/cli feat/Pair-Missing feat/pair_missing feat/ main fix/-bsize "feat/$(printf 'a%.0s' {1..50})"; do
    expect reject valid_branch "$b" || failed=1
  done
  for s in "fix: reject a leading UTF-8 BOM" "feat(format)!: drop the old extractor version" "test(verifier): cover pairs in separate directories"; do
    expect accept valid_subject "$s" || failed=1
  done
  for s in "wip: CLI fixes" "Fix: x" "fix: ends with a period." "fix:no space" "feat(Format): x" "fix: " "Merge branch 'x'" "fix: $(printf 'a%.0s' {1..70})"; do
    expect reject valid_subject "$s" || failed=1
  done
  expect accept needs_body "fix(format): x" || failed=1
  expect reject needs_body "test: x" || failed=1
  expect accept has_body $'Explains why.\n\nCo-Authored-By: a <b>' || failed=1
  expect reject has_body $'Co-Authored-By: a <b>\nRefs: #3' || failed=1
  return $failed
}

if [[ ${1:-} == --self-test ]]; then
  self_test && echo "self-test: ok"
  exit
fi

: "${HEAD_REF:?}" "${BASE_SHA:?}" "${HEAD_SHA:?}" "${PR_TITLE:?}"
status=0
if ! valid_branch "$HEAD_REF"; then
  echo "branch '$HEAD_REF' does not match <type>/<kebab-case-description>, at most 50 characters (docs/CONVENTIONS.md, Git)"
  status=1
fi
if ! valid_subject "$PR_TITLE"; then
  echo "pull request title '$PR_TITLE' does not match <type>(<scope>): <summary> (docs/CONVENTIONS.md, Git)"
  status=1
fi
while IFS= read -r sha; do
  subject=$(git log -1 --format=%s "$sha")
  body=$(git log -1 --format=%b "$sha")
  if ! valid_subject "$subject"; then
    echo "commit ${sha:0:7} '$subject' does not match <type>(<scope>): <summary>, at most 72 characters, no final period"
    status=1
  elif needs_body "$subject" && ! has_body "$body"; then
    echo "commit ${sha:0:7} '$subject' is a feat or fix without a body explaining why"
    status=1
  fi
done < <(git rev-list --no-merges "$BASE_SHA..$HEAD_SHA")
exit $status
