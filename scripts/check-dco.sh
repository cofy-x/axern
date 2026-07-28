#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: check-dco.sh <base-sha> <head-sha>" >&2
  exit 2
fi

base_sha="$1"
head_sha="$2"
missing=0

while IFS= read -r commit; do
  if ! git show -s --format=%B "${commit}" |
    grep -Eq '^Signed-off-by: [^<]+ <[^[:space:]<>]+@[^[:space:]<>]+>$'; then
    echo "commit ${commit} is missing a valid Signed-off-by trailer" >&2
    missing=1
  fi
done < <(git rev-list "${base_sha}..${head_sha}")

if [[ "${missing}" -ne 0 ]]; then
  echo "See CONTRIBUTING.md and DCO for the contribution sign-off policy." >&2
  exit 1
fi

echo "dco_check=passed"
