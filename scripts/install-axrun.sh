#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "missing required command: $1" >&2
    exit 1
  }
}

require_cmd go

gobin="$(go env GOBIN)"
if [ -z "${gobin}" ]; then
  gopath="$(go env GOPATH)"
  gobin="${gopath}/bin"
fi

mkdir -p "${gobin}"
(cd "${AXERN_ROOT}" && go build -o "${gobin}/axrun" ./apps/axrun)

echo "axrun_install_ok=true"
echo "installed_path=${gobin}/axrun"

case ":${PATH}:" in
  *:"${gobin}":*)
    ;;
  *)
    echo "warning=GOBIN is not on PATH (${gobin})" >&2
    ;;
esac
