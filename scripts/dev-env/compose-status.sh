#!/usr/bin/env bash
set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"

emit_lock_status "env-compose"

if [ ! -f "$(compose_env_file)" ]; then
  echo "compose_state=not_initialized"
  exit 0
fi

compose_file="${DEPLOY_ROOT}/compose/docker-compose.yml"
docker compose --project-name "${COMPOSE_PROJECT_NAME}" --env-file "$(compose_env_file)" -f "${compose_file}" ps

if curl --noproxy 'localhost,127.0.0.1,::1' -fsS "http://127.0.0.1:${COMPOSE_CONTROLD_HTTP_PORT}/healthz" >/dev/null 2>&1; then
  echo "controld_health=ready"
  emit_node_summary_status "http://127.0.0.1:${COMPOSE_CONTROLD_HTTP_PORT}/nodesz"
  emit_compose_imported_image_status
else
  echo "controld_health=unreachable"
  echo "node_count=0"
  echo "node_summary_fresh=false"
  echo "axnoded_ready=false"
  echo "interface_pool=0/0/0"
  echo "cgroup_pool=0/0/0"
  echo "running_allocation_ids=0"
  echo "active_allocation_ids=0"
  echo "running_containers=0"
  echo "mounted_images=0"
  echo "imagemgr_ready_nodes=0"
  echo "imagefsd_ready_nodes=0"
  echo "volumed_ready_nodes=0"
  echo "volumed_error_nodes=0"
  echo "imported_images=0"
fi
