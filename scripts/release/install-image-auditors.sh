#!/usr/bin/env bash
set -euo pipefail

destination="$(realpath -m "${1:?usage: install-image-auditors.sh BIN_DIRECTORY [syft]}")"
selection="${2-all}"
case "$(uname -s)/$(uname -m)/${selection}" in
  Linux/x86_64/all|Linux/x86_64/syft)
    arch=amd64
    syft_sha256=ad92bd3ec1b94a3df01d4d0aec199afa9c42b72e86da436ff5f837914d32706b
    ;;
  Linux/aarch64/syft)
    arch=arm64
    syft_sha256=8efe605309c98798e1ee0cffdcf8c05ca26819b4cd44c0b03650f1e963e9bdb6
    ;;
  *)
    echo "Use Linux amd64 for the image auditors, or Linux amd64/arm64 for Syft alone." >&2
    exit 1
    ;;
esac
mkdir -p "${destination}"
download_dir="$(mktemp -d)"
trap 'rm -rf "${download_dir}"' EXIT

install_auditor() {
  local name="$1" version="$2" checksum="$3" archive
  archive="${download_dir}/${name}_${version}_linux_${arch}.tar.gz"
  curl --proto '=https' --proto-redir '=https' --fail --location --retry 3 \
    --output "${archive}" "https://github.com/anchore/${name}/releases/download/v${version}/${archive##*/}"
  printf '%s  %s\n' "${checksum}" "${archive}" | sha256sum --check --strict
  tar -xzf "${archive}" -C "${download_dir}" "${name}"
  install -m 0755 "${download_dir}/${name}" "${destination}/${name}"
}

install_auditor syft 1.57.2 "${syft_sha256}"
if [[ "${selection}" == all ]]; then
  install_auditor grype 0.122.1 f1ba7e2917ee1395618de20a98da0aa7c0fb1788927797651047c50502a37538
fi
