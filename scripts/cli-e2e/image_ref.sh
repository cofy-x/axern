#!/usr/bin/env bash

verify_external_image_ref() {
  local image_ref="${AXERN_CLI_E2E_IMAGE_REF:-docker.io/library/nginx:1.27}"
  local expected_ref="${AXERN_CLI_E2E_EXPECTED_IMAGE_REF:-${image_ref}}"
  local runtime_classes="${AXERN_CLI_E2E_IMAGE_REF_RUNTIME_CLASSES:-runsc}"
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
  local run_output run_id allocation_id cancelled_run_id

  echo "axern_cli_image_ref_e2e_runtime_class=${runtime_class}" >&2
  run_output="$("${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run --detach \
    -o json --environment "${environment_id}" \
    --runtime-class "${runtime_class}" \
    -- /bin/sh -lc 'sleep 120')"
  run_id="$(json_query "run create external image-ref" 'json.load(sys.stdin)["run"]["id"]' "${run_output}")"
  [ -n "${run_id}" ] || {
    echo "run create for external image-ref did not return a run id" >&2
    dump_logs
    exit 1
  }

  allocation_id="$(wait_for_running_run_allocation "${run_id}" "external image-ref run" "${ready_timeout}")"
  [ -n "${allocation_id}" ] || exit 1
  "${AXERN_BIN}" --endpoint "${GATEWAY_CONTROL_ADDRESS}" run cancel "${run_id}" -o json >"${cli_object_output}"
  cancelled_run_id="$(json_query "run cancel external image-ref" 'json.load(sys.stdin)["run"]["id"]' "$(cat "${cli_object_output}")")"
  [ "${cancelled_run_id}" = "${run_id}" ] || {
    echo "run cancel returned id = ${cancelled_run_id}, want ${run_id}" >&2
    dump_logs
    exit 1
  }
}
