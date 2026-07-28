#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/release/images.sh"

destination="${1:-${AXERN_INSTALL_DIR:-${AXERN_ROOT}/bin}/axern}"
tag="$(axern_release_version)"
version="${tag#v}"
case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

archive="axern_${version}_${os}_${arch}.tar.gz"
release_base="${AXERN_RELEASE_DOWNLOAD_BASE:-https://github.com/cofy-x/axern/releases/download/${tag}}"
tmp_dir="$(mktemp -d)"
cleanup() { rm -rf "${tmp_dir}"; }
trap cleanup EXIT

curl --fail --location --retry 3 --output "${tmp_dir}/${archive}" "${release_base}/${archive}"
curl --fail --location --retry 3 --output "${tmp_dir}/checksums.txt" "${release_base}/checksums.txt"
(
  cd "${tmp_dir}"
  grep "  ${archive}$" checksums.txt | shasum -a 256 --check
  tar -xzf "${archive}"
)
mkdir -p "$(dirname "${destination}")"
install -m 0755 "${tmp_dir}/axern" "${destination}"
echo "axern_cli_installed=${destination}"
echo "axern_cli_version=${tag}"
