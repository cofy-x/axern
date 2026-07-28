#!/usr/bin/env bash
set -euo pipefail

destination="${1:?usage: $0 <destination>}"
version=v3.18.6
archive_sha256=3f43c0aa57243852dd542493a0f54f1396c0bc8ec7296bbb2c01e802010819ce
case "$(uname -s)-$(uname -m)" in
  Linux-x86_64|Linux-amd64) platform=linux-amd64 ;;
  *) echo "the release workflow Helm installer supports Linux amd64" >&2; exit 1 ;;
esac

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
