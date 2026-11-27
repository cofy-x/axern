#!/usr/bin/env bash
set -euo pipefail

destination="$(realpath -m "${1:?usage: install-image-auditors.sh BIN_DIRECTORY}")"
if [[ "$(uname -s)/$(uname -m)" != Linux/x86_64 ]]; then
  echo "The delivery image auditors require Linux amd64." >&2
  exit 1
fi
mkdir -p "${destination}"
download_dir="$(mktemp -d)"
trap 'rm -rf "${download_dir}"' EXIT

install_auditor() {
  local name="$1" version="$2" checksum="$3" archive
  archive="${download_dir}/${name}_${version}_linux_amd64.tar.gz"
  curl --proto '=https' --proto-redir '=https' --fail --location --retry 3 \
    --output "${archive}" "https://github.com/anchore/${name}/releases/download/v${version}/${archive##*/}"
  printf '%s  %s\n' "${checksum}" "${archive}" | sha256sum --check --strict
  tar -xzf "${archive}" -C "${download_dir}" "${name}"
  install -m 0755 "${download_dir}/${name}" "${destination}/${name}"
}

install_auditor syft 1.57.2 ad92bd3ec1b94a3df01d4d0aec199afa9c42b72e86da436ff5f837914d32706b
install_auditor grype 0.122.1 f1ba7e2917ee1395618de20a98da0aa7c0fb1788927797651047c50502a37538
