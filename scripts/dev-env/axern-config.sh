#!/usr/bin/env bash

ensure_secrets_master_key() {
  local env_name="$1"
  local key_file
  key_file="$(secrets_master_key_file "${env_name}")"
  if [ ! -s "${key_file}" ]; then
    openssl rand -hex 16 > "${key_file}"
  fi
}

generate_compose_certs() {
  AXERN_TLS_SERVER_DNS_NAMES="localhost,host.docker.internal,controld,tunneld,gatewayd,mock-provider,registry" \
    AXERN_TLS_SERVER_IPS="127.0.0.1" \
    bash "${AXERN_ROOT}/scripts/dev-mtls-certs.sh" "${COMPOSE_STATE_DIR}/certs" >/dev/null
}

ensure_compose_ssh_keys() {
 require_cmd ssh-keygen
 local ssh_dir host_key client_key
  ssh_dir="${COMPOSE_STATE_DIR}/ssh"
  host_key="${ssh_dir}/gateway_host_ed25519"
  client_key="${ssh_dir}/gateway_client_ed25519"
  mkdir -p "${ssh_dir}"
  if [ ! -s "${host_key}" ]; then
    ssh-keygen -q -t ed25519 -N "" -f "${host_key}" -C "axern-local-gatewayd" >/dev/null
  fi
  if [ ! -s "${client_key}" ]; then
    ssh-keygen -q -t ed25519 -N "" -f "${client_key}" -C "axern-local-client" >/dev/null
  fi
  cat "${client_key}.pub" > "${ssh_dir}/authorized_keys"
  chmod 700 "${ssh_dir}"
  chmod 600 "${host_key}" "${client_key}" "${ssh_dir}/authorized_keys"
}

ensure_k8s_ssh_keys() {
  require_cmd ssh-keygen
  local ssh_dir host_key client_key
  ssh_dir="${K8S_STATE_DIR}/ssh"
  host_key="${ssh_dir}/gateway_host_ed25519"
  client_key="${ssh_dir}/gateway_client_ed25519"
  mkdir -p "${ssh_dir}"
  if [ ! -s "${host_key}" ]; then
    ssh-keygen -q -t ed25519 -N "" -f "${host_key}" -C "axern-${K8S_ENV_NAME}-gatewayd" >/dev/null
  fi
  if [ ! -s "${client_key}" ]; then
    ssh-keygen -q -t ed25519 -N "" -f "${client_key}" -C "axern-${K8S_ENV_NAME}-client" >/dev/null
  fi
  cat "${client_key}.pub" > "${ssh_dir}/authorized_keys"
  chmod 700 "${ssh_dir}"
  chmod 600 "${host_key}" "${client_key}" "${ssh_dir}/authorized_keys"
}

generate_k8s_certs() {
  AXERN_TLS_SERVER_DNS_NAMES="localhost,host.docker.internal,controld,controld.${K8S_NAMESPACE},controld.${K8S_NAMESPACE}.svc,controld.${K8S_NAMESPACE}.svc.cluster.local,tunneld,tunneld.${K8S_NAMESPACE},tunneld.${K8S_NAMESPACE}.svc,tunneld.${K8S_NAMESPACE}.svc.cluster.local,tunneld-a,tunneld-a.${K8S_NAMESPACE},tunneld-a.${K8S_NAMESPACE}.svc,tunneld-a.${K8S_NAMESPACE}.svc.cluster.local,tunneld-b,tunneld-b.${K8S_NAMESPACE},tunneld-b.${K8S_NAMESPACE}.svc,tunneld-b.${K8S_NAMESPACE}.svc.cluster.local,gatewayd,gatewayd.${K8S_NAMESPACE},gatewayd.${K8S_NAMESPACE}.svc,gatewayd.${K8S_NAMESPACE}.svc.cluster.local" \
    AXERN_TLS_SERVER_IPS="127.0.0.1" \
    bash "${AXERN_ROOT}/scripts/dev-mtls-certs.sh" "${K8S_STATE_DIR}/certs" >/dev/null
}

write_cli_env() {
  local env_name="$1"
  local endpoint="$2"
  local cert_dir="${STATE_ROOT}/${env_name}/certs"
  local service_url ssh_endpoint ssh_identity_file
  local config_file
  service_url="$(axern_context_service_url "${env_name}")"
  ssh_endpoint="$(axern_context_ssh_endpoint "${env_name}")"
  ssh_identity_file="$(axern_context_ssh_identity_file "${env_name}")"
  config_file="$(axern_config_file)"
  cat > "$(cli_env_file "${env_name}")" <<EOF
export AXERN_CONFIG="${config_file}"
export AXERN_ENDPOINT="${endpoint}"
export AXERN_SERVICE_URL="${service_url}"
export AXERN_SSH_ENDPOINT="${ssh_endpoint}"
export AXERN_SSH_IDENTITY_FILE="${ssh_identity_file}"
export AXERN_TLS_CA_CERT="${cert_dir}/ca.crt"
export AXERN_TLS_CERT="${cert_dir}/client.crt"
export AXERN_TLS_KEY="${cert_dir}/client.key"
EOF
  write_axern_context "${env_name}" "${endpoint}" "${cert_dir}" "${service_url}" "${ssh_endpoint}" "${ssh_identity_file}"
}

write_axern_context() {
  upsert_axern_context "$1" "$2" "$3" true "${4:-}" "${5:-}" "${6:-}"
}

upsert_axern_context() {
  local env_name="$1"
  local endpoint="$2"
  local cert_dir="$3"
  local set_current="${4:-false}"
  local service_url="${5:-}"
  local ssh_endpoint="${6:-}"
  local ssh_identity_file="${7:-}"
  local config_file
  config_file="$(axern_config_file)"
  mkdir -p "$(dirname "${config_file}")"
  python3 - "${config_file}" "${env_name}" "${endpoint}" "${cert_dir}/ca.crt" "${cert_dir}/client.crt" "${cert_dir}/client.key" "${set_current}" "${service_url}" "${ssh_endpoint}" "${ssh_identity_file}" <<'PY'
import json
import pathlib
import sys

config_path = pathlib.Path(sys.argv[1])
context_name = sys.argv[2]
endpoint = sys.argv[3]
ca_cert = sys.argv[4]
client_cert = sys.argv[5]
client_key = sys.argv[6]
set_current = sys.argv[7].lower() == "true"
service_url = sys.argv[8]
ssh_endpoint = sys.argv[9]
ssh_identity_file = sys.argv[10]

if config_path.exists():
    data = json.loads(config_path.read_text())
else:
    data = {}

contexts = data.get("contexts")
if not isinstance(contexts, dict):
    contexts = {}

context = {
    "endpoint": endpoint,
    "tls": {
        "ca_cert": ca_cert,
        "cert": client_cert,
        "key": client_key,
    },
    "proxy_mode": "direct",
}
if service_url:
    context["service_url"] = service_url
if ssh_endpoint:
    context["ssh_endpoint"] = ssh_endpoint
if ssh_identity_file:
    context["ssh_identity_file"] = ssh_identity_file
contexts[context_name] = context

data["contexts"] = contexts
if set_current or not data.get("current_context"):
    data["current_context"] = context_name
config_path.write_text(json.dumps(data, indent=2) + "\n")
config_path.chmod(0o600)
PY
}

axern_context_endpoint() {
  case "$1" in
    compose)
      printf '127.0.0.1:%s\n' "${COMPOSE_GATEWAY_CONTROL_PORT}"
      ;;
    kind)
      if [ "${K8S_ENV_NAME}" = "kind" ]; then
        printf '127.0.0.1:%s\n' "${K8S_GATEWAY_LOCAL_CONTROL_PORT}"
      else
        printf '127.0.0.1:%s\n' "$(k8s_default_gateway_local_control_port kind)"
      fi
      ;;
    k8s)
      printf '127.0.0.1:%s\n' "${K8S_GATEWAY_LOCAL_CONTROL_PORT}"
      ;;
    *)
      echo "unknown axern context environment: $1" >&2
      return 1
      ;;
  esac
}

axern_context_service_url() {
  case "$1" in
    compose)
      printf 'http://127.0.0.1:%s\n' "${COMPOSE_GATEWAY_HTTP_PORT}"
      ;;
    kind)
      if [ "${K8S_ENV_NAME}" = "kind" ]; then
        printf 'http://127.0.0.1:%s\n' "${K8S_GATEWAY_LOCAL_HTTP_PORT}"
      else
        printf 'http://127.0.0.1:%s\n' "$(k8s_default_gateway_local_http_port kind)"
      fi
      ;;
    k8s)
      printf 'http://127.0.0.1:%s\n' "${K8S_GATEWAY_LOCAL_HTTP_PORT}"
      ;;
    *)
      printf '\n'
      ;;
  esac
}

axern_context_ssh_endpoint() {
  case "$1" in
    compose)
      printf '127.0.0.1:%s\n' "${COMPOSE_GATEWAY_SSH_PORT}"
      ;;
    kind|k8s)
      printf '127.0.0.1:%s\n' "${K8S_GATEWAY_LOCAL_SSH_PORT}"
      ;;
    *)
      printf '\n'
      ;;
  esac
}

axern_context_state_dir() {
  case "$1" in
    compose|kind|k8s)
      printf '%s/%s\n' "${STATE_ROOT}" "$1"
      ;;
    *)
      printf '\n'
      ;;
  esac
}

axern_context_ssh_identity_file() {
  local state_dir
  state_dir="$(axern_context_state_dir "$1")"
  [ -n "${state_dir}" ] || return 0
  printf '%s/ssh/gateway_client_ed25519\n' "${state_dir}"
}

sync_local_axern_contexts() {
  local env_name cert_dir
  for env_name in compose kind; do
    cert_dir="${STATE_ROOT}/${env_name}/certs"
    if [ -f "${cert_dir}/ca.crt" ] && [ -f "${cert_dir}/client.crt" ] && [ -f "${cert_dir}/client.key" ]; then
      upsert_axern_context \
        "${env_name}" \
		"$(axern_context_endpoint "${env_name}")" \
		"${cert_dir}" \
		false \
		"$(axern_context_service_url "${env_name}")" \
		"$(axern_context_ssh_endpoint "${env_name}")" \
		"$(axern_context_ssh_identity_file "${env_name}")"
    fi
  done
}
