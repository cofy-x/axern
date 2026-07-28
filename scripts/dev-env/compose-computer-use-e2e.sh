#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

begin_env_lock compose
trap 'end_env_lock compose' EXIT

require_cmd docker
require_cmd uv

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose
local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"
if ! desktop_template_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" catalog get desktop-base -o json)"; then
  echo "desktop-base catalog template is unavailable; run make local-compose-refresh" >&2
  exit 1
fi
python3 - "${DESKTOP_BASE_RUNTIME_IMAGE}" "${desktop_template_json}" <<'PY'
import json
import sys

expected_ref = sys.argv[1]
payload = json.loads(sys.argv[2])
template = payload.get("runtime_template") or payload.get("runtimeTemplate") or {}
descriptor = template.get("image_descriptor") or template.get("imageDescriptor") or {}
annotations = descriptor.get("annotations") or {}
actual_ref = annotations.get("org.opencontainers.image.ref.name", "")
if actual_ref != expected_ref:
    print(
        "desktop-base catalog image mismatch: "
        f"got {actual_ref!r}, want {expected_ref!r}; run make local-compose-refresh",
        file=sys.stderr,
    )
    raise SystemExit(1)
PY

node_container="${COMPOSE_PROJECT_NAME}-node-1"
if ! docker ps --format '{{.Names}}' | grep -qx "${node_container}"; then
  echo "missing compose node container ${node_container}; run make local-compose-up first" >&2
  exit 1
fi

if ! docker image inspect "${DESKTOP_BASE_RUNTIME_IMAGE}" >/dev/null 2>&1; then
  echo "missing desktop runtime image ${DESKTOP_BASE_RUNTIME_IMAGE}; run make local-images-build first" >&2
  exit 1
fi
IMAGE="${DESKTOP_BASE_RUNTIME_IMAGE}" bash "${AXERN_ROOT}/scripts/dev-env/compose-image-import.sh"

runtime_list="${AXERN_COMPUTER_USE_E2E_RUNTIMES:-runsc}"
for runtime_class in ${runtime_list}; do
  echo "node_computer_use_e2e_runtime=${runtime_class} phase=start"
  uv run --package axern-sdk python "${AXERN_ROOT}/sdk/python/tests/e2e/computer_use_e2e.py" \
    --endpoint "${AXERN_ENDPOINT}" \
    --tls-ca-cert "${AXERN_TLS_CA_CERT}" \
	    --tls-cert "${AXERN_TLS_CERT}" \
	    --tls-key "${AXERN_TLS_KEY}" \
	    --runtime-class "${runtime_class}" \
	    --node-container "${node_container}"
done

echo "compose_computer_use_e2e_ok=true"
