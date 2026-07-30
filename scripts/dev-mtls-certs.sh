#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="${1:-${ROOT_DIR}/.dev/certs}"
SERVER_DNS_NAMES="${AXERN_TLS_SERVER_DNS_NAMES:-localhost,host.docker.internal}"
SERVER_IPS="${AXERN_TLS_SERVER_IPS:-127.0.0.1}"

mkdir -p "${OUT_DIR}"

ca_key="${OUT_DIR}/ca.key"
ca_crt="${OUT_DIR}/ca.crt"
server_key="${OUT_DIR}/controld.key"
server_crt="${OUT_DIR}/controld.crt"
gateway_key="${OUT_DIR}/gatewayd.key"
gateway_crt="${OUT_DIR}/gatewayd.crt"
tunnel_key="${OUT_DIR}/tunneld.key"
tunnel_crt="${OUT_DIR}/tunneld.crt"
client_key="${OUT_DIR}/client.key"
client_crt="${OUT_DIR}/client.crt"
node_key="${OUT_DIR}/node.key"
node_crt="${OUT_DIR}/node.crt"
rollout_worker_key="${OUT_DIR}/rollout-worker.key"
rollout_worker_crt="${OUT_DIR}/rollout-worker.crt"

cert_matches_requested_names() {
  local name
  IFS=',' read -r -a dns_names <<<"${SERVER_DNS_NAMES}"
  for name in "${dns_names[@]}"; do
    name="$(printf '%s' "${name}" | xargs)"
    if [ -n "${name}" ] && ! openssl x509 -in "${server_crt}" -noout -checkhost "${name}" >/dev/null 2>&1; then
      return 1
    fi
  done
  IFS=',' read -r -a ip_names <<<"${SERVER_IPS}"
  for name in "${ip_names[@]}"; do
    name="$(printf '%s' "${name}" | xargs)"
    if [ -n "${name}" ] && ! openssl x509 -in "${server_crt}" -noout -checkip "${name}" >/dev/null 2>&1; then
      return 1
    fi
  done
  if ! openssl x509 -in "${gateway_crt}" -noout -purpose 2>/dev/null | grep -q '^SSL client : Yes'; then
    return 1
  fi
  if ! openssl x509 -in "${node_crt}" -noout -subject -nameopt RFC2253 2>/dev/null | grep -Eq '^subject= ?CN=axern-node$'; then
    return 1
  fi
  return 0
}

if [ -s "${ca_crt}" ] && [ -s "${server_crt}" ] && [ -s "${gateway_crt}" ] && [ -s "${tunnel_crt}" ] && [ -s "${client_crt}" ] && [ -s "${node_crt}" ] && [ -s "${rollout_worker_crt}" ] && cert_matches_requested_names; then
  echo "dev_mtls_certs_dir=${OUT_DIR}"
  exit 0
fi

rm -f "${ca_key}" "${ca_crt}" "${server_key}" "${server_crt}" "${gateway_key}" "${gateway_crt}" "${tunnel_key}" "${tunnel_crt}" "${client_key}" "${client_crt}" "${node_key}" "${node_crt}" "${rollout_worker_key}" "${rollout_worker_crt}" "${OUT_DIR}/ca.srl"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "${ca_key}" \
  -out "${ca_crt}" \
  -days 3650 \
  -subj "/CN=axern-dev-ca" >/dev/null 2>&1

make_server_cert() {
  local name="$1"
  local key="$2"
  local crt="$3"
  local extended_key_usage="${4:-serverAuth}"
  cat > "${tmp_dir}/${name}.cnf" <<EOF
[req]
distinguished_name=req_distinguished_name
req_extensions=v3_req
prompt=no
[req_distinguished_name]
CN=${name}
[v3_req]
keyUsage=keyEncipherment,digitalSignature
extendedKeyUsage=${extended_key_usage}
subjectAltName=@alt_names
[alt_names]
EOF

  local dns_index=1
  local dns_name
  IFS=',' read -r -a dns_names <<<"${SERVER_DNS_NAMES},${name}"
  for dns_name in "${dns_names[@]}"; do
    dns_name="$(printf '%s' "${dns_name}" | xargs)"
    if [ -z "${dns_name}" ]; then
      continue
    fi
    printf 'DNS.%d=%s\n' "${dns_index}" "${dns_name}" >> "${tmp_dir}/${name}.cnf"
    dns_index=$((dns_index + 1))
  done

  local ip_index=1
  local ip_name
  IFS=',' read -r -a ip_names <<<"${SERVER_IPS}"
  for ip_name in "${ip_names[@]}"; do
    ip_name="$(printf '%s' "${ip_name}" | xargs)"
    if [ -z "${ip_name}" ]; then
      continue
    fi
    printf 'IP.%d=%s\n' "${ip_index}" "${ip_name}" >> "${tmp_dir}/${name}.cnf"
    ip_index=$((ip_index + 1))
  done

  openssl req -newkey rsa:2048 -nodes \
    -keyout "${key}" \
    -out "${tmp_dir}/${name}.csr" \
    -config "${tmp_dir}/${name}.cnf" >/dev/null 2>&1
  openssl x509 -req \
    -in "${tmp_dir}/${name}.csr" \
    -CA "${ca_crt}" \
    -CAkey "${ca_key}" \
    -CAcreateserial \
    -out "${crt}" \
    -days 3650 \
    -extensions v3_req \
    -extfile "${tmp_dir}/${name}.cnf" >/dev/null 2>&1
}

make_client_cert() {
  local name="$1"
  local key="$2"
  local crt="$3"
  cat > "${tmp_dir}/${name}.cnf" <<EOF
[req]
distinguished_name=req_distinguished_name
req_extensions=v3_req
prompt=no
[req_distinguished_name]
CN=${name}
[v3_req]
keyUsage=keyEncipherment,digitalSignature
extendedKeyUsage=clientAuth
EOF
  openssl req -newkey rsa:2048 -nodes \
    -keyout "${key}" \
    -out "${tmp_dir}/${name}.csr" \
    -config "${tmp_dir}/${name}.cnf" >/dev/null 2>&1
  openssl x509 -req \
    -in "${tmp_dir}/${name}.csr" \
    -CA "${ca_crt}" \
    -CAkey "${ca_key}" \
    -CAcreateserial \
    -out "${crt}" \
    -days 3650 \
    -extensions v3_req \
    -extfile "${tmp_dir}/${name}.cnf" >/dev/null 2>&1
}

make_server_cert "controld" "${server_key}" "${server_crt}"
make_server_cert "gatewayd" "${gateway_key}" "${gateway_crt}" "serverAuth,clientAuth"
make_server_cert "tunneld" "${tunnel_key}" "${tunnel_crt}" "serverAuth,clientAuth"
make_client_cert "axern-dev-client" "${client_key}" "${client_crt}"
make_client_cert "axern-node" "${node_key}" "${node_crt}"
make_client_cert "rollout-worker" "${rollout_worker_key}" "${rollout_worker_crt}"

chmod 0600 "${OUT_DIR}"/*.key
echo "dev_mtls_certs_dir=${OUT_DIR}"
