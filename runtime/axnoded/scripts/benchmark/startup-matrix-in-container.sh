#!/usr/bin/env bash
set -euo pipefail

resolve_verify_docker_platform_local() {
  local arch
  arch="$(uname -m)"
  case "${arch}" in
    x86_64)
      echo "linux/amd64"
      ;;
    aarch64|arm64)
      echo "linux/arm64"
      ;;
    *)
      echo "unsupported startup-matrix container arch: ${arch}" >&2
      return 1
      ;;
  esac
}

STARTUP_MATRIX_SCENARIO="${STARTUP_MATRIX_SCENARIO:?STARTUP_MATRIX_SCENARIO is required}"
STARTUP_MATRIX_MODE="${STARTUP_MATRIX_MODE:-cold}"
STARTUP_MATRIX_SAMPLES="${STARTUP_MATRIX_SAMPLES:-1}"
STARTUP_MATRIX_OMIT_STDIO="${STARTUP_MATRIX_OMIT_STDIO:-true}"

IMAGEMGR_SOCKET="${IMAGEMGR_SOCKET:-/run/imagemgr/imagemgr.sock}"
AXNODED_SOCKET="${AXNODED_SOCKET:-/run/axnoded/axnoded.sock}"
METRICS_URL="${METRICS_URL:-http://127.0.0.1:23001/debug/metricsz}"
INVENTORY_URL="${INVENTORY_URL:-http://127.0.0.1:23001/inventoryz}"
VERIFY_DOCKER_PLATFORM="${VERIFY_DOCKER_PLATFORM:-$(resolve_verify_docker_platform_local)}"

case "${STARTUP_MATRIX_SCENARIO}" in
  runsc-local)
    runtime_name="runsc"
    mount_type="local"
    rootfs_src="local"
    rootfs_path="/opt/sample-rootfs"
    argv_json='["/bin/sh","-c","sleep 1"]'
    ;;
  runc-local)
    runtime_name="runc"
    mount_type="local"
    rootfs_src="local"
    rootfs_path="/opt/sample-rootfs"
    argv_json='["/bin/sh","-c","sleep 1"]'
    ;;
  runsc-oci)
    runtime_name="runsc"
    mount_type="oci"
    rootfs_src="image"
    argv_json='["/bin/sh","-c","sleep 1"]'
    case "${VERIFY_DOCKER_PLATFORM}" in
      linux/amd64)
        image_url="docker.io/library/busybox@sha256:b8d1827e38a1d49cd17217efd7b07d689e4ea1744e39c7dcbb95533d175bea65"
        ;;
      linux/arm64 | linux/arm64/v8)
        image_url="docker.io/library/busybox@sha256:c4e5b27bf840ba1ebd5568b6b914f6926f3559b2ad4f505b1f37aae483b907d6"
        ;;
      *)
        echo "unsupported VERIFY_DOCKER_PLATFORM for startup matrix oci scenario: ${VERIFY_DOCKER_PLATFORM}" >&2
        exit 1
        ;;
    esac
    ;;
  runsc-nydus)
    runtime_name="runsc"
    mount_type="nydus"
    rootfs_src="image"
    argv_json='["/bin/sh","-c","/usr/sbin/nginx -v >/tmp/startup-matrix-nginx-version 2>&1; sleep 1"]'
    case "${VERIFY_DOCKER_PLATFORM}" in
      linux/amd64)
        image_url="ghcr.io/dragonflyoss/image-service/nginx@sha256:e263899b73cfecb68980fbe396dbac8dbabd108397786bed8b423c496500a2a7"
        ;;
      linux/arm64 | linux/arm64/v8)
        image_url="ghcr.io/dragonflyoss/image-service/nginx@sha256:02cde82e5688297fdc6e011b4a4a5535ee106a3d39ecdef8005b45244e39ede2"
        ;;
      *)
        echo "unsupported VERIFY_DOCKER_PLATFORM for startup matrix nydus scenario: ${VERIFY_DOCKER_PLATFORM}" >&2
        exit 1
        ;;
    esac
    ;;
  *)
    echo "unsupported STARTUP_MATRIX_SCENARIO: ${STARTUP_MATRIX_SCENARIO}" >&2
    exit 1
    ;;
esac

node_log="/tmp/startup-matrix-node.log"
/bin/bash /workspace/scripts/verify/node-all-in-one-entrypoint.sh >"${node_log}" 2>&1 &
NODE_PID=$!

cleanup() {
  set +e
  kill "${NODE_PID}" >/dev/null 2>&1 || true
  return 0
}
trap cleanup EXIT

for _ in $(seq 1 60); do
  if [ -S "${AXNODED_SOCKET}" ] && [ -S "${IMAGEMGR_SOCKET}" ] && curl -fsS "http://127.0.0.1:23001/readyz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! [ -S "${AXNODED_SOCKET}" ] || ! [ -S "${IMAGEMGR_SOCKET}" ] || ! curl -fsS "http://127.0.0.1:23001/readyz" >/dev/null 2>&1; then
  cat "${node_log}" >&2 || true
  echo "axnoded node all-in-one did not become ready in time" >&2
  exit 1
fi

cmd=(
  /usr/local/bin/verify-startup
  -address "${AXNODED_SOCKET}"
  -metrics-url "${METRICS_URL}"
  -inventory-url "${INVENTORY_URL}"
  -scenario "${STARTUP_MATRIX_SCENARIO}"
  -mode "${STARTUP_MATRIX_MODE}"
  -samples "${STARTUP_MATRIX_SAMPLES}"
  -omit-stdio="${STARTUP_MATRIX_OMIT_STDIO}"
  -argv-json "${argv_json}"
  -wait-before-delete=true
  -expected-exit 0
  -runtime "${runtime_name}"
  -runtime-id "startup-matrix-${STARTUP_MATRIX_SCENARIO}"
  -mount-type "${mount_type}"
)

case "${rootfs_src}" in
  local)
    cmd+=(-rootfs-src local -rootfs "${rootfs_path}")
    ;;
  image)
    cmd+=(-rootfs-src image -image-url "${image_url}")
    ;;
esac

"${cmd[@]}"
