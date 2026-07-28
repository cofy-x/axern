#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

check_same_file() {
  local source_file="$1"
  local copied_file="$2"
  if cmp -s "${source_file}" "${copied_file}"; then
    return 0
  fi
  echo "Grafana asset copy is out of sync:" >&2
  echo "  source: ${source_file#${ROOT_DIR}/}" >&2
  echo "  copy:   ${copied_file#${ROOT_DIR}/}" >&2
  echo "Update the Helm chart copy from the local Grafana asset." >&2
  return 1
}

check_same_file \
  "${ROOT_DIR}/deploy/local/grafana/provisioning/dashboards/axern.yaml" \
  "${ROOT_DIR}/deploy/helm/axern/files/grafana/provisioning/dashboards/axern.yaml"

if ! grep -q 'allowUiUpdates: false' "${ROOT_DIR}/deploy/local/grafana/provisioning/dashboards/axern.yaml"; then
  echo "Provisioned Grafana dashboards must be managed from source" >&2
  exit 1
fi

dashboard_mount_path="/var/lib/grafana/dashboards/axern"
for asset in \
  "${ROOT_DIR}/deploy/local/grafana/provisioning/dashboards/axern.yaml" \
  "${ROOT_DIR}/deploy/local/compose/docker-compose.yml" \
  "${ROOT_DIR}/deploy/local/k8s/otel.yaml" \
  "${ROOT_DIR}/deploy/helm/axern/templates/grafana.yaml"
do
  if ! grep -q "${dashboard_mount_path}" "${asset}"; then
    echo "Grafana dashboard path is inconsistent: ${asset#${ROOT_DIR}/}" >&2
    exit 1
  fi
done

check_same_file \
  "${ROOT_DIR}/deploy/local/grafana/dashboards/axern-core.json" \
  "${ROOT_DIR}/deploy/helm/axern/files/grafana/dashboards/axern-core.json"

check_same_file \
  "${ROOT_DIR}/deploy/local/grafana/dashboards/axern-node-resources.json" \
  "${ROOT_DIR}/deploy/helm/axern/files/grafana/dashboards/axern-node-resources.json"

check_same_file \
  "${ROOT_DIR}/deploy/local/grafana/dashboards/axern-image-distribution.json" \
  "${ROOT_DIR}/deploy/helm/axern/files/grafana/dashboards/axern-image-distribution.json"

for dashboard in \
  axern-core.json \
  axern-node-resources.json \
  axern-image-distribution.json
do
  dashboard_path="${ROOT_DIR}/deploy/local/grafana/dashboards/${dashboard}"
  if ! jq -e '
    (.timepicker.refresh_intervals | index("5s")) != null and
    (.panels | length) > 0 and
    ([.panels[].id] | unique | length) == (.panels | length) and
    ([.panels[].title] | unique | length) == (.panels | length) and
    all(.panels[];
      (.id | type) == "number" and
      (.title | type) == "string" and
      (.gridPos | type) == "object" and
      .gridPos.x >= 0 and
      .gridPos.y >= 0 and
      .gridPos.w > 0 and
      .gridPos.h > 0 and
      (.gridPos.x + .gridPos.w) <= 24 and
      (.targets | type) == "array")
  ' "${dashboard_path}" >/dev/null; then
    echo "Grafana dashboard has an invalid panel or refresh contract: ${dashboard}" >&2
    exit 1
  fi
done

core_dashboard="${ROOT_DIR}/deploy/local/grafana/dashboards/axern-core.json"
node_dashboard="${ROOT_DIR}/deploy/local/grafana/dashboards/axern-node-resources.json"
image_distribution_dashboard="${ROOT_DIR}/deploy/local/grafana/dashboards/axern-image-distribution.json"

if ! grep -q 'job=\\"dragonfly-seed-client\\"' "${image_distribution_dashboard}"; then
  echo "Grafana image distribution dashboard must query the centralized Seed Client job" >&2
  exit 1
fi

if grep -q 'job=\\"dragonfly-client\\"' "${image_distribution_dashboard}"; then
  echo "Grafana image distribution dashboard must not query the unused node-local Dragonfly job" >&2
  exit 1
fi

if ! jq -e '
  ([.panels[] | select(.id == 1 or .id == 2) | .gridPos.y] | unique | length) == 1 and
  ([.panels[] | select(.id == 1 or .id == 2) | .gridPos.h] | unique | length) == 1
' "${image_distribution_dashboard}" >/dev/null; then
  echo "Grafana image distribution summary panels must share the same row and height" >&2
  exit 1
fi

for metric in \
  axern_controld_service_ready_duration_seconds_count \
  axern_controld_service_replica_ready_duration_seconds_count \
  axern_axnoded_allocation_start_duration_seconds_count \
  axern_imagemgr_timed_operation_duration_seconds_count
do
  if ! grep -q "${metric}" "${core_dashboard}"; then
    echo "Grafana core dashboard is missing startup event rate metric: ${metric}" >&2
    exit 1
  fi
done

if ! grep -q 'last_over_time' "${core_dashboard}"; then
  echo "Grafana core dashboard is missing recent latency continuity queries" >&2
  exit 1
fi

if ! jq -e '
  all(.panels[] | select(.id == 12 or .id == 13 or .id == 14 or .id == 15);
    all(.targets[]; (.expr | contains("last_over_time")) and (.expr | contains("or vector(0)") | not))) and
  (.panels[] | select(.id == 17) | .fieldConfig.defaults.noValue) == "Unavailable" and
  (.panels[] | select(.id == 17) | .fieldConfig.defaults.mappings[0].options["-1"].text) == "Idle" and
  (.panels[] | select(.id == 17) | .targets[0].expr | contains("axern_controld_reconcile_consecutive_failures")) and
  (.panels[] | select(.id == 21) | .targets[0].expr | contains("0 * axern_controld_reconcile_consecutive_failures")) and
  (.panels[] | select(.id == 21) | .targets[0].expr | contains("clamp_min") | not) and
  (.tags | index("local")) == null
' "${core_dashboard}" >/dev/null; then
  echo "Grafana core dashboard has invalid idle/no-data semantics" >&2
  exit 1
fi

if ! jq -e '
  .panels[] | select(.id == 15) |
  .title == "Node BPFNet State" and
  .type == "table" and
  (.targets | length) == 5 and
  all(.targets[]; .instant == true and .format == "table")
' "${node_dashboard}" >/dev/null; then
  echo "Grafana node resources dashboard has an invalid BPFNet state table" >&2
  exit 1
fi

for metric in \
  axern_controld_nodes_current \
  axern_controld_node_resource_current \
  axern_controld_node_storage_current \
  axern_controld_node_pool_current \
  axern_controld_node_bpfnet_current \
  axern_controld_node_allocations_current
do
  if ! grep -q "${metric}" "${node_dashboard}"; then
    echo "Grafana node resources dashboard is missing node metric: ${metric}" >&2
    exit 1
  fi
done

if grep -q 'axern_controld_creatable_instances_current' "${ROOT_DIR}"/deploy/local/grafana/dashboards/*.json; then
  echo "Grafana dashboards must not restore the fixed-shape creatable instances metric" >&2
  exit 1
fi

for metric in \
  axern_controld_reconcile_consecutive_failures \
  axern_controld_reconcile_last_success_age_seconds \
  axern_controld_reconcile_last_error_age_seconds \
  axern_controld_reconcile_running
do
  if ! grep -q "${metric}" "${core_dashboard}"; then
    echo "Grafana core dashboard is missing reconcile health metric: ${metric}" >&2
    exit 1
  fi
done
