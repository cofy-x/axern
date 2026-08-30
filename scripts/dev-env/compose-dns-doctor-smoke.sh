#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
set -Eeuo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker
require_cmd python3
begin_env_lock compose

verify_root=""
cleanup() {
  if [ -n "${verify_root}" ] && [ -d "${verify_root}" ] && [[ "$(basename "${verify_root}")" == axern-compose-dns-doctor.* ]]; then
    rm -rf -- "${verify_root}"
  fi
  end_env_lock compose
}
trap cleanup EXIT

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" compose

query_name="fixture.axern.test."
configured_nameservers="$(awk -F= '$1 == "AXNODED_DNS_NAMESERVERS" { print $2; exit }' "$(compose_env_file)")"
if [ -z "${configured_nameservers}" ]; then
  echo "Compose DNS fixture resolver is not materialized" >&2
  exit 1
fi
compose_file="${DEPLOY_ROOT}/compose/docker-compose.yml"
compose_args=(--project-name "${COMPOSE_PROJECT_NAME}" --env-file "$(compose_env_file)" -f "${compose_file}")
verify_root="$(mktemp -d "${TMPDIR:-/tmp}/axern-compose-dns-doctor.XXXXXX")"
local_dir="${verify_root}/local"
config_file="${verify_root}/config.json"
mkdir -p "${local_dir}"

node_result="$(docker compose "${compose_args[@]}" exec -T \
  -e "AXERN_DNS_PROBE_NAME=${query_name}" \
  -e "AXERN_DNS_PROBE_TIMEOUT=15s" \
  node /usr/local/libexec/axnoded/dns-probe)"
printf '%s\n' "${node_result}" >"${verify_root}/node-result.json"
python3 - "${verify_root}/node-result.json" "${configured_nameservers}" <<'PY'
import ipaddress
import json
import pathlib
import sys

report = json.loads(pathlib.Path(sys.argv[1]).read_text())
configured = [str(ipaddress.ip_address(item.strip())) for item in sys.argv[2].split(",") if item.strip()]
if report.get("status") != "pass" or report.get("code") != "runtime_dns_node_reachable":
    raise SystemExit(f"unexpected Node DNS probe result: {report}")
effective = report.get("effective_resolver_count", 0)
if effective <= 0 or report.get("successful_resolver_count") != effective:
    raise SystemExit(f"Node DNS probe did not verify every effective resolver: {report}")
if effective != len(configured):
    raise SystemExit(f"Node DNS probe did not use the hermetic fixture: {report}")
PY

cp "${compose_file}" "${local_dir}/compose.yaml"
cp "$(compose_env_file)" "${local_dir}/compose.env"
ln -s "${COMPOSE_STATE_DIR}/certs" "${local_dir}/certs"
ln -s "${COMPOSE_STATE_DIR}/ssh" "${local_dir}/ssh"

axern_bin="$(local_smoke_axern_bin)"
axern_version="$("${axern_bin}" version)"
python3 - "${local_dir}/metadata.json" "${axern_version}" <<'PY'
import datetime
import json
import pathlib
import sys

now = datetime.datetime.now(datetime.timezone.utc).isoformat().replace("+00:00", "Z")
pathlib.Path(sys.argv[1]).write_text(json.dumps({
    "version": sys.argv[2],
    "created_at": now,
    "updated_at": now,
}, indent=2) + "\n")
PY

AXERN_CONFIG="${config_file}" upsert_axern_context \
  local "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}" "${COMPOSE_STATE_DIR}/certs" true \
  "http://127.0.0.1:${COMPOSE_GATEWAY_HTTP_PORT}" "127.0.0.1:${COMPOSE_GATEWAY_SSH_PORT}" \
  "${COMPOSE_STATE_DIR}/ssh/gateway_client_ed25519"

local_doctor_cmd=(env \
  -u AXERN_CONTEXT \
  -u AXERN_ENDPOINT \
  -u AXERN_TLS_CA_CERT \
  -u AXERN_TLS_CERT \
  -u AXERN_TLS_KEY \
  -u AXERN_TLS_SERVER_NAME \
  -u AXERN_PROXY_MODE \
  AXERN_HOME="${verify_root}" \
  AXERN_CONFIG="${config_file}" \
  "${axern_bin}" --config "${config_file}" --timeout 6m)

read_report_file="${verify_root}/read-report.json"
probe_report_file="${verify_root}/probe-report.json"
if ! "${local_doctor_cmd[@]}" local doctor --dns-query-name "${query_name}" --output json >"${read_report_file}"; then
  python3 -m json.tool <"${read_report_file}" >&2 || true
  echo "read-only local DNS doctor did not exit successfully" >&2
  exit 1
fi
if ! "${local_doctor_cmd[@]}" local doctor --probe --dns-query-name "${query_name}" --probe-timeout 5m --output json >"${probe_report_file}"; then
  python3 -m json.tool <"${probe_report_file}" >&2 || true
  echo "sandbox local DNS doctor did not exit successfully" >&2
  exit 1
fi

python3 - "${read_report_file}" "${probe_report_file}" "${query_name}" "${configured_nameservers}" <<'PY'
import json
import pathlib
import sys

read_report = json.loads(pathlib.Path(sys.argv[1]).read_text())
probe_report = json.loads(pathlib.Path(sys.argv[2]).read_text())
query_name = sys.argv[3]
resolvers = [item.strip() for item in sys.argv[4].split(",") if item.strip()]

def check(report, mode, sandbox_status, sandbox_code):
    if report.get("status") != "healthy" or report.get("mode") != mode:
        raise SystemExit(f"unexpected local doctor report status: {report}")
    checks = {item.get("name"): item for item in report.get("checks", [])}
    expected = {
        "runtime_dns_config": ("pass", "runtime_dns_config_valid"),
        "runtime_dns_node": ("pass", "runtime_dns_node_reachable"),
        "runtime_dns_sandbox": (sandbox_status, sandbox_code),
    }
    for name, (status, code) in expected.items():
        item = checks.get(name, {})
        if item.get("status") != status or item.get("code") != code:
            raise SystemExit(f"unexpected {name} result: {item}")
    serialized = json.dumps(report, sort_keys=True)
    for sensitive in [query_name, *resolvers]:
        if sensitive and sensitive in serialized:
            raise SystemExit("local doctor JSON exposed DNS verification input")

check(read_report, "read_only", "skip", "runtime_dns_sandbox_skipped")
check(probe_report, "probe", "pass", "runtime_dns_sandbox_resolved")
PY

table_output="$("${local_doctor_cmd[@]}" local doctor --dns-query-name "${query_name}")"
for required in CODE LATENCY runtime_dns_config_valid runtime_dns_node_reachable; do
  if ! grep -Fq "${required}" <<<"${table_output}"; then
    echo "local doctor table is missing ${required}" >&2
    exit 1
  fi
done

"${local_doctor_cmd[@]}" namespace list --output json >"${verify_root}/namespaces.json"
"${local_doctor_cmd[@]}" environment list --output json >"${verify_root}/environments.json"
"${local_doctor_cmd[@]}" secret list --output json >"${verify_root}/secrets.json"
python3 - "${verify_root}/namespaces.json" "${verify_root}/environments.json" "${verify_root}/secrets.json" <<'PY'
import json
import pathlib
import sys

namespaces = json.loads(pathlib.Path(sys.argv[1]).read_text()).get("namespaces", [])
environments = json.loads(pathlib.Path(sys.argv[2]).read_text()).get("environments", [])
secrets = json.loads(pathlib.Path(sys.argv[3]).read_text()).get("secrets", [])

if any((item.get("namespace") or "").startswith("axern-doctor-dns-") for item in namespaces):
    raise SystemExit("sandbox DNS doctor left a temporary Namespace")
if any(item.get("labels", {}).get("axern.doctor") == "local-dns" and item.get("status") != "deleted" for item in environments):
    raise SystemExit("sandbox DNS doctor left an active Environment")
if any(item.get("labels", {}).get("axern.doctor") == "local-dns" for item in secrets):
    raise SystemExit("sandbox DNS doctor left a temporary Secret")
PY

set +e
"${local_doctor_cmd[@]}" local doctor --dns-query-name invalid_name --output json >/dev/null 2>&1
usage_exit=$?
set -e
if [ "${usage_exit}" -ne 2 ]; then
  echo "invalid local doctor arguments exited ${usage_exit}, want 2" >&2
  exit 1
fi

python3 - "${local_dir}/compose.env" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
lines = path.read_text().splitlines()
updated = []
found = False
for line in lines:
    if line.startswith("AXNODED_DNS_NAMESERVERS="):
        updated.append("AXNODED_DNS_NAMESERVERS=127.0.0.1")
        found = True
    else:
        updated.append(line)
if not found:
    updated.append("AXNODED_DNS_NAMESERVERS=127.0.0.1")
path.write_text("\n".join(updated) + "\n")
PY

failed_report_file="${verify_root}/failed-report.json"
set +e
"${local_doctor_cmd[@]}" local doctor --dns-query-name "${query_name}" --output json >"${failed_report_file}"
failed_exit=$?
set -e
if [ "${failed_exit}" -ne 3 ]; then
  echo "required local doctor failure exited ${failed_exit}, want 3" >&2
  exit 1
fi
python3 - "${failed_report_file}" <<'PY'
import json
import pathlib
import sys

report = json.loads(pathlib.Path(sys.argv[1]).read_text())
checks = {item.get("name"): item for item in report.get("checks", [])}
if report.get("status") != "failed":
    raise SystemExit(f"required failure did not fold to failed: {report}")
if checks.get("runtime_dns_config", {}).get("code") != "runtime_dns_config_invalid":
    raise SystemExit(f"configuration failure code is unstable: {checks.get('runtime_dns_config')}")
if checks.get("runtime_dns_node", {}).get("code") != "runtime_dns_node_skipped":
    raise SystemExit(f"Node dependency was not skipped: {checks.get('runtime_dns_node')}")
if checks.get("runtime_dns_sandbox", {}).get("code") != "runtime_dns_sandbox_skipped":
    raise SystemExit(f"sandbox dependency was not skipped: {checks.get('runtime_dns_sandbox')}")
PY

echo "compose_dns_doctor_smoke_ok=true"
