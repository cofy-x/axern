#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
fixture_dir=""
node_fixture_dir=""
node_container=""
cleanup() {
  if [ -n "${node_fixture_dir}" ] && [ -n "${node_container}" ]; then
    docker exec "${node_container}" bash -c '
      if [ -f "$1/pid" ]; then kill "$(< "$1/pid")" 2>/dev/null || true; fi
      rm -f "$1/pid" "$1/cert.pem" "$1/key.pem"
      rmdir "$1"
    ' bash "${node_fixture_dir}" >/dev/null 2>&1 || true
  fi
  if [ -n "${fixture_dir}" ]; then
    rm -f "${fixture_dir}/ca.pem"
    rmdir "${fixture_dir}" || true
  fi
  end_env_lock compose
}
trap cleanup EXIT

require_cmd docker
require_cmd uv

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose
local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"
resolved_image="${LOCAL_SMOKE_RESOLVED_IMAGE_REF:-$(local_smoke_prepare_compose_image)}"

node_container="${COMPOSE_PROJECT_NAME}-node-1"
if ! docker ps --format '{{.Names}}' | grep -qx "${node_container}"; then
  echo "missing compose node container ${node_container}; run make local-compose-up first" >&2
  exit 1
fi

uv run --package axern-sdk python "${AXERN_ROOT}/sdk/python/tests/e2e/sandbox_tunnel_e2e.py" \
  --endpoint "${AXERN_ENDPOINT}" \
  --tls-ca-cert "${AXERN_TLS_CA_CERT}" \
  --tls-cert "${AXERN_TLS_CERT}" \
  --tls-key "${AXERN_TLS_KEY}" \
  --image-ref "${resolved_image}" \
  --node-container "${node_container}"

# This source Compose stack intentionally uses a hermetic DNS fixture and does
# not promise public Internet access. Serve real TLS/HTTP on the node bridge:
# unrestricted sandboxes can reach it, while an isolated sandbox cannot.
fixture_ip="$(docker exec "${node_container}" bash -c "ip -4 -o addr show dev sandbox0 | awk '{print \$4}' | cut -d/ -f1")"
if [ -z "${fixture_ip}" ]; then
  echo "compose node has no sandbox bridge address" >&2
  exit 1
fi
fixture_dir="$(mktemp -d)"
node_fixture_dir="$(docker exec "${node_container}" mktemp -d /tmp/axern-egress-fixture.XXXXXX)"
docker exec "${node_container}" openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -subj "/CN=axern-egress-fixture" -addext "subjectAltName=IP:${fixture_ip}" \
  -keyout "${node_fixture_dir}/key.pem" -out "${node_fixture_dir}/cert.pem" >/dev/null 2>&1
docker cp "${node_container}:${node_fixture_dir}/cert.pem" "${fixture_dir}/ca.pem" >/dev/null
docker exec -d "${node_container}" bash -c '
  echo "$$" > "$1/pid"
  exec openssl s_server -accept "$2:28443" -cert "$1/cert.pem" -key "$1/key.pem" -www -quiet
' bash "${node_fixture_dir}" "${fixture_ip}"
fixture_ready=false
for _ in $(seq 1 30); do
  if docker exec "${node_container}" curl --noproxy '*' --silent --show-error --fail \
    --cacert "${node_fixture_dir}/cert.pem" "https://${fixture_ip}:28443/" >/dev/null 2>&1; then
    fixture_ready=true
    break
  fi
  sleep 0.2
done
if [ "${fixture_ready}" != true ]; then
  echo "compose HTTPS egress fixture did not become ready" >&2
  exit 1
fi

uv run --package axern-sdk python "${AXERN_ROOT}/sdk/python/tests/e2e/network_policy_e2e.py" \
  --endpoint "${AXERN_ENDPOINT}" \
  --tls-ca-cert "${AXERN_TLS_CA_CERT}" \
  --tls-cert "${AXERN_TLS_CERT}" \
  --tls-key "${AXERN_TLS_KEY}" \
  --image-ref "${resolved_image}" \
  --https-url "https://${fixture_ip}:28443/" \
  --tls-ca-pem-file "${fixture_dir}/ca.pem" \
  --direct-tcp-host "${fixture_ip}" \
  --direct-tcp-port 28443

echo "compose_python_sdk_e2e_ok=true"
