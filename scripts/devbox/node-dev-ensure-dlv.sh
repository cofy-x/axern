#!/usr/bin/env bash
set -euo pipefail

LOCK_FILE="${1:-}"
DLV_VERSION="${DLV_VERSION:-latest}"
DLV_BIN="${DLV_BIN:-/usr/local/bin/dlv}"

install_dlv_bin() {
  local source_bin="$1"
  if [ -w "$(dirname "${DLV_BIN}")" ]; then
    install -m 0755 "${source_bin}" "${DLV_BIN}"
  else
    sudo -n install -m 0755 "${source_bin}" "${DLV_BIN}"
  fi
}

if [ -x "${DLV_BIN}" ]; then
  exit 0
fi

install_dlv() {
  local tmpdir
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "${tmpdir}"' RETURN

  export GOTOOLCHAIN=local
  GOFLAGS= GOBIN="${tmpdir}" go install "github.com/go-delve/delve/cmd/dlv@${DLV_VERSION}"
  install_dlv_bin "${tmpdir}/dlv"
}

if [ -n "${LOCK_FILE}" ] && command -v flock >/dev/null 2>&1; then
  mkdir -p "$(dirname "${LOCK_FILE}")"
  export DLV_BIN DLV_VERSION
  flock "${LOCK_FILE}" bash -lc '
    set -euo pipefail
    if [ -x "${DLV_BIN}" ]; then
      exit 0
    fi

    existing_dlv="$(command -v dlv || true)"
    if [ -n "${existing_dlv}" ]; then
      if [ -w "$(dirname "${DLV_BIN}")" ]; then
        install -m 0755 "${existing_dlv}" "${DLV_BIN}"
      else
        sudo -n install -m 0755 "${existing_dlv}" "${DLV_BIN}"
      fi
      exit 0
    fi

    tmpdir="$(mktemp -d)"
    trap '\''rm -rf "${tmpdir}"'\'' EXIT

    export GOTOOLCHAIN=local
    GOFLAGS= GOBIN="${tmpdir}" go install "github.com/go-delve/delve/cmd/dlv@${DLV_VERSION}"

    if [ -w "$(dirname "${DLV_BIN}")" ]; then
      install -m 0755 "${tmpdir}/dlv" "${DLV_BIN}"
    else
      sudo -n install -m 0755 "${tmpdir}/dlv" "${DLV_BIN}"
    fi
  '
else
  existing_dlv="$(command -v dlv || true)"
  if [ -n "${existing_dlv}" ]; then
    install_dlv_bin "${existing_dlv}"
  else
    install_dlv
  fi
fi
