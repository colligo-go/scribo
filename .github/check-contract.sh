#!/usr/bin/env bash
set -euo pipefail

repo=colligo-go/colligo
path=docs/CONTRACT.md

fetch_remote() {
  if [[ -n ${GH_TOKEN:-} ]]; then
    gh api -H "Accept: application/vnd.github.raw" "repos/${repo}/contents/${path}?ref=main"
  else
    curl --fail --silent --show-error --location "https://raw.githubusercontent.com/${repo}/main/${path}"
  fi
}

compare() {
  local local_file=$1 remote_file=$2
  if diff -u "$remote_file" "$local_file"; then
    echo "CONTRACT.md is identical to the copy on ${repo}@main"
  else
    echo "CONTRACT.md differs from the copy on ${repo}@main: change both repositories in the same cycle" >&2
    return 1
  fi
}

self_test() {
  local dir
  dir=$(mktemp -d)
  trap 'rm -rf "$dir"' RETURN
  printf 'same\n' >"$dir/a"
  printf 'same\n' >"$dir/b"
  printf 'other\n' >"$dir/c"
  compare "$dir/a" "$dir/b" >/dev/null || { echo "self-test: identical files should pass"; return 1; }
  if compare "$dir/a" "$dir/c" >/dev/null 2>&1; then
    echo "self-test: different files should fail"
    return 1
  fi
  echo "self-test ok"
}

if [[ ${1:-} == "--self-test" ]]; then
  self_test
  exit
fi

remote=$(mktemp)
trap 'rm -f "$remote"' EXIT
if ! fetch_remote >"$remote"; then
  echo "cannot read ${path} from ${repo}@main: set the COLLIGO_READ_TOKEN secret (read access to repository contents) while the repository is private" >&2
  exit 1
fi
compare "${1:-$path}" "$remote"
