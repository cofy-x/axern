#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

mode="${1:-}"
service_smoke="false"
service_volume_smoke="false"
run_smoke="false"
server_base_smoke="false"
quota_admission_smoke="false"
function_smoke="false"

for arg in "$@"; do
  case "${arg}" in
    --smoke) service_smoke="true" ;;
    --service-volume-smoke) service_volume_smoke="true" ;;
    --run-smoke) run_smoke="true" ;;
    --server-base-smoke) server_base_smoke="true" ;;
    --quota-admission-smoke) quota_admission_smoke="true" ;;
    --function-smoke) function_smoke="true" ;;
  esac
done

should_check_consistency() {
  [ "${service_smoke}" = "true" ] ||
    [ "${service_volume_smoke}" = "true" ] ||
    [ "${run_smoke}" = "true" ] ||
    [ "${server_base_smoke}" = "true" ] ||
    [ "${quota_admission_smoke}" = "true" ] ||
    [ "${function_smoke}" = "true" ]
}

case "${mode}" in
  compose)
    echo "wait_ready_phase=controld_health timeout_seconds=120"
    if ! wait_for_http_ready "http://127.0.0.1:${COMPOSE_CONTROLD_HTTP_PORT}/healthz" 120 controld_health; then
      echo "compose controld did not become ready in time" >&2
      exit 1
    fi
    echo "wait_ready_phase=gateway_health timeout_seconds=120"
    if ! wait_for_http_ready "http://127.0.0.1:${COMPOSE_GATEWAY_HTTP_PORT}/healthz" 120 gateway_health; then
      echo "compose gatewayd did not become ready in time" >&2
      exit 1
    fi
    echo "wait_ready_phase=node_summary timeout_seconds=180"
    if ! wait_for_node_summary "http://127.0.0.1:${COMPOSE_CONTROLD_HTTP_PORT}/nodesz" "node-compose-local" 180; then
      echo "compose node summary did not become fresh in time" >&2
      exit 1
    fi
    [ "${service_smoke}" = "true" ] && run_local_smoke compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}" "compose"
    [ "${service_volume_smoke}" = "true" ] && run_local_service_volume_smoke compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}" "compose"
    [ "${run_smoke}" = "true" ] && run_local_run_smoke compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}" "compose"
    [ "${server_base_smoke}" = "true" ] && run_local_server_base_smoke compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}" "compose" "127.0.0.1:${COMPOSE_GATEWAY_HTTP_PORT}"
    [ "${quota_admission_smoke}" = "true" ] && run_local_quota_admission_smoke compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}" "compose"
    [ "${function_smoke}" = "true" ] && run_local_function_smoke compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}" "compose"
    should_check_consistency && local_smoke_assert_consistency_ok compose "127.0.0.1:${COMPOSE_GATEWAY_CONTROL_PORT}"
    ;;
  k8s)
    kubectl -n "${K8S_NAMESPACE}" rollout status deployment/postgres --timeout=180s >/dev/null
    kubectl -n "${K8S_NAMESPACE}" wait --for=condition=complete job/controld-migrate --timeout=180s >/dev/null
    kubectl -n "${K8S_NAMESPACE}" rollout status deployment/minio --timeout=180s >/dev/null
    kubectl -n "${K8S_NAMESPACE}" rollout status deployment/storaged --timeout=180s >/dev/null
    kubectl -n "${K8S_NAMESPACE}" rollout status deployment/controld --timeout=180s >/dev/null
    kubectl -n "${K8S_NAMESPACE}" rollout status deployment/controld-retention --timeout=180s >/dev/null
    kubectl -n "${K8S_NAMESPACE}" rollout status deployment/gatewayd --timeout=180s >/dev/null
    kubectl -n "${K8S_NAMESPACE}" rollout status daemonset/node-all-in-one --timeout=240s >/dev/null
    ensure_k8s_local_access
    if ! wait_for_http_ready "http://127.0.0.1:${K8S_CONTROLD_LOCAL_HTTP_PORT}/healthz" 120; then
      echo "k8s controld did not become ready in time" >&2
      exit 1
    fi
    if ! wait_for_http_ready "http://127.0.0.1:${K8S_GATEWAY_LOCAL_HTTP_PORT}/healthz" 120; then
      echo "k8s gatewayd did not become ready in time" >&2
      exit 1
    fi
    local_node_ids=()
    while IFS= read -r local_node_id; do
      local_node_ids+=("${local_node_id}")
    done < <(kubectl get nodes -o jsonpath='{range .items[*]}node-{.metadata.name}{"\n"}{end}' | awk 'NF' | sort)
    if ! wait_for_node_summaries "http://127.0.0.1:${K8S_CONTROLD_LOCAL_HTTP_PORT}/nodesz" 240 "${local_node_ids[@]}"; then
      echo "k8s node summaries did not become fresh in time" >&2
      exit 1
    fi
    [ "${service_smoke}" = "true" ] && run_local_smoke "${K8S_ENV_NAME}" "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}" "${K8S_ENV_NAME}"
    [ "${service_volume_smoke}" = "true" ] && run_local_service_volume_smoke "${K8S_ENV_NAME}" "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}" "${K8S_ENV_NAME}"
    [ "${run_smoke}" = "true" ] && run_local_run_smoke "${K8S_ENV_NAME}" "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}" "${K8S_ENV_NAME}"
    [ "${server_base_smoke}" = "true" ] && run_local_server_base_smoke "${K8S_ENV_NAME}" "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}" "${K8S_ENV_NAME}" "127.0.0.1:${K8S_GATEWAY_LOCAL_HTTP_PORT}"
    [ "${quota_admission_smoke}" = "true" ] && run_local_quota_admission_smoke "${K8S_ENV_NAME}" "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}" "${K8S_ENV_NAME}"
    [ "${function_smoke}" = "true" ] && run_local_function_smoke "${K8S_ENV_NAME}" "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}" "${K8S_ENV_NAME}"
    should_check_consistency && local_smoke_assert_consistency_ok "${K8S_ENV_NAME}" "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}"
    ;;
  *)
    echo "usage: $0 <compose|k8s> [--smoke] [--service-volume-smoke] [--run-smoke] [--server-base-smoke] [--quota-admission-smoke] [--function-smoke]" >&2
    exit 1
    ;;
esac

echo "${mode}_ready=true"
