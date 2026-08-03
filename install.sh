#!/bin/sh
set -eu

repository="cofy-x/axern"
install_dir="${AXERN_INSTALL_DIR:-${HOME}/.local/bin}"
version="${AXERN_VERSION:-}"

if [ -z "${version}" ]; then
  latest_url="$(curl -fsSIL -o /dev/null -w '%{url_effective}' "https://github.com/${repository}/releases/latest")"
  version="${latest_url##*/}"
fi
case "${version}" in
  v*) tag="${version}"; version="${version#v}" ;;
  *) tag="v${version}" ;;
esac

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) echo "axern: unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64) arch="amd64" ;;
  *) echo "axern: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

archive="axern_${version}_${os}_${arch}.tar.gz"
release_base="https://github.com/${repository}/releases/download/${tag}"
temporary="$(mktemp -d)"
cleanup() { rm -rf "${temporary}"; }
trap cleanup EXIT HUP INT TERM

curl -fsSL --retry 3 -o "${temporary}/${archive}" "${release_base}/${archive}"
curl -fsSL --retry 3 -o "${temporary}/checksums.txt" "${release_base}/checksums.txt"
expected="$(sed -n "s/[[:space:]][[:space:]]${archive}$/  ${archive}/p" "${temporary}/checksums.txt")"
if [ -z "${expected}" ]; then
  echo "axern: release checksum for ${archive} was not found" >&2
  exit 1
fi
(
  cd "${temporary}"
  if command -v shasum >/dev/null 2>&1; then
    printf '%s\n' "${expected}" | shasum -a 256 --check
  elif command -v sha256sum >/dev/null 2>&1; then
    printf '%s\n' "${expected}" | sha256sum --check --strict
  else
    echo "axern: shasum or sha256sum is required" >&2
    exit 1
  fi
  tar -xzf "${archive}"
)

mkdir -p "${install_dir}"
install -m 0755 "${temporary}/axern" "${install_dir}/axern"
"${install_dir}/axern" version >/dev/null
echo "Installed axern ${tag} to ${install_dir}/axern"
case ":${PATH}:" in
  *":${install_dir}:"*) ;;
  *) echo "Add ${install_dir} to PATH before running: axern local up" ;;
esac
