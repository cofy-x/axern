#!/usr/bin/env bash
set -euo pipefail

export K8S_ENV_NAME=kind
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

require_cmd docker
require_cmd kind
require_cmd kubectl

image_ref="${IMAGE:-}"
if [ -z "${image_ref}" ]; then
  echo "IMAGE is required, for example: make kind-image-import IMAGE=myapp:dev" >&2
  exit 2
fi

if ! docker image inspect "${image_ref}" >/dev/null 2>&1; then
  echo "host Docker image not found: ${image_ref}" >&2
  exit 1
fi

if ! kind get clusters 2>/dev/null | grep -Fxq "${K8S_CLUSTER_NAME}"; then
  echo "kind cluster is not present: ${K8S_CLUSTER_NAME}" >&2
  exit 1
fi

export KUBECONFIG="$(k8s_kubeconfig_file)"
pods="$(
  kubectl -n "${K8S_NAMESPACE}" get pods -l app=node-all-in-one \
    -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null || true
)"
if [ -z "${pods}" ]; then
  echo "no node-all-in-one pods found in namespace ${K8S_NAMESPACE}" >&2
  exit 1
fi

import_timeout="${AXERN_IMAGE_IMPORT_TIMEOUT:-5m}"
image_id="$(docker image inspect "${image_ref}" --format '{{.Id}}')"

while IFS= read -r pod; do
  [ -n "${pod}" ] || continue
  echo "Streaming ${image_ref} into kind node pod ${pod}"
  docker image save "${image_id}" | kubectl -n "${K8S_NAMESPACE}" exec -i "${pod}" -- axctl --timeout "${import_timeout}" image import \
    --imagemgr-socket /run/imagemgr/imagemgr.sock \
    --file - \
    --ref "${image_ref}"
done <<< "${pods}"
