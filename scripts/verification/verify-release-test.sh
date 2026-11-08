#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERIFY="${ROOT_DIR}/scripts/verify-release.sh"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/axern-release-gate-contract.XXXXXX")"
trap 'rm -rf -- "${work_dir}"' EXIT
export RELEASE_GATE_RECEIPT="${work_dir}/calls"

# Exercise the real Make wrapper and orchestrator without compiling artifacts,
# starting runtimes, invoking scanners or publishing anything.
make() {
  printf '%s\n' "$*" >>"${RELEASE_GATE_RECEIPT}"
  if [[ "$*" == "${FAIL_RELEASE_TARGET:-}" ]]; then return 73; fi
}
export -f make

printf '%s\n' verify-full security-check release-build open-source-check sdk-contract-verify docs-verify >"${work_dir}/expected"
bash "${VERIFY}" --list >"${work_dir}/listed"
cmp "${work_dir}/expected" "${work_dir}/listed"
test ! -e "${RELEASE_GATE_RECEIPT}"
command make --no-print-directory -s -C "${ROOT_DIR}" verify-release >"${work_dir}/success.log"
cmp "${work_dir}/expected" "${RELEASE_GATE_RECEIPT}"
grep -Fx 'release_verification_ok=true' "${work_dir}/success.log" >/dev/null

# Every owner is mandatory. A failure must preserve its status and prevent all
# later owners from running; successful earlier owners must not run twice.
for target in verify-full security-check release-build open-source-check sdk-contract-verify docs-verify; do
  : >"${RELEASE_GATE_RECEIPT}"
  if FAIL_RELEASE_TARGET="${target}" bash "${VERIFY}" >"${work_dir}/failure.log" 2>&1; then
    echo "release gate accepted a failed ${target}" >&2
    exit 1
  else
    test "$?" = 73
  fi
  awk -v last="${target}" '{ print; if ($0 == last) exit }' "${work_dir}/expected" >"${work_dir}/prefix"
  cmp "${work_dir}/prefix" "${RELEASE_GATE_RECEIPT}"
  if grep -Fq 'release_verification_ok=true' "${work_dir}/failure.log"; then
    echo 'failed local verification claimed success' >&2
    exit 1
  fi
done

# A release invocation cannot quietly become a suite subset or resume range.
# The actual public Make target must reject these before calling any owner.
for args in '--suite source' '--from build' '--to test' '--list --from build'; do
  : >"${RELEASE_GATE_RECEIPT}"
  if command make --no-print-directory -s -C "${ROOT_DIR}" verify-release "ARGS=${args}" >"${work_dir}/invalid.log" 2>&1; then
    echo "release gate accepted partial options: ${args}" >&2
    exit 1
  fi
  test ! -s "${RELEASE_GATE_RECEIPT}"
done

echo 'release_verification_contract_ok=true'
