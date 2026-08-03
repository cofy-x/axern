#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dist="${AXERN_RELEASE_DIST:-${root}/dist/release}"
output="${1:?usage: homebrew-formula.sh OUTPUT}"
version="$(tr -d '[:space:]' < "${root}/VERSION")"
base="https://github.com/cofy-x/axern/releases/download/v${version}"

checksum() {
  awk -v file="$1" '$2 == file { print $1 }' "${dist}/checksums.txt"
}

darwin_arm="$(checksum "axern_${version}_darwin_arm64.tar.gz")"
darwin_amd="$(checksum "axern_${version}_darwin_amd64.tar.gz")"
linux_arm="$(checksum "axern_${version}_linux_arm64.tar.gz")"
linux_amd="$(checksum "axern_${version}_linux_amd64.tar.gz")"
for value in "${darwin_arm}" "${darwin_amd}" "${linux_arm}" "${linux_amd}"; do
  [[ -n "${value}" ]] || { echo "missing CLI archive checksum" >&2; exit 1; }
done

mkdir -p "$(dirname "${output}")"
sed \
  -e "s|@VERSION@|${version}|g" \
  -e "s|@BASE@|${base}|g" \
  -e "s|@DARWIN_ARM_SHA@|${darwin_arm}|g" \
  -e "s|@DARWIN_AMD_SHA@|${darwin_amd}|g" \
  -e "s|@LINUX_ARM_SHA@|${linux_arm}|g" \
  -e "s|@LINUX_AMD_SHA@|${linux_amd}|g" \
  "${root}/packaging/homebrew/axern.rb.tmpl" > "${output}"
