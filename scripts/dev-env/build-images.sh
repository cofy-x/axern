#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
set -euo pipefail

AXERN_DEV_ENV_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=./lib.sh
source "${AXERN_DEV_ENV_ROOT}/scripts/dev-env/lib.sh"
# shellcheck source=./docker-build-cache.sh
source "${AXERN_DEV_ENV_ROOT}/scripts/dev-env/docker-build-cache.sh"
# shellcheck source=../../runtime/axnoded/scripts/lib/verify-docker-common.sh
source "${AXERN_DEV_ENV_ROOT}/runtime/axnoded/scripts/lib/verify-docker-common.sh"

require_cmd docker
begin_named_lock "images-build"
trap 'end_named_lock "images-build"' EXIT

image_scope="${AXERN_LOCAL_IMAGE_SCOPE:-all}"
build_node_runtime_base=false
build_runtime_core=false
build_full_runtime_catalog=false
build_control_stack=false
build_tunneld=false
build_node_image=false
case "${image_scope}" in
  all)
    build_node_runtime_base=true
    build_runtime_core=true
    build_full_runtime_catalog=true
    build_control_stack=true
    build_tunneld=true
    build_node_image=true
    ;;
  control)
    build_control_stack=true
    ;;
  managed-rollout)
    build_node_runtime_base=true
    build_runtime_core=true
    build_control_stack=true
    build_tunneld=true
    build_node_image=true
    ;;
  node)
    build_node_runtime_base=true
    build_node_image=true
    ;;
  *)
    echo "AXERN_LOCAL_IMAGE_SCOPE must be all, control, managed-rollout, or node" >&2
    exit 2
    ;;
esac

if [ "${build_control_stack}" = "true" ] || [ "${build_tunneld}" = "true" ]; then
  go_bin="$(axern_go_bin)"
  require_cmd "${go_bin}"
fi

push_image_after_build() {
  local image_ref="$1"
  if [ "${AXERN_DOCKER_PUSH_AFTER_BUILD:-}" = "1" ]; then
    docker push "${image_ref}"
  fi
}

report_image_build_phase() {
  local phase="$1"
  local started_at="$2"
  local duration_seconds="$(( $(date +%s) - started_at ))"
  printf 'image_build_phase=%s duration_seconds=%s\n' "${phase}" "${duration_seconds}"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    printf '| %s | %s |\n' "${phase}" "${duration_seconds}" >> "${GITHUB_STEP_SUMMARY}"
  fi
}

if [ "${build_node_runtime_base}" = "true" ]; then
  phase_started_at="$(date +%s)"
  build_node_runtime_base_image "${APT_MIRROR_SOURCE}" "${CARGO_REGISTRY_SOURCE}"
  push_image_after_build "${NODE_RUNTIME_BASE_IMAGE_TAG}"
  report_image_build_phase "node-runtime-base" "${phase_started_at}"
fi

if [ "${build_runtime_core}" = "true" ]; then
  phase_started_at="$(date +%s)"
  IMAGE_REF="${PYTHON311_RUNTIME_IMAGE}" bash "${AXERN_DEV_ENV_ROOT}/runtime/axnoded/scripts/runtime/build-python311-runtime-image.sh" >/dev/null
  push_image_after_build "${PYTHON311_RUNTIME_IMAGE}"
  IMAGE_REF="${SERVER_BASE_RUNTIME_IMAGE}" APT_MIRROR_SOURCE="${APT_MIRROR_SOURCE}" bash "${AXERN_DEV_ENV_ROOT}/runtime/axnoded/scripts/runtime/build-server-base-runtime-image.sh" >/dev/null
  push_image_after_build "${SERVER_BASE_RUNTIME_IMAGE}"
  IMAGE_REF="${CODING_BASE_RUNTIME_IMAGE}" SERVER_BASE_RUNTIME_IMAGE="${SERVER_BASE_RUNTIME_IMAGE}" APT_MIRROR_SOURCE="${APT_MIRROR_SOURCE}" bash "${AXERN_DEV_ENV_ROOT}/runtime/axnoded/scripts/runtime/build-coding-base-runtime-image.sh" >/dev/null
  push_image_after_build "${CODING_BASE_RUNTIME_IMAGE}"
  IMAGE_REF="${CODEX_BUNDLE_IMAGE}" bash "${AXERN_DEV_ENV_ROOT}/runtime/axnoded/scripts/runtime/build-codex-bundle-image.sh"
  push_image_after_build "${CODEX_BUNDLE_IMAGE}"
  IMAGE_REF="${CLAUDE_CODE_BUNDLE_IMAGE}" bash "${AXERN_DEV_ENV_ROOT}/runtime/axnoded/scripts/runtime/build-claude-code-bundle-image.sh"
  push_image_after_build "${CLAUDE_CODE_BUNDLE_IMAGE}"
  report_image_build_phase "runtime-core" "${phase_started_at}"
fi

if [ "${build_full_runtime_catalog}" = "true" ]; then
  phase_started_at="$(date +%s)"
  IMAGE_REF="${DESKTOP_BASE_RUNTIME_IMAGE}" SERVER_BASE_RUNTIME_IMAGE="${SERVER_BASE_RUNTIME_IMAGE}" APT_MIRROR_SOURCE="${APT_MIRROR_SOURCE}" bash "${AXERN_DEV_ENV_ROOT}/runtime/axnoded/scripts/runtime/build-desktop-base-runtime-image.sh" >/dev/null
  push_image_after_build "${DESKTOP_BASE_RUNTIME_IMAGE}"
  report_image_build_phase "runtime-catalog" "${phase_started_at}"
fi

if [ "${build_control_stack}" = "true" ] || [ "${build_tunneld}" = "true" ]; then
  phase_started_at="$(date +%s)"
fi
if [ "${build_control_stack}" = "true" ]; then
  mkdir -p "${AXERN_DEV_ENV_ROOT}/deploy/images/controld/.build"
  mkdir -p "${AXERN_DEV_ENV_ROOT}/deploy/images/gatewayd/.build"
fi
if [ "${build_tunneld}" = "true" ]; then
  mkdir -p "${AXERN_DEV_ENV_ROOT}/deploy/images/tunneld/.build"
fi
if [ "${build_control_stack}" = "true" ] || [ "${build_tunneld}" = "true" ]; then
  case "${AXERN_TARGET_GOARCH:-$(uname -m)}" in
    arm64|aarch64)
      CONTROLD_GOARCH="arm64"
      ;;
    x86_64|amd64)
      CONTROLD_GOARCH="amd64"
      ;;
    *)
      echo "unsupported host architecture for local application images: $(uname -m)" >&2
      exit 1
      ;;
  esac
fi
if [ "${build_control_stack}" = "true" ]; then
  rm -rf "${AXERN_DEV_ENV_ROOT}/deploy/images/gatewayd/.build/dashboard-vendor"
  (
    cd "${AXERN_DEV_ENV_ROOT}" && \
      GOTOOLCHAIN=local GOFLAGS='' "${go_bin}" run ./gateway/gatewayd/cmd/dashassets \
        -vendor-dir "${AXERN_DEV_ENV_ROOT}/deploy/images/gatewayd/.build/dashboard-vendor"
  )
  (
    cd "${AXERN_DEV_ENV_ROOT}" && \
      GOOS=linux GOARCH="${CONTROLD_GOARCH}" CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS='' \
        "${go_bin}" build -o "${AXERN_DEV_ENV_ROOT}/deploy/images/controld/.build/controld" ./control/controld/cmd/controld
      GOOS=linux GOARCH="${CONTROLD_GOARCH}" CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS='' \
        "${go_bin}" build -o "${AXERN_DEV_ENV_ROOT}/deploy/images/controld/.build/controld-migrate" ./control/controld/cmd/migrate
      GOOS=linux GOARCH="${CONTROLD_GOARCH}" CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS='' \
        "${go_bin}" build -o "${AXERN_DEV_ENV_ROOT}/deploy/images/controld/.build/controld-access-bootstrap" ./control/controld/cmd/access-bootstrap
      GOOS=linux GOARCH="${CONTROLD_GOARCH}" CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS='' \
        "${go_bin}" build -o "${AXERN_DEV_ENV_ROOT}/deploy/images/controld/.build/controld-retention" ./control/controld/cmd/retention
      GOOS=linux GOARCH="${CONTROLD_GOARCH}" CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS='' \
        "${go_bin}" build -o "${AXERN_DEV_ENV_ROOT}/deploy/images/controld/.build/storaged" ./control/storaged/cmd/storaged
      GOOS=linux GOARCH="${CONTROLD_GOARCH}" CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS='' \
        "${go_bin}" build -o "${AXERN_DEV_ENV_ROOT}/deploy/images/controld/.build/axrun" ./apps/axrun
      GOOS=linux GOARCH="${CONTROLD_GOARCH}" CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS='' \
        "${go_bin}" build -o "${AXERN_DEV_ENV_ROOT}/deploy/images/gatewayd/.build/gatewayd" ./gateway/gatewayd
  )
fi

if [ "${build_tunneld}" = "true" ]; then
  (
    cd "${AXERN_DEV_ENV_ROOT}" && \
      GOOS=linux GOARCH="${CONTROLD_GOARCH}" CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS='' \
        "${go_bin}" build -o "${AXERN_DEV_ENV_ROOT}/deploy/images/tunneld/.build/tunneld" ./runtime/tunneld/cmd/tunneld
  )
fi
if [ "${build_control_stack}" = "true" ] || [ "${build_tunneld}" = "true" ]; then
  report_image_build_phase "application-binaries" "${phase_started_at}"
fi

if [ "${build_control_stack}" = "true" ]; then
  phase_started_at="$(date +%s)"
  axern_docker_build \
    -f "${AXERN_DEV_ENV_ROOT}/deploy/images/controld/Dockerfile" \
    --build-arg "APT_MIRROR_BASE_URL=${APT_MIRROR_BASE_URL:-}" \
    -t "${CONTROLD_IMAGE}" \
    "${AXERN_DEV_ENV_ROOT}"
  push_image_after_build "${CONTROLD_IMAGE}"

  axern_docker_build \
    -f "${AXERN_DEV_ENV_ROOT}/deploy/images/gatewayd/Dockerfile" \
    --build-arg "APT_MIRROR_BASE_URL=${APT_MIRROR_BASE_URL:-}" \
    -t "${GATEWAYD_IMAGE}" \
    "${AXERN_DEV_ENV_ROOT}"
  push_image_after_build "${GATEWAYD_IMAGE}"
  report_image_build_phase "control-images" "${phase_started_at}"
fi

if [ "${build_tunneld}" = "true" ]; then
  phase_started_at="$(date +%s)"
  axern_docker_build \
    -f "${AXERN_DEV_ENV_ROOT}/deploy/images/tunneld/Dockerfile" \
    -t "${TUNNELD_IMAGE}" \
    "${AXERN_DEV_ENV_ROOT}"
  push_image_after_build "${TUNNELD_IMAGE}"
  report_image_build_phase "tunneld-image" "${phase_started_at}"
fi

if [ "${build_node_image}" = "true" ]; then
  phase_started_at="$(date +%s)"
  axern_docker_build \
    -f "${AXERN_DEV_ENV_ROOT}/deploy/images/node-all-in-one/Dockerfile" \
    --build-arg "NODE_RUNTIME_BASE_IMAGE=${NODE_RUNTIME_BASE_IMAGE_TAG}" \
    --build-arg "NODE_RUNTIME_BASE_IMAGE_ID=$(docker image inspect "${NODE_RUNTIME_BASE_IMAGE_TAG}" --format '{{.Id}}')" \
    -t "${NODE_ALL_IN_ONE_IMAGE}" \
    "${AXERN_DEV_ENV_ROOT}"
  push_image_after_build "${NODE_ALL_IN_ONE_IMAGE}"
  report_image_build_phase "node-image" "${phase_started_at}"
fi

if [ "${build_runtime_core}" = "true" ]; then
  docker image inspect \
    "${PYTHON311_RUNTIME_IMAGE}" \
    "${SERVER_BASE_RUNTIME_IMAGE}" \
    "${CODING_BASE_RUNTIME_IMAGE}" \
    "${CODEX_BUNDLE_IMAGE}" \
    "${CLAUDE_CODE_BUNDLE_IMAGE}" >/dev/null
fi
if [ "${build_full_runtime_catalog}" = "true" ]; then
  docker image inspect \
    "${DESKTOP_BASE_RUNTIME_IMAGE}" >/dev/null
fi
if [ "${build_tunneld}" = "true" ]; then
  docker image inspect "${TUNNELD_IMAGE}" >/dev/null
fi
if [ "${build_node_image}" = "true" ]; then
  docker image inspect "${NODE_ALL_IN_ONE_IMAGE}" >/dev/null
fi

echo "local_images_ready=true"
echo "local_image_scope=${image_scope}"
echo "controld_image=${CONTROLD_IMAGE}"
echo "tunneld_image=${TUNNELD_IMAGE}"
echo "gatewayd_image=${GATEWAYD_IMAGE}"
echo "node_runtime_base_image=${NODE_RUNTIME_BASE_IMAGE_TAG}"
echo "node_all_in_one_image=${NODE_ALL_IN_ONE_IMAGE}"
echo "python311_runtime_image=${PYTHON311_RUNTIME_IMAGE}"
echo "server_base_runtime_image=${SERVER_BASE_RUNTIME_IMAGE}"
echo "coding_base_runtime_image=${CODING_BASE_RUNTIME_IMAGE}"
echo "desktop_base_runtime_image=${DESKTOP_BASE_RUNTIME_IMAGE}"
echo "claude_code_bundle_image=${CLAUDE_CODE_BUNDLE_IMAGE}"
echo "codex_bundle_image=${CODEX_BUNDLE_IMAGE}"
