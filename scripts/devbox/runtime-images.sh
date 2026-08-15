#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEV_DIR="${ROOT_DIR}/.dev"
RUN_DIR="${DEV_DIR}/run"

usage() {
  cat <<'EOF'
Usage:
  scripts/devbox/runtime-images.sh build [python311|server-base|coding-base|desktop-base|claude-code-bundle|codex-bundle...]
  scripts/devbox/runtime-images.sh load  [python311|server-base|coding-base|desktop-base|claude-code-bundle|codex-bundle...]

Defaults to python311 when no image names are provided.
Set DEV_RUNTIME_IMAGE_REBUILD=1 to force rebuilding existing local images.
EOF
}

runtime_ref() {
  case "$1" in
    python311) printf '%s\n' "ghcr.io/cofy-x/axern/python311-runtime:3.11" ;;
    server-base) printf '%s\n' "ghcr.io/cofy-x/axern/server-base-runtime:24.04" ;;
    coding-base) printf '%s\n' "ghcr.io/cofy-x/axern/coding-base-runtime:24.04" ;;
    desktop-base) printf '%s\n' "ghcr.io/cofy-x/axern/desktop-base-runtime:24.04" ;;
    claude-code-bundle) printf '%s\n' "ghcr.io/cofy-x/axern/claude-code-bundle:2.1.205" ;;
    codex-bundle) printf '%s\n' "ghcr.io/cofy-x/axern/codex-bundle:0.144.6" ;;
    *)
      echo "unknown runtime image: $1" >&2
      return 2
      ;;
  esac
}

build_runtime_image() {
  local name="$1"
  local ref
  local image_id
  ref="$(runtime_ref "${name}")"

  if [ "${DEV_RUNTIME_IMAGE_REBUILD:-}" != "1" ] && docker image inspect "${ref}" >/dev/null 2>&1; then
    echo "reusing runtime image ${ref}"
    return 0
  fi

  echo "building runtime image ${ref}"
  case "${name}" in
    python311)
      IMAGE_REF="${ref}" bash "${ROOT_DIR}/runtime/axnoded/scripts/runtime/build-python311-runtime-image.sh"
      ;;
    server-base)
      IMAGE_REF="${ref}" APT_MIRROR_SOURCE="${APT_MIRROR_SOURCE:-archive}" \
        bash "${ROOT_DIR}/runtime/axnoded/scripts/runtime/build-server-base-runtime-image.sh"
      ;;
    coding-base)
      build_runtime_image server-base
      IMAGE_REF="${ref}" SERVER_BASE_RUNTIME_IMAGE="$(runtime_ref server-base)" APT_MIRROR_SOURCE="${APT_MIRROR_SOURCE:-archive}" \
        bash "${ROOT_DIR}/runtime/axnoded/scripts/runtime/build-coding-base-runtime-image.sh"
      ;;
    desktop-base)
      build_runtime_image server-base
      IMAGE_REF="${ref}" SERVER_BASE_RUNTIME_IMAGE="$(runtime_ref server-base)" APT_MIRROR_SOURCE="${APT_MIRROR_SOURCE:-archive}" \
        bash "${ROOT_DIR}/runtime/axnoded/scripts/runtime/build-desktop-base-runtime-image.sh"
      ;;
    claude-code-bundle)
      IMAGE_REF="${ref}" bash "${ROOT_DIR}/runtime/axnoded/scripts/runtime/build-claude-code-bundle-image.sh"
      ;;
    codex-bundle)
      IMAGE_REF="${ref}" bash "${ROOT_DIR}/runtime/axnoded/scripts/runtime/build-codex-bundle-image.sh"
      ;;
  esac
}

load_runtime_image() {
  local name="$1"
  local ref
  ref="$(runtime_ref "${name}")"

  build_runtime_image "${name}"

  if [ ! -S "${RUN_DIR}/imagemgr.sock" ]; then
    echo "standalone imagemgr socket is not running: ${RUN_DIR}/imagemgr.sock" >&2
    echo "start the standalone stack or the Imagefsd/Imagemgr debug services first" >&2
    return 1
  fi

  image_id="$(docker image inspect "${ref}" --format '{{.Id}}')"
  echo "streaming runtime image ${ref} into standalone imagemgr"
  docker image save "${image_id}" | go -C "${ROOT_DIR}/runtime/axnoded" run ./axctl \
    --address "${RUN_DIR}/axnoded.sock" \
    image import \
    --imagemgr-socket "${RUN_DIR}/imagemgr.sock" \
    --file - \
    --ref "${ref}"
}

main() {
  if [ "$(uname -s)" != "Linux" ]; then
    echo "runtime image loading requires the Linux devbox workspace" >&2
    exit 1
  fi
  if [ "$#" -lt 1 ]; then
    usage >&2
    exit 2
  fi

  local command="$1"
  shift
  if [ "$#" -eq 0 ]; then
    set -- python311
  fi

  case "${command}" in
    build)
      for name in "$@"; do
        build_runtime_image "${name}"
      done
      ;;
    load)
      for name in "$@"; do
        load_runtime_image "${name}"
      done
      ;;
    -h|--help|help)
      usage
      ;;
    *)
      usage >&2
      exit 2
      ;;
  esac
}

main "$@"
