run_local_image_service_smoke() {
  local env_name="$1"
  local endpoint="$2"
  local prefix="$3"
  local gateway_url="$4"
  local namespace="${prefix}-${env_name}-image-service-smoke-$(date +%s)"
  local requested_image_ref="${LOCAL_IMAGE_SERVICE_SMOKE_REQUESTED_IMAGE_REF:-python:3.12-slim}"
  local resolved_image_ref

  if ! resolved_image_ref="$(local_image_service_smoke_prepare_image "${env_name}" "${requested_image_ref}")"; then
    return 1
  fi
  export LOCAL_SMOKE_RESOLVED_IMAGE_REF="${resolved_image_ref}"

  local_smoke_init_axern_cmd "${env_name}" "${endpoint}"
  local env_json environment_id service_json service_id service_get body
  env_json="$(local_smoke_create_environment "${namespace}")"
  environment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["environment"]["id"])' <<<"${env_json}")"

  local service_script
  service_script='from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"image-service-smoke-ok\n"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, fmt, *args):
        pass
ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()'

  service_id=""
  cleanup_image_service() {
    if [ -n "${service_id}" ]; then
      local_smoke_delete_service "${service_id}" >/dev/null 2>&1 || true
      service_id=""
    fi
  }
  trap cleanup_image_service RETURN

  fail_image_service_smoke() {
    local reason="$1"
    echo "image service smoke failed: ${reason}" >&2
    if [ -n "${service_get}" ]; then
      echo "last service state:" >&2
      python3 -m json.tool <<<"${service_get}" >&2 || printf '%s\n' "${service_get}" >&2
    fi
    if [ -n "${service_id}" ]; then
      echo "service events:" >&2
      local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service events -o json "${service_id}" 2>/dev/null | python3 -m json.tool >&2 || true
    fi
    cleanup_image_service
    return 1
  }

  service_json="$(local_smoke_json_once_or_recover_by_namespace service services service "${namespace}" \
    "${AXERN_SMOKE_CMD[@]}" service create -o json --namespace "${namespace}" \
    --environment-id "${environment_id}" --replicas 1 \
    --argv python --argv -u --argv -c --argv "${service_script}" \
    --readiness-http-port 8080 --readiness-http-path / --readiness-period 1s --readiness-timeout 1s)"
  service_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["service"]["id"])' <<<"${service_json}")"

  local deadline=$((SECONDS + 120))
  service_get=""
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    service_get="$(local_smoke_retry_json "${AXERN_SMOKE_CMD[@]}" service get -o json "${service_id}" 2>/dev/null || true)"
    if [ -n "${service_get}" ] && python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1 else 1)' <<<"${service_get}"; then
      break
    fi
    sleep 2
  done
  if ! [ -n "${service_get}" ] || ! python3 -c 'import json,sys; data=json.load(sys.stdin); sys.exit(0 if data["service"]["status"] == "ready" and data["service"]["ready_replicas"] == 1 else 1)' <<<"${service_get}" >/dev/null; then
    fail_image_service_smoke "service did not become ready before timeout"
    return 1
  fi

  if ! body="$(curl --connect-timeout 2 --max-time 30 -fsS "${gateway_url}/svc/${namespace}/${service_id}/8080/smoke")"; then
    fail_image_service_smoke "gateway request failed"
    return 1
  fi
  if [ "${body}" != "image-service-smoke-ok" ]; then
    echo "unexpected image service response: ${body}" >&2
    return 1
  fi

  cleanup_image_service
  echo "${prefix}_image_service_smoke_ok=true"
}

local_image_service_smoke_prepare_image() {
  local env_name="$1"
  local requested_image_ref="$2"
  local resolved_image_ref
  local node_platform

  case "${env_name}" in
    kind)
      node_platform="$(local_image_service_smoke_kind_node_platform)" || return 1
      resolved_image_ref="$(ensure_docker_image_resolved_for_platform "${requested_image_ref}" "${node_platform}")" || return 1
      IMAGE="${resolved_image_ref}" bash "${AXERN_ROOT}/scripts/dev-env/kind-image-import.sh" >/dev/null
      ;;
    compose)
      node_platform="$(local_image_service_smoke_compose_node_platform)" || return 1
      resolved_image_ref="$(ensure_docker_image_resolved_for_platform "${requested_image_ref}" "${node_platform}")" || return 1
      IMAGE="${resolved_image_ref}" bash "${AXERN_ROOT}/scripts/dev-env/compose-image-import.sh" >/dev/null
      ;;
    *)
      resolved_image_ref="$(docker_image_resolved_digest_ref "${requested_image_ref}")" || return 1
      ;;
  esac

  printf '%s\n' "${resolved_image_ref}"
}

local_image_service_smoke_kind_node_platform() {
  local node_arches node_arch
  node_arches="$(kubectl get nodes -o jsonpath='{range .items[*]}{.status.nodeInfo.architecture}{"\n"}{end}' | awk 'NF' | sort -u)"
  if [ -z "${node_arches}" ]; then
    echo "kind image service smoke could not determine node architecture" >&2
    return 1
  fi
  if [ "$(printf '%s\n' "${node_arches}" | wc -l | tr -d ' ')" != "1" ]; then
    echo "kind image service smoke requires a single-architecture node pool, got: ${node_arches}" >&2
    return 1
  fi
  node_arch="$(printf '%s\n' "${node_arches}" | head -n 1)"
  linux_platform_from_uname_arch "${node_arch}"
}

local_image_service_smoke_compose_node_platform() {
  local node_image_id
  node_image_id="$(docker inspect "${COMPOSE_PROJECT_NAME}-node-1" --format '{{.Image}}')" || return 1
  docker image inspect "${node_image_id}" \
    --format '{{.Os}}/{{.Architecture}}{{if .Variant}}/{{.Variant}}{{end}}'
}
