#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
resolver="${root}/scripts/release/helm-platform.sh"
helm_version=v3.18.6
amd64_sha256=3f43c0aa57243852dd542493a0f54f1396c0bc8ec7296bbb2c01e802010819ce
arm64_sha256=5b8e00b6709caab466cbbb0bc29ee09059b8dc9417991dd04b497530e49b1737

assert_platform() {
  local system="${1}"
  local machine="${2}"
  local expected="${3}"
  local actual
  actual="$(bash "${resolver}" "${system}" "${machine}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "Helm platform ${system}-${machine} resolved to ${actual}, want ${expected}" >&2
    exit 1
  fi
}

assert_platform Linux x86_64 "${helm_version} linux-amd64 ${amd64_sha256}"
assert_platform Linux amd64 "${helm_version} linux-amd64 ${amd64_sha256}"
assert_platform Linux aarch64 "${helm_version} linux-arm64 ${arm64_sha256}"
assert_platform Linux arm64 "${helm_version} linux-arm64 ${arm64_sha256}"

if bash "${resolver}" Darwin arm64 >/dev/null 2>&1; then
  echo "unsupported Helm platform Darwin-arm64 must fail" >&2
  exit 1
fi
if bash "${resolver}" Linux riscv64 >/dev/null 2>&1; then
  echo "unsupported Helm platform Linux-riscv64 must fail" >&2
  exit 1
fi

installer="${root}/scripts/release/install-helm.sh"
if ! grep -Fq 'scripts/release/helm-platform.sh' "${installer}"; then
  echo "Helm installer must consume the tested platform contract" >&2
  exit 1
fi

echo "helm_platform_contract_ok=true"
