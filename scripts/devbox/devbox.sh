#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

usage() {
  cat >&2 <<'EOF'
Usage:
  scripts/devbox/devbox.sh build [--project-dir DIR] [--platform PLATFORM] [--image IMAGE] [--apt-mirror-source archive|aliyun|ustc|tuna] [--build-proxy auto|none|URL]
  scripts/devbox/devbox.sh up [--project-dir DIR] [--platform PLATFORM] [--image IMAGE] [--container-name NAME] [--ssh-port PORT] [--ssh-config-host HOST] [--ssh-config-path PATH] [--apt-mirror-source archive|aliyun|ustc|tuna] [--build-proxy auto|none|URL]
  scripts/devbox/devbox.sh status [--container-name NAME]
  scripts/devbox/devbox.sh down [--container-name NAME]
  scripts/devbox/devbox.sh exec [--container-name NAME] [--project-dir DIR] -- CMD...
  scripts/devbox/devbox.sh shell [--container-name NAME] [--project-dir DIR] [-- CMD...]

Axern's repo-local Linux development container wrapper.
EOF
}

command="${1:-}"
if [ -z "${command}" ]; then
  usage
  exit 2
fi
shift

project_dir="${DEVBOX_PROJECT_DIR:-${ROOT_DIR}}"
platform="${DEVBOX_PLATFORM:-linux/arm64}"
image="${DEVBOX_IMAGE:-axern-devbox:latest-arm64}"
container_name="${DEVBOX_CONTAINER_NAME:-axern-devbox}"
devbox_user="${DEVBOX_USER:-axern}"
ssh_port="${DEVBOX_SSH_PORT:-2222}"
ssh_config_host="${DEVBOX_SSH_CONFIG_HOST:-${container_name}}"
ssh_config_path="${DEVBOX_SSH_CONFIG_PATH:-${HOME:-}/.ssh/config}"
apt_mirror_source="${DEVBOX_APT_MIRROR_SOURCE:-archive}"
build_proxy="${DEVBOX_BUILD_PROXY:-none}"
goproxy="${GOPROXY:-https://proxy.golang.org,direct}"
npm_registry="${NPM_CONFIG_REGISTRY:-https://registry.npmjs.org}"
extra_args=()

while [ "$#" -gt 0 ]; do
  case "$1" in
    --project-dir)
      project_dir="$2"
      shift 2
      ;;
    --platform)
      platform="$2"
      shift 2
      ;;
    --image)
      image="$2"
      shift 2
      ;;
    --container-name)
      container_name="$2"
      if [ -z "${DEVBOX_SSH_CONFIG_HOST:-}" ]; then
        ssh_config_host="${container_name}"
      fi
      shift 2
      ;;
    --ssh-port)
      ssh_port="$2"
      shift 2
      ;;
    --ssh-config-host)
      ssh_config_host="$2"
      shift 2
      ;;
    --ssh-config-path)
      ssh_config_path="$2"
      shift 2
      ;;
    --apt-mirror-source)
      apt_mirror_source="$2"
      shift 2
      ;;
    --build-proxy)
      build_proxy="$2"
      shift 2
      ;;
    --)
      shift
      extra_args=("$@")
      break
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      extra_args+=("$1")
      shift
      ;;
  esac
done

project_dir="$(cd "${project_dir}" && pwd)"

case "${apt_mirror_source}" in
  archive|aliyun|ustc|tuna) ;;
  *)
    echo "unsupported apt mirror source: ${apt_mirror_source}" >&2
    exit 2
    ;;
esac

ensure_docker() {
  command -v docker >/dev/null 2>&1 || {
    echo "docker is required for Axern devbox" >&2
    exit 1
  }
}

container_id() {
  docker ps -aq --filter "name=^/${container_name}$"
}

container_running() {
  docker ps -q --filter "name=^/${container_name}$" | grep -q .
}

detect_build_proxy() {
  case "${build_proxy}" in
    ""|none|off|0)
      return
      ;;
    auto)
      if command -v nc >/dev/null 2>&1 && nc -z 127.0.0.1 7890 >/dev/null 2>&1; then
        printf 'http://host.docker.internal:7890'
      fi
      ;;
    *)
      printf '%s' "${build_proxy}"
      ;;
  esac
}

build_image() {
  build_args=(
    --platform "${platform}"
    --build-arg "APT_MIRROR_SOURCE=${apt_mirror_source}"
    --build-arg "GOPROXY=${goproxy}"
    --build-arg "NPM_CONFIG_REGISTRY=${npm_registry}"
    -f "${ROOT_DIR}/docker/devbox/Dockerfile"
    -t "${image}"
  )

  proxy_url="$(detect_build_proxy)"
  if [ -n "${proxy_url}" ]; then
    build_args+=(--add-host "host.docker.internal:host-gateway")
    build_args+=(
      --build-arg "HTTP_PROXY=${proxy_url}"
      --build-arg "HTTPS_PROXY=${proxy_url}"
      --build-arg "http_proxy=${proxy_url}"
      --build-arg "https_proxy=${proxy_url}"
    )
    echo "Using Docker build proxy: ${proxy_url}"
  fi

  docker build "${build_args[@]}" "${ROOT_DIR}"
}

build_image_if_missing() {
  if docker image inspect "${image}" >/dev/null 2>&1; then
    return
  fi

  echo "Building missing devbox image ${image} for ${platform}..."
  build_image
}

write_ssh_config() {
  if [ -z "${ssh_config_path}" ] || [ "${ssh_config_path}" = "none" ]; then
    return
  fi

  ssh_config_dir="$(dirname "${ssh_config_path}")"
  install -d -m 0700 "${ssh_config_dir}"
  touch "${ssh_config_path}"
  chmod 0600 "${ssh_config_path}"

  block_begin="# >>> axern devbox: ${ssh_config_host}"
  block_end="# <<< axern devbox: ${ssh_config_host}"
  tmp_config="$(mktemp)"
  identity_files=()

  if [ -d "${HOME:-}/.ssh" ]; then
    for public_key in "${HOME}/.ssh"/*.pub; do
      private_key="${public_key%.pub}"
      if [ -f "${private_key}" ]; then
        identity_files+=("${private_key}")
      fi
    done
  fi

  awk -v begin="${block_begin}" -v end="${block_end}" '
    $0 == begin { skip = 1; next }
    $0 == end { skip = 0; next }
    skip != 1 { print }
  ' "${ssh_config_path}" > "${tmp_config}"

  {
    printf '%s\n' "${block_begin}"
    printf 'Host %s\n' "${ssh_config_host}"
    printf '  HostName 127.0.0.1\n'
    printf '  Port %s\n' "${ssh_port}"
    printf '  User %s\n' "${devbox_user}"
    printf '  UserKnownHostsFile /dev/null\n'
    printf '  StrictHostKeyChecking no\n'
    printf '  PubkeyAuthentication yes\n'
    printf '  PasswordAuthentication no\n'
    printf '  PreferredAuthentications publickey\n'
    if [ "${#identity_files[@]}" -gt 0 ]; then
      printf '  IdentitiesOnly yes\n'
      for identity_file in "${identity_files[@]}"; do
        printf '  IdentityFile %s\n' "${identity_file}"
      done
    fi
    printf '%s\n' "${block_end}"
    cat "${tmp_config}"
  } > "${tmp_config}.new"

  mv "${tmp_config}.new" "${ssh_config_path}"
  rm -f "${tmp_config}"
  chmod 0600 "${ssh_config_path}"
}

write_authorized_keys_bundle() {
  authorized_keys_file=""

  if [ -z "${HOME:-}" ] || [ ! -d "${HOME}/.ssh" ]; then
    return
  fi

  install -d -m 0700 "${project_dir}/.dev/devbox"
  tmp_keys="$(mktemp)"

  if [ -f "${HOME}/.ssh/authorized_keys" ]; then
    cat "${HOME}/.ssh/authorized_keys" >> "${tmp_keys}"
  fi

  for public_key in "${HOME}/.ssh"/*.pub; do
    private_key="${public_key%.pub}"
    if [ -f "${public_key}" ] && [ -f "${private_key}" ]; then
      cat "${public_key}" >> "${tmp_keys}"
    fi
  done

  if [ ! -s "${tmp_keys}" ]; then
    rm -f "${tmp_keys}"
    return
  fi

  authorized_keys_file="${project_dir}/.dev/devbox/authorized_keys"
  sort -u "${tmp_keys}" > "${authorized_keys_file}"
  rm -f "${tmp_keys}"
  chmod 0600 "${authorized_keys_file}"
}

print_connection_hints() {
  echo "SSH: ssh ${ssh_config_host}"
  echo "VS Code Remote-SSH host: ${ssh_config_host}"
  if command -v code >/dev/null 2>&1; then
    echo "VS Code: code --remote ssh-remote+${ssh_config_host} ${project_dir}"
  fi
}

case "${command}" in
  build)
    ensure_docker
    build_image
    ;;
  up)
    ensure_docker
    build_image_if_missing

    if container_running; then
      write_ssh_config
      echo "Axern devbox is already running: ${container_name}"
      print_connection_hints
      echo "Enter it with: ${ROOT_DIR}/scripts/devbox/devbox.sh shell --container-name ${container_name}"
      exit 0
    fi

    existing_id="$(container_id)"
    if [ -n "${existing_id}" ]; then
      docker rm "${container_name}" >/dev/null
    fi

    write_authorized_keys_bundle

    run_args=(
      -d
      --name "${container_name}"
      --hostname "${container_name}"
      --platform "${platform}"
      --privileged
      --add-host "host.docker.internal:host-gateway"
      --mount "type=bind,source=${project_dir},target=${project_dir}"
      --mount "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock"
      --workdir "${project_dir}"
      --env "GOTOOLCHAIN=local"
      --env "GOPROXY=${goproxy}"
      --env "NPM_CONFIG_REGISTRY=${npm_registry}"
      --env "AXERN_DEV_WORKSPACE=${project_dir}"
      --env "AXERN_ENDPOINT=127.0.0.1:25000"
      --env "AXERN_PROXY_MODE=direct"
      --env "AXERN_TLS_CA_CERT=${project_dir}/.dev/certs/ca.crt"
      --env "AXERN_TLS_CERT=${project_dir}/.dev/certs/client.crt"
      --env "AXERN_TLS_KEY=${project_dir}/.dev/certs/client.key"
      --env "AXNODED_SOCKET=${project_dir}/.dev/run/axnoded.sock"
      --env "IMAGEMGR_SOCKET=${project_dir}/.dev/run/imagemgr.sock"
      --env "VOLUMED_SOCKET=${project_dir}/.dev/run/volumed.sock"
      --publish "127.0.0.1:${ssh_port}:22"
    )

    if [ -n "${authorized_keys_file}" ]; then
      run_args+=(
        --mount "type=bind,source=${authorized_keys_file},target=/host-ssh-authorized-keys,readonly"
      )
    fi

    run_args+=("${image}")

    docker run "${run_args[@]}" >/dev/null

    write_ssh_config
    echo "Started Axern devbox: ${container_name}"
    echo "Project: ${project_dir}"
    echo "Image: ${image}"
    echo "Platform: ${platform}"
    print_connection_hints
    echo "Enter it with: ${ROOT_DIR}/scripts/devbox/devbox.sh shell --container-name ${container_name}"
    ;;
  status)
    ensure_docker
    if ! docker ps -a --filter "name=^/${container_name}$" --format '{{.Names}}' | grep -qx "${container_name}"; then
      echo "Axern devbox is not created: ${container_name}"
      exit 0
    fi

    docker inspect "${container_name}" \
      --format 'Name: {{.Name}}
Status: {{.State.Status}}
Image: {{.Config.Image}}
Platform: {{.Platform}}
Workdir: {{.Config.WorkingDir}}'
    ;;
  down)
    ensure_docker
    if container_running; then
      docker stop "${container_name}" >/dev/null
    fi
    if [ -n "$(container_id)" ]; then
      docker rm "${container_name}" >/dev/null
      echo "Removed Axern devbox: ${container_name}"
    else
      echo "Axern devbox is not created: ${container_name}"
    fi
    ;;
  shell)
    ensure_docker
    if ! container_running; then
      echo "Axern devbox is not running: ${container_name}" >&2
      echo "Start it with: make devbox-up" >&2
      exit 1
    fi

    if [ "${#extra_args[@]}" -eq 0 ]; then
      extra_args=(bash)
    fi

    docker exec -it \
      --user "${devbox_user}" \
      --workdir "${project_dir}" \
      "${container_name}" \
      "${extra_args[@]}"
    ;;
  exec)
    ensure_docker
    if ! container_running; then
      echo "Axern devbox is not running: ${container_name}" >&2
      echo "Start it with: make devbox-up" >&2
      exit 1
    fi

    if [ "${#extra_args[@]}" -eq 0 ]; then
      echo "devbox exec requires a command" >&2
      exit 2
    fi

    docker exec \
      --user "${devbox_user}" \
      --workdir "${project_dir}" \
      "${container_name}" \
      "${extra_args[@]}"
    ;;
  *)
    usage
    exit 2
    ;;
esac
