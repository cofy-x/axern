local_smoke_axern_bin() {
  local axern_bin="${AXERN_ROOT}/bin/axern"
  local go_bin
  go_bin="$(axern_go_bin)"
  if [ ! -x "${axern_bin}" ] || find "${AXERN_ROOT}/apps/cli" "${AXERN_ROOT}/sdk/go" -type f -newer "${axern_bin}" -print -quit | grep -q .; then
    (cd "${AXERN_ROOT}" && "${go_bin}" build -o "${axern_bin}" ./apps/cli)
  fi
  printf '%s\n' "${axern_bin}"
}

local_smoke_init_axern_cmd() {
  local env_name="$1"
  local endpoint="$2"
  local cli_env
  cli_env="$(cli_env_file "${env_name}")"
  # shellcheck disable=SC1090
  source "${cli_env}"
  local axern_bin
  axern_bin="$(local_smoke_axern_bin)"
  local axern_timeout="${LOCAL_SMOKE_AXERN_TIMEOUT:-20s}"
  AXERN_SMOKE_CMD=("${axern_bin}" "--context" "${env_name}" "--endpoint" "${endpoint}" "--timeout" "${axern_timeout}")
}

local_smoke_is_transient_grpc_error() {
  printf '%s' "$1" | grep -Eq 'DeadlineExceeded|context deadline exceeded|RST_STREAM|stream terminated|error reading server preface: EOF|connection error: desc = "error reading server preface: EOF"'
}

local_smoke_is_valid_json() {
  [ -n "$1" ] && python3 -c 'import json,sys; json.load(sys.stdin)' <<<"$1" >/dev/null 2>&1
}

local_smoke_retry_json() {
  local attempt rc stdout_file stderr_file stdout_body stderr_body
  stdout_file="$(mktemp)"
  stderr_file="$(mktemp)"
  rc=0
  for attempt in 1 2 3; do
    if "$@" >"${stdout_file}" 2>"${stderr_file}"; then
      stdout_body="$(cat "${stdout_file}")"
      stderr_body="$(cat "${stderr_file}")"
      if ! local_smoke_is_valid_json "${stdout_body}"; then
        if [ "${attempt}" -lt 3 ]; then
          sleep 2
          continue
        fi
        if [ -n "${stderr_body}" ]; then
          printf '%s\n' "${stderr_body}" >&2
        fi
        echo "smoke command returned invalid JSON after ${attempt} attempts: $*" >&2
        rm -f "${stdout_file}" "${stderr_file}"
        return 1
      fi
      cat "${stdout_file}"
      rm -f "${stdout_file}" "${stderr_file}"
      return 0
    fi
    rc=$?
    stdout_body="$(cat "${stdout_file}")"
    stderr_body="$(cat "${stderr_file}")"
    if [ "${attempt}" -lt 3 ] && local_smoke_is_transient_grpc_error "${stderr_body}"; then
      sleep 2
      continue
    fi
    if [ -n "${stdout_body}" ]; then
      printf '%s\n' "${stdout_body}" >&2
    fi
    if [ -n "${stderr_body}" ]; then
      printf '%s\n' "${stderr_body}" >&2
    fi
    rm -f "${stdout_file}" "${stderr_file}"
    return "${rc}"
  done
  rm -f "${stdout_file}" "${stderr_file}"
  return "${rc}"
}

local_smoke_recover_json_by_namespace() {
  local resource="$1"
  local list_key="$2"
  local item_key="$3"
  local target_namespace="$4"
  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" "${resource}" list -o json | python3 -c '
import json
import sys

list_key = sys.argv[1]
item_key = sys.argv[2]
target_namespace = sys.argv[3]

payload = json.load(sys.stdin)
items = [item for item in payload.get(list_key, []) if item.get("namespace") == target_namespace]
if not items:
    raise SystemExit(1)
items.sort(key=lambda item: (
    (item.get("created_at") or {}).get("seconds", 0),
    (item.get("created_at") or {}).get("nanos", 0),
))
print(json.dumps({item_key: items[-1]}))
' "${list_key}" "${item_key}" "${target_namespace}"
}

local_smoke_json_once() {
  local rc stdout_file stderr_file stdout_body stderr_body
  stdout_file="$(mktemp)"
  stderr_file="$(mktemp)"
  if "$@" >"${stdout_file}" 2>"${stderr_file}"; then
    stdout_body="$(cat "${stdout_file}")"
    stderr_body="$(cat "${stderr_file}")"
    if local_smoke_is_valid_json "${stdout_body}"; then
      cat "${stdout_file}"
      rm -f "${stdout_file}" "${stderr_file}"
      return 0
    fi
    if [ -n "${stderr_body}" ]; then
      printf '%s\n' "${stderr_body}" >&2
    fi
    echo "smoke command returned invalid JSON: $*" >&2
    rm -f "${stdout_file}" "${stderr_file}"
    return 1
  fi
  rc=$?
  stdout_body="$(cat "${stdout_file}")"
  stderr_body="$(cat "${stderr_file}")"
  if [ -n "${stdout_body}" ]; then
    printf '%s\n' "${stdout_body}" >&2
  fi
  if [ -n "${stderr_body}" ]; then
    printf '%s\n' "${stderr_body}" >&2
  fi
  rm -f "${stdout_file}" "${stderr_file}"
  return "${rc}"
}

local_smoke_json_once_or_recover_by_namespace() {
  local resource="$1"
  local list_key="$2"
  local item_key="$3"
  local target_namespace="$4"
  shift 4
  local rc stdout_file stderr_file stdout_body stderr_body recovered_json
  stdout_file="$(mktemp)"
  stderr_file="$(mktemp)"
  if "$@" >"${stdout_file}" 2>"${stderr_file}"; then
    stdout_body="$(cat "${stdout_file}")"
    stderr_body="$(cat "${stderr_file}")"
    if ! local_smoke_is_valid_json "${stdout_body}"; then
      if recovered_json="$(local_smoke_recover_json_by_namespace "${resource}" "${list_key}" "${item_key}" "${target_namespace}" 2>/dev/null)"; then
        printf '%s\n' "${recovered_json}"
        rm -f "${stdout_file}" "${stderr_file}"
        return 0
      fi
      if [ -n "${stderr_body}" ]; then
        printf '%s\n' "${stderr_body}" >&2
      fi
      echo "smoke create command returned invalid JSON and recovery found no ${resource} in namespace ${target_namespace}" >&2
      rm -f "${stdout_file}" "${stderr_file}"
      return 1
    fi
    cat "${stdout_file}"
    rm -f "${stdout_file}" "${stderr_file}"
    return 0
  fi
  rc=$?
  stdout_body="$(cat "${stdout_file}")"
  stderr_body="$(cat "${stderr_file}")"
  if local_smoke_is_transient_grpc_error "${stderr_body}"; then
    if recovered_json="$(local_smoke_recover_json_by_namespace "${resource}" "${list_key}" "${item_key}" "${target_namespace}" 2>/dev/null)"; then
      printf '%s\n' "${recovered_json}"
      rm -f "${stdout_file}" "${stderr_file}"
      return 0
    fi
  fi
  if [ -n "${stdout_body}" ]; then
    printf '%s\n' "${stdout_body}" >&2
  fi
  if [ -n "${stderr_body}" ]; then
    printf '%s\n' "${stderr_body}" >&2
  fi
  rm -f "${stdout_file}" "${stderr_file}"
  return "${rc}"
}

local_smoke_create_environment() {
  local namespace="$1"
  if [ -n "${LOCAL_SMOKE_RESOLVED_IMAGE_REF:-}" ]; then
    local_smoke_json_once_or_recover_by_namespace environment environments environment "${namespace}" \
      "${AXERN_SMOKE_CMD[@]}" environment create -o json --namespace "${namespace}" --image-ref "${LOCAL_SMOKE_RESOLVED_IMAGE_REF}"
  else
    local_smoke_json_once_or_recover_by_namespace environment environments environment "${namespace}" \
      "${AXERN_SMOKE_CMD[@]}" environment create -o json --namespace "${namespace}" --template-id python311
  fi
}

local_smoke_create_secret() {
  local namespace="$1"
  local_smoke_json_once_or_recover_by_namespace secret secrets secret "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" secret create -o json --namespace "${namespace}" --type opaque --literal token=hello-local
}

local_smoke_delete_service() {
  local service_id="$1"
  local deadline service_json status
  [ -n "${service_id}" ] || return 0

  local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service delete -o json "${service_id}" >/dev/null 2>&1 || true
  deadline=$((SECONDS + 120))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    if [ -z "${service_json}" ]; then
      return 0
    fi
    status="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["service"].get("status", ""))' <<<"${service_json}" 2>/dev/null || true)"
    if [ "${status}" = "deleted" ]; then
      if local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" admin service purge -o json \
        --operator-reason "automated smoke cleanup" "${service_id}" >/dev/null 2>&1; then
        return 0
      fi
    fi
    sleep 2
  done
  echo "service ${service_id} did not become purgeable before cleanup timeout" >&2
  return 1
}

local_smoke_assert_default_runtime_templates() {
  local catalog_json="$1"
  python3 -c '
import json
import sys

payload = json.load(sys.stdin)
required = {"python311", "server-base", "coding-base", "desktop-base"}
ids = {item.get("id") for item in payload.get("runtime_templates", [])}
missing = sorted(required - ids)
if missing:
    print(
        "runtime catalog missing templates: "
        + ", ".join(missing)
        + "; got: "
        + ", ".join(sorted(item for item in ids if item)),
        file=sys.stderr,
    )
    raise SystemExit(1)
' <<<"${catalog_json}"
}

local_smoke_assert_default_agent_bundles() {
  local catalog_json="$1"
  python3 -c '
import json
import sys

payload = json.load(sys.stdin)
required = {"claude-code", "codex"}
ids = {item.get("id") for item in payload.get("agent_bundles", [])}
missing = sorted(required - ids)
if missing:
    print(
        "agent bundle catalog missing bundles: "
        + ", ".join(missing)
        + "; got: "
        + ", ".join(sorted(item for item in ids if item)),
        file=sys.stderr,
    )
    raise SystemExit(1)
' <<<"${catalog_json}"
}

local_smoke_wait_for_run_status() {
  local run_id="$1"
  local expected_status="$2"
  local deadline=$((SECONDS + 120))
  local run_json=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    run_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run get -o json "${run_id}" 2>/dev/null || true)"
    if [ -z "${run_json}" ]; then
      sleep 2
      continue
    fi
    if python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data["run"]["status"] == sys.argv[1] else 1)' "${expected_status}" <<<"${run_json}"; then
      printf '%s\n' "${run_json}"
      return 0
    fi
    sleep 2
  done
  printf '%s\n' "${run_json}"
  return 1
}

local_smoke_wait_for_run_terminal() {
  local run_id="$1"
  local deadline=$((SECONDS + 120))
  local run_json=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    run_json="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" run get -o json "${run_id}" 2>/dev/null || true)"
    if [ -z "${run_json}" ]; then
      sleep 2
      continue
    fi
    if python3 -c '
import json
import sys

status = json.load(sys.stdin)["run"]["status"]
sys.exit(0 if status in {"cancelled", "failed", "succeeded"} else 1)
' <<<"${run_json}"; then
      printf '%s\n' "${run_json}"
      return 0
    fi
    sleep 2
  done
  printf '%s\n' "${run_json}"
  return 1
}
