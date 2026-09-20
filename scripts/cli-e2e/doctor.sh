#!/usr/bin/env bash

verify_doctor() {
  local environments namespace report
  namespace="doctor-e2e"
  # shellcheck disable=SC2154 # Defined by the shared CLI E2E environment.
  "${AXERN_BIN}" --config "${cli_config_file}" namespace create "${namespace}" --output json >/dev/null
  if ! report="$("${AXERN_BIN}" --config "${cli_config_file}" doctor \
      --namespace "${namespace}" \
      --probe \
      --image "${PYTHON_RUNTIME_IMAGE_REF}" \
      --probe-timeout 5m \
      --output json)"; then
    echo "doctor probe failed:" >&2
    printf '%s\n' "${report}" >&2
    dump_logs
    return 1
  fi
  python3 -c '
import json
import sys

report = json.load(sys.stdin)
if report.get("status") != "healthy" or report.get("mode") != "probe":
    raise SystemExit(f"unexpected doctor report: {report}")
namespace = report.get("namespace")
if namespace != "doctor-e2e":
    raise SystemExit(f"unexpected doctor namespace: {namespace}")
checks = {item.get("name"): item for item in report.get("checks", [])}
expected = {
    "configuration", "tls_material", "tls_expiry", "tls_key_permissions",
    "gateway", "identity", "authorization", "namespace", "data_plane",
}
if set(checks) != expected:
    raise SystemExit(f"unexpected doctor checks: {sorted(checks)}")
if any(item.get("status") != "pass" for item in checks.values()):
    raise SystemExit(f"doctor check did not pass: {checks}")
probe = checks["data_plane"]
if probe.get("code") != "probe_succeeded":
    raise SystemExit(f"data-plane probe code is invalid: {probe}")
' <<<"${report}"

  environments="$("${AXERN_BIN}" --config "${cli_config_file}" environment list --output json)"
  python3 -c '
import json
import sys

payload = json.load(sys.stdin)
active = [
    item for item in payload.get("environments", [])
    if item.get("labels", {}).get("axern.doctor") == "probe"
]
if active:
    raise SystemExit(f"doctor left active probe environments: {active}")
' <<<"${environments}"
}
