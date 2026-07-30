#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd kubectl
require_cmd curl
begin_env_lock "${K8S_ENV_NAME}"
trap 'end_env_lock "${K8S_ENV_NAME}"' EXIT

ensure_state_dirs
ensure_local_images
ensure_k8s_images_loaded
generate_k8s_certs
ensure_k8s_ssh_keys
ensure_secrets_master_key "${K8S_ENV_NAME}"
write_cli_env "${K8S_ENV_NAME}" "127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT}"

kubectl apply -f "${DEPLOY_ROOT}/k8s/namespace.yaml"

kubectl -n "${K8S_NAMESPACE}" create secret generic controld-pki \
  --from-file=ca.crt="${K8S_STATE_DIR}/certs/ca.crt" \
  --from-file=controld.crt="${K8S_STATE_DIR}/certs/controld.crt" \
  --from-file=controld.key="${K8S_STATE_DIR}/certs/controld.key" \
  --from-file=client.crt="${K8S_STATE_DIR}/certs/client.crt" \
  --from-file=client.key="${K8S_STATE_DIR}/certs/client.key" \
  --from-file=rollout-worker.crt="${K8S_STATE_DIR}/certs/rollout-worker.crt" \
  --from-file=rollout-worker.key="${K8S_STATE_DIR}/certs/rollout-worker.key" \
  --from-file=gatewayd.crt="${K8S_STATE_DIR}/certs/gatewayd.crt" \
  --from-file=gatewayd.key="${K8S_STATE_DIR}/certs/gatewayd.key" \
  --from-file=node.crt="${K8S_STATE_DIR}/certs/node.crt" \
  --from-file=node.key="${K8S_STATE_DIR}/certs/node.key" \
  --from-file=tunneld.crt="${K8S_STATE_DIR}/certs/tunneld.crt" \
  --from-file=tunneld.key="${K8S_STATE_DIR}/certs/tunneld.key" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "${K8S_NAMESPACE}" create secret generic controld-secrets \
  --from-literal=AXERN_SECRETS_MASTER_KEY="$(cat "$(secrets_master_key_file "${K8S_ENV_NAME}")")" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl -n "${K8S_NAMESPACE}" create secret generic gatewayd-ssh \
  --from-file=gateway_host_ed25519="${K8S_STATE_DIR}/ssh/gateway_host_ed25519" \
  --from-file=authorized_keys="${K8S_STATE_DIR}/ssh/authorized_keys" \
  --dry-run=client -o yaml | kubectl apply -f -

proxy_env_args=()
while IFS= read -r proxy_env_arg; do
  proxy_env_args+=("${proxy_env_arg}")
done < <(k8s_proxy_env_args)
kubectl -n "${K8S_NAMESPACE}" create configmap local-proxy-env \
  "${proxy_env_args[@]}" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f "${DEPLOY_ROOT}/k8s/postgres.yaml"
kubectl apply -f "${DEPLOY_ROOT}/k8s/minio.yaml"
if [ "${OTEL:-1}" = "1" ] || [ "${OTEL:-1}" = "true" ]; then
  kubectl -n "${K8S_NAMESPACE}" create configmap grafana-dashboard-provisioning \
    --from-file=axern.yaml="${DEPLOY_ROOT}/grafana/provisioning/dashboards/axern.yaml" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl -n "${K8S_NAMESPACE}" create configmap grafana-dashboards \
    --from-file=axern-core.json="${DEPLOY_ROOT}/grafana/dashboards/axern-core.json" \
    --from-file=axern-node-resources.json="${DEPLOY_ROOT}/grafana/dashboards/axern-node-resources.json" \
    --from-file=axern-image-distribution.json="${DEPLOY_ROOT}/grafana/dashboards/axern-image-distribution.json" \
    --dry-run=client -o yaml | kubectl apply -f -
  kubectl apply -f "${DEPLOY_ROOT}/k8s/otel.yaml"
  kubectl -n "${K8S_NAMESPACE}" set image deployment/otel-collector \
    otel-collector="${OTEL_COLLECTOR_IMAGE}" >/dev/null
  kubectl -n "${K8S_NAMESPACE}" set image deployment/otel-lgtm \
    otel-lgtm="${OTEL_LGTM_IMAGE}" >/dev/null
  kubectl -n "${K8S_NAMESPACE}" rollout restart deployment/otel-collector deployment/otel-lgtm >/dev/null
  kubectl -n "${K8S_NAMESPACE}" delete deployment/jaeger service/jaeger --ignore-not-found >/dev/null
else
  kubectl delete -f "${DEPLOY_ROOT}/k8s/otel.yaml" --ignore-not-found >/dev/null
  kubectl -n "${K8S_NAMESPACE}" delete deployment/jaeger service/jaeger --ignore-not-found >/dev/null
fi
kubectl -n "${K8S_NAMESPACE}" rollout status deployment/postgres --timeout=180s >/dev/null
for deployment in gatewayd controld-retention controld storaged; do
  if kubectl -n "${K8S_NAMESPACE}" get deployment/"${deployment}" >/dev/null 2>&1; then
    kubectl -n "${K8S_NAMESPACE}" scale deployment/"${deployment}" --replicas=0 >/dev/null
    kubectl -n "${K8S_NAMESPACE}" rollout status deployment/"${deployment}" --timeout=180s >/dev/null
  fi
done
if [ "${AXERN_K8S_RESET_POSTGRES:-0}" = "1" ] || [ "${AXERN_K8S_RESET_POSTGRES:-0}" = "true" ]; then
  kubectl -n "${K8S_NAMESPACE}" rollout restart deployment/postgres >/dev/null
  kubectl -n "${K8S_NAMESPACE}" rollout status deployment/postgres --timeout=180s >/dev/null
fi
kubectl -n "${K8S_NAMESPACE}" delete job/controld-migrate --ignore-not-found >/dev/null
kubectl apply -f "${DEPLOY_ROOT}/k8s/controld-migrate.yaml"
kubectl -n "${K8S_NAMESPACE}" wait --for=condition=complete job/controld-migrate --timeout=180s >/dev/null
kubectl apply -f "${DEPLOY_ROOT}/k8s/controld.yaml"
kubectl apply -f "${DEPLOY_ROOT}/k8s/tunneld.yaml"
kubectl apply -f "${DEPLOY_ROOT}/k8s/node-all-in-one.yaml"
kubectl apply -f "${DEPLOY_ROOT}/k8s/gatewayd.yaml"

kubectl -n "${K8S_NAMESPACE}" set env deployment/controld \
	AXERN_RUNTIME_CATALOG_PYTHON311_IMAGE="${PYTHON311_RUNTIME_IMAGE}" \
	AXERN_RUNTIME_CATALOG_SERVER_BASE_IMAGE="${SERVER_BASE_RUNTIME_IMAGE}" \
	AXERN_RUNTIME_CATALOG_CODING_BASE_IMAGE="${CODING_BASE_RUNTIME_IMAGE}" \
	AXERN_RUNTIME_CATALOG_DESKTOP_BASE_IMAGE="${DESKTOP_BASE_RUNTIME_IMAGE}" \
	AXERN_AGENT_BUNDLE_CLAUDE_CODE_IMAGE="${CLAUDE_CODE_BUNDLE_IMAGE}" \
	AXERN_AGENT_BUNDLE_CODEX_IMAGE="${CODEX_BUNDLE_IMAGE}" \
	CONTROLD_TUNNEL_RELAYS="default,127.0.0.1:${K8S_GATEWAY_LOCAL_CONTROL_PORT},tunneld.${K8S_NAMESPACE}.svc.cluster.local:24100,1,false" >/dev/null

if [ "${OTEL:-1}" = "1" ] || [ "${OTEL:-1}" = "true" ]; then
  kubectl -n "${K8S_NAMESPACE}" set env deployment/controld \
    AXERN_OTEL_ENABLED=true \
    OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector.axern-local.svc.cluster.local:4317 \
    OTEL_EXPORTER_OTLP_INSECURE=true \
    OTEL_RESOURCE_ATTRIBUTES=deployment.environment="${K8S_ENV_NAME}" >/dev/null
  kubectl -n "${K8S_NAMESPACE}" set env deployment/controld-retention \
    AXERN_OTEL_ENABLED=true \
    OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector.axern-local.svc.cluster.local:4317 \
    OTEL_EXPORTER_OTLP_INSECURE=true \
    OTEL_RESOURCE_ATTRIBUTES=deployment.environment="${K8S_ENV_NAME}" >/dev/null
  kubectl -n "${K8S_NAMESPACE}" set env deployment/gatewayd \
    AXERN_OTEL_ENABLED=true \
    OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector.axern-local.svc.cluster.local:4317 \
    OTEL_EXPORTER_OTLP_INSECURE=true \
    OTEL_RESOURCE_ATTRIBUTES=deployment.environment="${K8S_ENV_NAME}" >/dev/null
  kubectl -n "${K8S_NAMESPACE}" set env daemonset/node-all-in-one \
    AXERN_OTEL_ENABLED=true \
    OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector.axern-local.svc.cluster.local:4317 \
    OTEL_EXPORTER_OTLP_INSECURE=true \
    OTEL_RESOURCE_ATTRIBUTES=deployment.environment="${K8S_ENV_NAME}" >/dev/null
else
  kubectl -n "${K8S_NAMESPACE}" set env deployment/controld \
    AXERN_OTEL_ENABLED=false \
    OTEL_EXPORTER_OTLP_ENDPOINT- \
    OTEL_EXPORTER_OTLP_INSECURE- \
    OTEL_RESOURCE_ATTRIBUTES- >/dev/null
  kubectl -n "${K8S_NAMESPACE}" set env deployment/controld-retention \
    AXERN_OTEL_ENABLED=false \
    OTEL_EXPORTER_OTLP_ENDPOINT- \
    OTEL_EXPORTER_OTLP_INSECURE- \
    OTEL_RESOURCE_ATTRIBUTES- >/dev/null
  kubectl -n "${K8S_NAMESPACE}" set env deployment/gatewayd \
    AXERN_OTEL_ENABLED=false \
    OTEL_EXPORTER_OTLP_ENDPOINT- \
    OTEL_EXPORTER_OTLP_INSECURE- \
    OTEL_RESOURCE_ATTRIBUTES- >/dev/null
  kubectl -n "${K8S_NAMESPACE}" set env daemonset/node-all-in-one \
    AXERN_OTEL_ENABLED=false \
    OTEL_EXPORTER_OTLP_ENDPOINT- \
    OTEL_EXPORTER_OTLP_INSECURE- \
    OTEL_RESOURCE_ATTRIBUTES- >/dev/null
fi

kubectl -n "${K8S_NAMESPACE}" rollout restart deployment/controld deployment/controld-retention >/dev/null
kubectl -n "${K8S_NAMESPACE}" rollout restart deployment/gatewayd >/dev/null
kubectl -n "${K8S_NAMESPACE}" rollout restart daemonset/node-all-in-one >/dev/null

bash "${AXERN_ROOT}/scripts/dev-env/wait-ready.sh" k8s

echo "k8s_up_ok=true"
echo "cli_env=$(cli_env_file "${K8S_ENV_NAME}")"
echo "axern_config=$(axern_config_file)"
