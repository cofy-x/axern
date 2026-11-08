#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

usage() {
  echo 'Usage: make verify-release [ARGS=--list]'
  echo 'Run the complete local release gate. Use verify-full for scoped diagnosis.'
  echo 'Native deployment qualification, registry acceptance and publication remain separate.'
}

list_only=false
if (($# > 0)); then
  if (($# != 1)); then usage >&2; exit 2; fi
  case "$1" in
    --list) list_only=true ;;
    -h|--help) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
  esac
fi

# Existing owners keep their own checks: release-build includes release-check,
# and sdk-contract-verify includes clean-consumer SDK artifact verification.
targets=(verify-full security-check release-build open-source-check sdk-contract-verify docs-verify)
if [[ "${list_only}" == true ]]; then
  printf '%s\n' "${targets[@]}"
  exit 0
fi

cd "${ROOT_DIR}"
for target in "${targets[@]}"; do
  printf 'release_verification_step=%s\n' "${target}"
  make "${target}"
done
echo 'release_verification_ok=true'
