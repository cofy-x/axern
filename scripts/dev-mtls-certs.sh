#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-${ROOT_DIR}/.dev/certs}"
cd "${ROOT_DIR}"
go run ./control/controld/cmd/workload-pki \
  -directory "${OUT_DIR}" \
  -cluster "${AXERN_WORKLOAD_CLUSTER:-axern.local}" \
  -dns "${AXERN_TLS_SERVER_DNS_NAMES:-localhost,host.docker.internal,controld,gatewayd,tunneld,registry}" \
  -ips "${AXERN_TLS_SERVER_IPS:-127.0.0.1}"
printf 'dev_mtls_certs_dir=%s\n' "${OUT_DIR}"
