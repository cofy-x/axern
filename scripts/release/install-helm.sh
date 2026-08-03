#!/usr/bin/env bash
set -euo pipefail

destination="${1:?usage: $0 <destination>}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
read -r version platform archive_sha256 < <(
  bash "${root}/scripts/release/helm-platform.sh" "$(uname -s)" "$(uname -m)"
)

tmp_dir="$(mktemp -d)"
cleanup() { rm -rf "${tmp_dir}"; }
trap cleanup EXIT
archive="helm-${version}-${platform}.tar.gz"
curl --fail --location --retry 3 --output "${tmp_dir}/${archive}" \
  "https://get.helm.sh/${archive}"
printf '%s  %s\n' "${archive_sha256}" "${tmp_dir}/${archive}" | sha256sum --check --strict
tar -xzf "${tmp_dir}/${archive}" -C "${tmp_dir}"
mkdir -p "$(dirname "${destination}")"
install -m 0755 "${tmp_dir}/${platform}/helm" "${destination}"
echo "helm_installed=${destination}"
echo "helm_version=${version}"
