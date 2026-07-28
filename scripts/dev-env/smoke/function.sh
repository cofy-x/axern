run_local_function_smoke() {
  local env_name="$1"
  local endpoint="$2"
  local service_prefix="$3"
  local cli_env
  cli_env="$(cli_env_file "${env_name}")"
  # shellcheck disable=SC1090
  source "${cli_env}"
  local axern_bin
  axern_bin="$(local_smoke_axern_bin)"
  local axern_timeout="${LOCAL_SMOKE_AXERN_TIMEOUT:-30s}"
  local -a axern_cmd=("${axern_bin}" "--endpoint" "${endpoint}" "--timeout" "${axern_timeout}")
  local namespace="${service_prefix}-${env_name}-fn-smoke-$(date +%s)"
  local fn_name="smoke-hello"
  local function_id=""
  local fn_dir=""

  cleanup_local_function_smoke() {
    local rc=$?
    if [ -n "${function_id:-}" ]; then
      local_smoke_retry_json "${axern_cmd[@]}" fn delete -o json "${function_id}" >/dev/null 2>&1 || true
      function_id=""
    fi
    if [ -n "${fn_dir:-}" ]; then
      rm -rf "${fn_dir}"
      fn_dir=""
    fi
    return "${rc}"
  }
  trap cleanup_local_function_smoke RETURN

  fn_dir="$(mktemp -d -t axern-fn-smoke-XXXX)"
  _write_smoke_function "${fn_dir}" "${fn_name}" "${namespace}"

  echo "${service_prefix}_function_smoke_phase=deploy" >&2
  local deploy_json
  deploy_json="$(local_smoke_retry_json "${axern_cmd[@]}" function deploy -o json --file "${fn_dir}/function.yaml")"
  function_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["function"]["id"])' <<<"${deploy_json}")"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data["function"]["id"], "missing function_id"' <<<"${deploy_json}" >/dev/null

  echo "${service_prefix}_function_smoke_phase=wait_ready" >&2
  local fn_get deadline
  deadline=$((SECONDS + 180))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    fn_get="$(local_smoke_retry_json "${axern_cmd[@]}" fn get -o json "${function_id}" 2>/dev/null || true)"
    if [ -z "${fn_get}" ]; then
      sleep 2
      continue
    fi
    if python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if d.get("deployment",{}).get("status") == "ready" and d.get("deployment",{}).get("ready_replicas",0) > 0 else 1)' <<<"${fn_get}"; then
      break
    fi
    sleep 2
  done
  python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("deployment",{}).get("status") == "ready", f"function not ready: {d}"' <<<"${fn_get}" >/dev/null

  echo "${service_prefix}_function_smoke_phase=invoke" >&2
  local invoke_json
  invoke_json="$(local_smoke_retry_json "${axern_cmd[@]}" fn invoke -o json --namespace "${namespace}" "${fn_name}" -d '{"name":"compose-smoke"}')"
  python3 -c '
import json, sys
data = json.load(sys.stdin)
status = data.get("status", "")
assert status == "succeeded", f"invocation not succeeded: {status}"
result_data = data.get("result", {}).get("data", "")
if result_data:
    decoded = json.loads(result_data)
    assert "compose-smoke" in str(decoded), f"unexpected result: {decoded}"
' <<<"${invoke_json}" >/dev/null

  echo "${service_prefix}_function_smoke_phase=get_invocation" >&2
  local sync_inv_id
  sync_inv_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${invoke_json}")"
  local get_inv_json
  get_inv_json="$(local_smoke_retry_json "${axern_cmd[@]}" function invocation get -o json "${sync_inv_id}")"
  python3 -c '
import json, sys
data = json.load(sys.stdin)
assert data.get("id") != "", "missing invocation id"
inv_status = data.get("status", "")
assert inv_status == "succeeded", "expected succeeded: " + inv_status
' <<<"${get_inv_json}" >/dev/null

  echo "${service_prefix}_function_smoke_phase=async_invoke" >&2
  local async_json
  async_json="$(local_smoke_retry_json "${axern_cmd[@]}" fn invoke -o json --namespace "${namespace}" "${fn_name}" -d '{"name":"async-test"}' --async)"
  python3 -c '
import json, sys
data = json.load(sys.stdin)
status = data.get("status", "")
assert status in ("queued", "running", "succeeded"), f"async invoke unexpected status: {status}"
' <<<"${async_json}" >/dev/null
  local async_inv_id async_get_json=""
  async_inv_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${async_json}")"
  for _ in $(seq 1 60); do
    async_get_json="$(local_smoke_retry_json "${axern_cmd[@]}" function invocation get -o json "${async_inv_id}")"
    if python3 -c 'import json,sys; s=json.load(sys.stdin).get("status",""); raise SystemExit(0 if s in ("succeeded","failed","cancelled","timed_out") else 1)' <<<"${async_get_json}"; then
      break
    fi
    sleep 1
  done
  python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("status") == "succeeded", f"async invocation did not succeed: {d}"' <<<"${async_get_json}" >/dev/null

  echo "${service_prefix}_function_smoke_phase=idempotency" >&2
  local request_id="smoke-idemp-$(date +%s)"
  local invoke1 invoke2 inv_id1 inv_id2
  invoke1="$(local_smoke_retry_json "${axern_cmd[@]}" fn invoke -o json --namespace "${namespace}" "${fn_name}" -d '{"name":"idemp"}' --request-id "${request_id}")"
  inv_id1="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${invoke1}")"
  invoke2="$(local_smoke_retry_json "${axern_cmd[@]}" fn invoke -o json --namespace "${namespace}" "${fn_name}" -d '{"name":"idemp"}' --request-id "${request_id}")"
  inv_id2="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"${invoke2}")"
  if [ "${inv_id1}" != "${inv_id2}" ]; then
    echo "idempotency failed: got different invocation ids ${inv_id1} vs ${inv_id2}" >&2
    return 1
  fi

  echo "${service_prefix}_function_smoke_phase=list" >&2
  local list_json
  list_json="$(local_smoke_retry_json "${axern_cmd[@]}" fn list -o json --namespace "${namespace}")"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert len(data.get("functions",[])) >= 1' <<<"${list_json}" >/dev/null

  echo "${service_prefix}_function_smoke_phase=invocations" >&2
  local invocations_json
  invocations_json="$(local_smoke_retry_json "${axern_cmd[@]}" function invocation list -o json --namespace "${namespace}" "${fn_name}")"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert len(data.get("invocations",[])) >= 2, f"expected at least 2 invocations: {data}"' <<<"${invocations_json}" >/dev/null

  echo "${service_prefix}_function_smoke_phase=invocation_events" >&2
  local events_json
  events_json="$(local_smoke_retry_json "${axern_cmd[@]}" function invocation events -o json "${sync_inv_id}")"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert len(data.get("events",[])) >= 1' <<<"${events_json}" >/dev/null

  echo "${service_prefix}_function_smoke_phase=delete" >&2
  local delete_json
  delete_json="$(local_smoke_retry_json "${axern_cmd[@]}" fn delete -o json "${function_id}")"
  python3 -c 'import json,sys; data=json.load(sys.stdin); assert data.get("status") == "deleted"' <<<"${delete_json}" >/dev/null
  function_id=""

  echo "${service_prefix}_function_smoke_ok=true"
}

_write_smoke_function() {
  local dir="$1"
  local name="$2"
  local namespace="$3"
  local src_dir="${dir}/src"
  mkdir -p "${src_dir}"
  cat > "${dir}/function.yaml" <<MANIFEST
api_version: axern/v1
kind: Function
metadata:
  name: ${name}
  namespace: ${namespace}
spec:
  source:
    template: python311
  function:
    runtime: python3.11
    handler: handler.hello
    initializer: handler.init
    source: src
    timeout_seconds: 30
    scaling:
      min_replicas: 1
      max_replicas: 1
      concurrency: 1
      idle_timeout: 1m
MANIFEST
  cat > "${src_dir}/handler.py" <<'HANDLER'
def init(context):
    return {"initialized": True, "function": context.function_name}

def hello(event, context):
    return {
        "message": f"hello {event.get('name', 'world')}",
        "request_id": context.request_id,
        "state": context.state,
    }
HANDLER
}
