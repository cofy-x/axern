#!/usr/bin/env bash
set -euo pipefail

AXERN_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source "${AXERN_ROOT}/scripts/release/images.sh"
# shellcheck source=../proxy-env.sh
source "${AXERN_ROOT}/scripts/proxy-env.sh"
tag="$(axern_release_version)"
namespace="axern-release"
cluster="axern-release"
chart="${AXERN_HELM_CHART:-oci://ghcr.io/cofy-x/charts/axern}"
image_tag_suffix="${AXERN_RELEASE_IMAGE_TAG_SUFFIX:-}"
state_dir="$(mktemp -d)"
cleanup() {
  jobs -p | xargs kill >/dev/null 2>&1 || true
  kind delete cluster --name "${cluster}" >/dev/null 2>&1 || true
  rm -rf "${state_dir}"
}
trap cleanup EXIT

release_http_proxy="$(container_proxy_url "${HTTP_PROXY:-${http_proxy:-}}")"
release_https_proxy="$(container_proxy_url "${HTTPS_PROXY:-${https_proxy:-}}")"
release_no_proxy="$(append_no_proxy_entries \
  "${NO_PROXY:-${no_proxy:-}}" \
  'localhost,127.0.0.1,::1,host.docker.internal,.svc,.svc.cluster.local,.cluster.local,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16')"
if [ -n "${release_http_proxy}" ] || [ -n "${release_https_proxy}" ]; then
  export HTTP_PROXY="${release_http_proxy}"
  export HTTPS_PROXY="${release_https_proxy}"
  export NO_PROXY="${release_no_proxy}"
  export http_proxy="${release_http_proxy}"
  export https_proxy="${release_https_proxy}"
  export no_proxy="${release_no_proxy}"
  echo "release_kind_proxy_configured=true"
fi

kind create cluster --name "${cluster}" --wait 120s
kubectl create namespace "${namespace}"

helm_args=(
  install axern "${chart}"
  --namespace "${namespace}"
  --wait --timeout 15m
)
if [[ "${chart}" == oci://* ]]; then
  helm_args+=(--version "${tag#v}")
fi
if [ -n "${image_tag_suffix}" ]; then
  candidate_tag="${tag}-${image_tag_suffix}"
  helm_args+=(
    --set-string "controld.image.tag=${candidate_tag}"
    --set-string "tunneld.image.tag=${candidate_tag}"
    --set-string "node.image.tag=${candidate_tag}"
    --set-string "gatewayd.image.tag=${candidate_tag}"
    --set-string "runtimeCatalog.python311Image=${AXERN_RELEASE_REGISTRY}/python311-runtime:${candidate_tag}"
    --set-string "runtimeCatalog.serverBaseImage=${AXERN_RELEASE_REGISTRY}/server-base-runtime:${candidate_tag}"
    --set-string "runtimeCatalog.codingBaseImage=${AXERN_RELEASE_REGISTRY}/coding-base-runtime:${candidate_tag}"
    --set-string "runtimeCatalog.desktopBaseImage=${AXERN_RELEASE_REGISTRY}/desktop-base-runtime:${candidate_tag}"
    --set-string "runtimeCatalog.claudeCodeBundleImage=${AXERN_RELEASE_REGISTRY}/claude-code-bundle:${candidate_tag}"
    --set-string "runtimeCatalog.codexBundleImage=${AXERN_RELEASE_REGISTRY}/codex-bundle:${candidate_tag}"
  )
fi
if [ -n "${release_http_proxy}" ] || [ -n "${release_https_proxy}" ]; then
  registry_proxy_url="${release_https_proxy:-${release_http_proxy}}"
  helm_no_proxy="${release_no_proxy//,/\\,}"
  helm_args+=(
    --set-string "proxyEnv.HTTP_PROXY=${release_http_proxy}"
    --set-string "proxyEnv.HTTPS_PROXY=${release_https_proxy}"
    --set-string "proxyEnv.NO_PROXY=${helm_no_proxy}"
    --set-string "proxyEnv.http_proxy=${release_http_proxy}"
    --set-string "proxyEnv.https_proxy=${release_https_proxy}"
    --set-string "proxyEnv.no_proxy=${helm_no_proxy}"
    --set-string "proxyEnv.REGISTRY_PROXY_URL=${registry_proxy_url}"
    --set-string "proxyEnv.REGISTRY_NO_PROXY=${helm_no_proxy}"
  )
fi
if [ -n "${AXERN_REGISTRY_USERNAME:-}" ] && [ -n "${AXERN_REGISTRY_PASSWORD:-}" ]; then
  kubectl --namespace "${namespace}" create secret docker-registry axern-release-registry \
    --docker-server="${AXERN_RELEASE_REGISTRY%%/*}" \
    --docker-username="${AXERN_REGISTRY_USERNAME}" \
    --docker-password="${AXERN_REGISTRY_PASSWORD}"
  helm_args+=(
    --set-string 'global.imagePullSecrets[0].name=axern-release-registry'
    --set-string 'node.registryAuth.existingSecret=axern-release-registry'
    --set-string 'rolloutWorker.registryAuth.existingSecret=axern-release-registry'
  )
fi
helm "${helm_args[@]}"
kubectl --namespace "${namespace}" rollout status deployment/controld --timeout=5m
kubectl --namespace "${namespace}" rollout status deployment/gatewayd --timeout=5m
kubectl --namespace "${namespace}" rollout status daemonset/node-all-in-one --timeout=10m

kubectl --namespace "${namespace}" port-forward svc/gatewayd 25100:25000 25101:25080 >"${state_dir}/port-forward.log" 2>&1 &
port_forward_pid=$!
gateway_ready=false
for _ in $(seq 1 60); do
  if curl --fail --silent http://127.0.0.1:25101/healthz >/dev/null 2>&1; then
    gateway_ready=true
    break
  fi
  if ! kill -0 "${port_forward_pid}" >/dev/null 2>&1; then
    cat "${state_dir}/port-forward.log" >&2
    exit 1
  fi
  sleep 1
done
if [ "${gateway_ready}" != "true" ]; then
  cat "${state_dir}/port-forward.log" >&2
  echo "gateway port-forward did not become ready" >&2
  exit 1
fi

config="${state_dir}/config.json"
"${AXERN_CLI_BINARY:-${AXERN_ROOT}/bin/axern}" --config "${config}" context import-kubernetes release \
  --namespace "${namespace}" --cert-dir "${state_dir}/certs" --current
"${AXERN_CLI_BINARY:-${AXERN_ROOT}/bin/axern}" --config "${config}" catalog list --output json >/dev/null

cat >"${state_dir}/run.yaml" <<'YAML'
api_version: axern/v1
kind: Run
metadata:
  namespace: default
spec:
  source:
    template: python311
  command:
    argv: [python, -c, "print('axern-release-ok')"]
  runtime_class: runsc
  resources: {}
YAML
"${AXERN_CLI_BINARY:-${AXERN_ROOT}/bin/axern}" --config "${config}" --timeout 10m run create --file "${state_dir}/run.yaml" --wait

if [ "$#" -gt 0 ]; then
  AXERN_SDK_ACCEPTANCE_CONFIG="${config}" \
    AXERN_SDK_ACCEPTANCE_CONTEXT=release \
    AXERN_SDK_ACCEPTANCE_CLI="${AXERN_CLI_BINARY:-${AXERN_ROOT}/bin/axern}" \
    "$@"
fi

echo "release_kind_acceptance_ok=${tag}${image_tag_suffix:+-${image_tag_suffix}}"
