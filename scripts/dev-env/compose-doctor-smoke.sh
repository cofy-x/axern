#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
begin_env_lock compose
trap 'end_env_lock compose' EXIT

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose
LOCAL_SMOKE_AXERN_TIMEOUT="${LOCAL_SMOKE_AXERN_TIMEOUT:-6m}"
local_smoke_init_axern_cmd compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"
namespace="compose-doctor-smoke-$(date +%s)-$$"

cleanup() {
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace delete "${namespace}" --output json >/dev/null 2>&1 || true
  end_env_lock compose
}
trap cleanup EXIT

local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" namespace create "${namespace}" --output json >/dev/null

if ! report="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" doctor \
    --namespace "${namespace}" \
    --probe \
    --probe-timeout 5m \
    --output json)"; then
  exit 1
fi
python3 -c '
import json
import sys

report = json.load(sys.stdin)
if report.get("status") != "healthy" or report.get("mode") != "probe":
    raise SystemExit(f"unexpected doctor report: {report}")
checks = {item.get("name"): item for item in report.get("checks", [])}
probe = checks.get("data_plane", {})
if probe.get("status") != "pass" or probe.get("code") != "probe_succeeded":
    raise SystemExit(f"data-plane doctor check did not pass: {probe}")
' <<<"${report}"

environments="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" environment list --output json)"
python3 -c '
import json
import sys

payload = json.load(sys.stdin)
active = [
    item for item in payload.get("environments", [])
    if item.get("labels", {}).get("axern.doctor") == "probe"
    and str(item.get("status", "")).lower() not in {"deleted", "environment_status_deleted"}
]
if active:
    raise SystemExit(f"doctor left active probe environments: {active}")
' <<<"${environments}"

echo "compose_doctor_smoke_ok=true"
