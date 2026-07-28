#!/usr/bin/env bash

verify_external_image_ref() {
  local image_ref="${AXERN_CLI_E2E_IMAGE_REF:-docker.io/library/nginx:1.27}"
  local expected_ref="${AXERN_CLI_E2E_EXPECTED_IMAGE_REF:-${image_ref}}"
  local runtime_classes="${AXERN_CLI_E2E_IMAGE_REF_RUNTIME_CLASSES:-runc runsc}"
  local ready_timeout="${AXERN_CLI_E2E_IMAGE_REF_READY_TIMEOUT:-300}"
  local env_output environment_id env_image_ref env_image_digest runtime_class

  if [[ "${expected_ref}" == docker.io/* ]]; then
    expected_ref="index.${expected_ref}"
  fi

  echo "axern_cli_image_ref_e2e_image_ref=${image_ref}" >&2
  echo "axern_cli_image_ref_e2e_expected_image_ref=${expected_ref}" >&2
  echo "axern_cli_image_ref_e2e_runtime_classes=${runtime_classes}" >&2
  echo "axern_cli_image_ref_e2e_ready_timeout=${ready_timeout}" >&2
  echo "axern_cli_image_ref_e2e_registry_proxy_url=${REGISTRY_PROXY_URL:-}" >&2
  echo "axern_cli_image_ref_e2e_registry_no_proxy=${REGISTRY_NO_PROXY:-}" >&2

  if [[ -z "${runtime_classes//[[:space:]]/}" ]]; then
    echo "AXERN_CLI_E2E_IMAGE_REF_RUNTIME_CLASSES must include at least one runtime class" >&2
    dump_logs
    exit 1
  fi

  env_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment create -o json --image-ref "${image_ref}")"
  environment_id="$(json_query "environment create external image-ref" 'json.load(sys.stdin)["environment"]["id"]' "${env_output}")"
  [ -n "${environment_id}" ] || {
    echo "environment create --image-ref did not return an environment id" >&2
    dump_logs
    exit 1
  }

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" environment get "${environment_id}" -o json >"${cli_object_output}"
  env_image_ref="$(json_query "environment get external image-ref" 'json.load(sys.stdin)["environment"]["spec"]["image"]["ref"]' "$(cat "${cli_object_output}")")"
  env_image_digest="$(json_query "environment get external image digest" 'json.load(sys.stdin)["environment"]["spec"]["image"]["digest"]' "$(cat "${cli_object_output}")")"
  [ "${env_image_ref}" = "${expected_ref}" ] || {
    echo "environment image ref = ${env_image_ref}, want ${expected_ref}" >&2
    dump_logs
    exit 1
  }
  [[ "${env_image_digest}" == sha256:* ]] || {
    echo "environment image digest = ${env_image_digest}, want sha256 digest" >&2
    dump_logs
    exit 1
  }

  for runtime_class in ${runtime_classes}; do
    verify_external_image_ref_runtime "${environment_id}" "${runtime_class}" "${ready_timeout}"
  done
}

verify_external_image_ref_runtime() {
  local environment_id="$1"
  local runtime_class="$2"
  local ready_timeout="$3"
  local service_output service_id allocation_id replicas_json deleted_service_id

  echo "axern_cli_image_ref_e2e_runtime_class=${runtime_class}" >&2
  service_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service create \
    -o json \
    --environment-id "${environment_id}" \
    --runtime-class "${runtime_class}" \
    --replicas 1 \
    --argv /bin/sh \
    --argv -lc \
    --argv 'sleep 120')"
  service_id="$(json_query "service create external image-ref" 'json.load(sys.stdin)["service"]["id"]' "${service_output}")"
  [ -n "${service_id}" ] || {
    echo "service create for external image-ref did not return a service id" >&2
    dump_logs
    exit 1
  }

  allocation_id="$(wait_for_ready_service_allocation "${service_id}" "external image-ref service" "${ready_timeout}")"
  replicas_json="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service replicas "${service_id}" --view current -o json)"
  if ! python3 -c '
import json
import sys

allocation_id = sys.argv[1]
payload = json.load(sys.stdin)
for replica in payload.get("replicas", []):
    if replica.get("id") == allocation_id and replica.get("ready") and not replica.get("terminal"):
        raise SystemExit(0)
raise SystemExit(1)
' "${allocation_id}" <<<"${replicas_json}"; then
    echo "external image-ref allocation did not appear ready in replicas output" >&2
    dump_logs
    exit 1
  fi

  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" service delete "${service_id}" -o json >"${cli_object_output}"
  deleted_service_id="$(json_query "service delete external image-ref" 'json.load(sys.stdin)["service_id"]' "$(cat "${cli_object_output}")")"
  [ "${deleted_service_id}" = "${service_id}" ] || {
    echo "service delete returned id = ${deleted_service_id}, want ${service_id}" >&2
    dump_logs
    exit 1
  }
}
