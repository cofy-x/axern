#!/usr/bin/env bash

setup_e2e_environment() {
  ensure_verify_image_once
  docker rm -f "${POSTGRES_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker rm -f "${NODE_CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker network rm "${POSTGRES_NETWORK_NAME}" >/dev/null 2>&1 || true

  docker network create "${POSTGRES_NETWORK_NAME}" >/dev/null

  docker run -d \
    --name "${POSTGRES_CONTAINER_NAME}" \
    --network "${POSTGRES_NETWORK_NAME}" \
    --platform "${VERIFY_DOCKER_PLATFORM}" \
    -p "127.0.0.1:${POSTGRES_HOST_PORT}:5432" \
    -e "POSTGRES_DB=${POSTGRES_DB}" \
    -e "POSTGRES_USER=${POSTGRES_USER}" \
    -e "POSTGRES_PASSWORD=${POSTGRES_PASSWORD}" \
    postgres:16-alpine >/dev/null

  if ! wait_for_postgres; then
    echo "postgres did not become ready in time" >&2
    docker logs "${POSTGRES_CONTAINER_NAME}" >&2 || true
    exit 1
  fi

  if [ "${AXERN_CLI_E2E_IMPORT_RUNTIME_IMAGE}" = "1" ]; then
    ensure_python_runtime_image_once
  fi
  bash "${AXERN_ROOT}/scripts/dev-mtls-certs.sh" "${cert_dir}" >/dev/null

  command -v ssh >/dev/null 2>&1 || {
    echo "missing required command: ssh" >&2
    exit 1
  }
  command -v ssh-keygen >/dev/null 2>&1 || {
    echo "missing required command: ssh-keygen" >&2
    exit 1
  }
  ssh-keygen -q -t ed25519 -N "" -f "${ssh_dir}/gateway_host_ed25519" -C "axern-cli-e2e-gatewayd" >/dev/null
  ssh-keygen -q -t ed25519 -N "" -f "${ssh_dir}/gateway_client_ed25519" -C "axern-cli-e2e-client" >/dev/null
  cp "${ssh_dir}/gateway_client_ed25519.pub" "${ssh_dir}/authorized_keys"
  chmod 700 "${ssh_dir}"
  chmod 600 "${ssh_dir}/gateway_host_ed25519" "${ssh_dir}/gateway_client_ed25519" "${ssh_dir}/authorized_keys"

  export AXERN_TLS_CA_CERT="${cert_dir}/ca.crt"
  export AXERN_TLS_CERT="${cert_dir}/client.crt"
  export AXERN_TLS_KEY="${cert_dir}/client.key"

  "${AXERN_ROOT}/bin/controld-migrate" \
    -postgres-dsn "${CONTROLD_POSTGRES_DSN}" \
    up
  "${AXERN_ROOT}/bin/controld-access-bootstrap" \
    -postgres-dsn "${CONTROLD_POSTGRES_DSN}" \
    -principal-name cli-e2e-admin \
    -display-name "CLI E2E Administrator" \
    -credential-label cli-e2e-client \
    -certificate "${cert_dir}/client.crt" \
    -rollout-worker-certificate "${cert_dir}/rollout-worker.crt"

  "${AXERN_ROOT}/bin/storaged" \
    -grpc-address "${STORAGED_GRPC_ADDRESS}" \
    -http-address "${STORAGED_HTTP_ADDRESS}" \
    -postgres-dsn "${CONTROLD_POSTGRES_DSN}" >"${storaged_log}" 2>&1 &
  STORAGED_PID=$!

  deadline=$((SECONDS + 60))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if curl -fsS "http://${STORAGED_HTTP_ADDRESS}/healthz" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done

  if ! curl -fsS "http://${STORAGED_HTTP_ADDRESS}/healthz" >/dev/null 2>&1; then
    echo "storaged did not become ready in time" >&2
    dump_logs
    exit 1
  fi

  AXERN_RUNTIME_CATALOG_PYTHON311_IMAGE="${PYTHON_RUNTIME_IMAGE_REF}" \
    "${AXERN_ROOT}/bin/controld" \
    -grpc-address "0.0.0.0:${CONTROLD_GRPC_ADDRESS##*:}" \
    -http-address "${CONTROLD_HTTP_ADDRESS}" \
    -tls-ca-cert "${cert_dir}/ca.crt" \
    -tls-cert "${cert_dir}/controld.crt" \
    -tls-key "${cert_dir}/controld.key" \
    -secrets-master-key "test-only-master-key-32-bytes!!!" \
    -postgres-dsn "${CONTROLD_POSTGRES_DSN}" \
    -storaged-target "${STORAGED_GRPC_ADDRESS}" \
    -log-level info >"${controld_log}" 2>&1 &
  CONTROLD_PID=$!

  deadline=$((SECONDS + 60))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/healthz" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done

  if ! curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/healthz" >/dev/null 2>&1; then
    echo "controld did not become ready in time" >&2
    dump_logs
    exit 1
  fi

  "${AXERN_ROOT}/bin/gatewayd" \
    -http-address "${GATEWAY_HTTP_ADDRESS}" \
    -control-edge-address "${GATEWAY_CONTROL_ADDRESS}" \
    -control-edge-tls-ca-cert "${cert_dir}/ca.crt" \
    -control-edge-tls-cert "${cert_dir}/gatewayd.crt" \
    -control-edge-tls-key "${cert_dir}/gatewayd.key" \
    -control-target "${CONTROLD_GRPC_ADDRESS}" \
    -tls-ca-cert "${cert_dir}/ca.crt" \
    -tls-cert "${cert_dir}/gatewayd.crt" \
    -tls-key "${cert_dir}/gatewayd.key" \
    -ssh-enabled \
    -ssh-address "${GATEWAY_SSH_ADDRESS}" \
    -ssh-host-key "${ssh_dir}/gateway_host_ed25519" \
    -ssh-authorized-keys "${ssh_dir}/authorized_keys" \
    -terminal-idle-timeout 30s \
    -terminal-max-duration 5m \
    -log-level info >"${gatewayd_log}" 2>&1 &
  GATEWAYD_PID=$!

  deadline=$((SECONDS + 60))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if curl -fsS "http://${GATEWAY_HTTP_ADDRESS}/healthz" >/dev/null 2>&1 && tcp_ready "${GATEWAY_CONTROL_ADDRESS}"; then
      break
    fi
    sleep 1
  done

  if ! curl -fsS "http://${GATEWAY_HTTP_ADDRESS}/healthz" >/dev/null 2>&1 || ! tcp_ready "${GATEWAY_CONTROL_ADDRESS}"; then
    echo "gatewayd did not become ready in time" >&2
    dump_logs
    exit 1
  fi

  "${AXERN_BIN}" --config "${cli_config_file}" context set axern-cli-e2e \
    --current \
    --endpoint "${GATEWAY_CONTROL_ADDRESS}" \
    --tls-ca-cert "${cert_dir}/ca.crt" \
    --tls-cert "${cert_dir}/client.crt" \
    --tls-key "${cert_dir}/client.key" \
    --service-url "http://${GATEWAY_HTTP_ADDRESS}" \
    --ssh-endpoint "${GATEWAY_SSH_ADDRESS}" \
    --ssh-identity-file "${ssh_dir}/gateway_client_ed25519" >/dev/null

  docker run -d \
    --name "${NODE_CONTAINER_NAME}" \
    --privileged \
    --platform "${VERIFY_DOCKER_PLATFORM}" \
    -p "${NODE_GRPC_ADDRESS}:${NODE_GRPC_ADDRESS##*:}" \
    --volume "${shared_run_dir}:/shared/run" \
    --volume "${cert_dir}:/shared/certs:ro" \
    -e "AXNODED_SOCKET=${AXNODED_SOCKET}" \
    -e "AXNODED_GRPC_ADDRESS=0.0.0.0:${NODE_GRPC_ADDRESS##*:}" \
    -e "REGISTRY_PROXY_URL=${REGISTRY_PROXY_URL}" \
    -e "REGISTRY_NO_PROXY=${REGISTRY_NO_PROXY}" \
    -e "AXNODED_HTTP_ADDRESS=${NODE_HTTP_ADDRESS}" \
    -e "AXNODED_MAX_INSTANCE_NUM=32" \
    -e "AXNODED_INTERFACE_CACHE_SIZE=16" \
    -e "AXNODED_CGROUP_CACHE_SIZE=16" \
    -e "AXNODED_CONTROL_PLANE_TARGET=host.docker.internal:${CONTROLD_GRPC_ADDRESS##*:}" \
    -e "AXNODED_CONTROL_PLANE_NODE_ID=${CONTROL_PLANE_NODE_ID}" \
    -e "AXNODED_CONTROL_PLANE_NODE_AUTH_TOKEN=${CONTROL_PLANE_NODE_AUTH_TOKEN}" \
    -e "AXNODED_CONTROL_PLANE_NODE_TARGET=${NODE_GRPC_ADDRESS}" \
    -e "AXNODED_CONTROL_PLANE_HEARTBEAT_INTERVAL=1s" \
    -e "AXNODED_CONTROL_PLANE_TLS_CA_CERT=/shared/certs/ca.crt" \
    -e "AXNODED_CONTROL_PLANE_TLS_CERT=/shared/certs/node.crt" \
    -e "AXNODED_CONTROL_PLANE_TLS_KEY=/shared/certs/node.key" \
    "${IMAGE_TAG}" \
    /bin/bash /workspace/scripts/verify/node-all-in-one-entrypoint.sh >/dev/null

  deadline=$((SECONDS + 180))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if ! docker inspect -f '{{.State.Running}}' "${NODE_CONTAINER_NAME}" 2>/dev/null | grep -qx true; then
      echo "node container exited before becoming ready" >&2
      dump_logs
      exit 1
    fi
    if [ -S "${shared_run_dir}/axnoded.sock" ] && docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "curl -fsS http://127.0.0.1:23001/readyz >/dev/null"; then
      break
    fi
    sleep 2
  done

  if ! [ -S "${shared_run_dir}/axnoded.sock" ] || ! docker exec "${NODE_CONTAINER_NAME}" /bin/bash -lc "curl -fsS http://127.0.0.1:23001/readyz >/dev/null"; then
    echo "node container did not become ready in time" >&2
    dump_logs
    exit 1
  fi

  if [ "${AXERN_CLI_E2E_IMPORT_RUNTIME_IMAGE}" = "1" ]; then
    import_python_runtime_image_once
  fi

  # Capability readiness includes serial runc and runsc conformance probes. Each
  # runtime has a 60-second fail-closed budget, so the inventory wait must cover
  # both probes plus the first report round trip.
  local capability_readiness_timeout="${AXERN_CLI_E2E_CAPABILITY_READINESS_TIMEOUT_SECONDS:-150}"
  if ! [[ "${capability_readiness_timeout}" =~ ^[1-9][0-9]*$ ]]; then
    echo "AXERN_CLI_E2E_CAPABILITY_READINESS_TIMEOUT_SECONDS must be a positive integer" >&2
    exit 1
  fi
  deadline=$((SECONDS + capability_readiness_timeout))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    nodes_body="$(curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/nodesz" || true)"
    if node_summary_fresh "${CONTROL_PLANE_NODE_ID}" "${nodes_body}"; then
      break
    fi
    sleep 1
  done

  nodes_body="$(curl -fsS "http://${CONTROLD_HTTP_ADDRESS}/nodesz" || true)"
  if ! node_summary_fresh "${CONTROL_PLANE_NODE_ID}" "${nodes_body}"; then
    echo "controld did not observe a fresh node summary in time" >&2
    dump_logs
    exit 1
  fi
}
