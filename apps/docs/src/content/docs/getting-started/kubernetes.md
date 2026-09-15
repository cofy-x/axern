---
title: Kubernetes Install
description: Install Axern on Kubernetes with the published Helm chart and connect the CLI.
---

Axern publishes its cloud-neutral chart to GHCR as an OCI artifact. The chart supports separate platform, observability, and runtime scheduling profiles so production clusters can isolate control-plane services from sandbox capacity.

This page describes the evaluation path using a local port-forward. It needs `kubectl`, Helm 3, and an `axern` CLI archive from the same release as the chart. Download the archive and its checksum from the [Axern releases](https://github.com/cofy-x/axern/releases) page before continuing. The `<version>` value below is the release version, without a leading `v`.

## Install the chart

Create the namespace, Axern-owned signing material and one random enrollment token per Kubernetes Node. The Secret key is the Axern Node ID (a fresh random identity, independent of the Kubernetes Node name); keep the temporary files until the matching identities are admitted below.

```bash
kubectl create namespace axern-system

axern admin pki bootstrap --directory ./axern-pki --cluster axern.local \
  --dns localhost,controld,gatewayd,tunneld,controld.axern-system.svc,gatewayd.axern-system.svc,tunneld.axern-system.svc
kubectl -n axern-system create secret generic axern-pki \
  --from-file=ca.crt=./axern-pki/ca.crt \
  --from-file=controld.pem=./axern-pki/controld.pem \
  --from-file=gatewayd.pem=./axern-pki/gatewayd.pem \
  --from-file=tunneld.pem=./axern-pki/tunneld.pem \
  --from-file=client.crt=./axern-pki/client.crt \
  --from-file=client.key=./axern-pki/client.key
kubectl -n axern-system create secret generic axern-pki-signer \
  --from-file=signer.pem=./axern-pki/private/signer.pem

enrollment_token_dir="$(mktemp -d)"
node_secret_args=()
node_helm_args=()
node_index=0
for kubernetes_node in $(kubectl get nodes -o name | sed 's#node/##'); do
  axern_node_id="node-$(openssl rand -hex 16)"
  node_helm_args+=(--set-string "node.enrollment.nodes[${node_index}].nodeName=${kubernetes_node}"
                    --set-string "node.enrollment.nodes[${node_index}].nodeID=${axern_node_id}")
  node_index=$((node_index + 1))
  openssl rand -hex 32 > "${enrollment_token_dir}/${axern_node_id}"
  chmod 600 "${enrollment_token_dir}/${axern_node_id}"
  node_secret_args+=(--from-file="${axern_node_id}=${enrollment_token_dir}/${axern_node_id}")
done
kubectl --namespace axern-system create secret generic axern-enrollment-tokens "${node_secret_args[@]}"

: "${AXERN_NODE_MEMORY_SYSTEM_RESERVE_BYTES:?set the qualified node memory reserve in bytes}"
helm install axern oci://ghcr.io/cofy-x/charts/axern \
  --version <version> \
  --namespace axern-system \
  --set-string node.enrollment.existingSecret=axern-enrollment-tokens \
  --set-string "node.memorySystemReserveBytes=${AXERN_NODE_MEMORY_SYSTEM_RESERVE_BYTES}" \
  "${node_helm_args[@]}"

kubectl -n axern-system rollout status deployment/controld --timeout=5m
kubectl -n axern-system rollout status deployment/gatewayd --timeout=5m
```

First installation deliberately does not wait for Node readiness before admission. Supply the memory reserve from the node qualification result; do not invent a small value merely to satisfy Helm validation. After control-plane startup and admission, the explicit DaemonSet rollout check below verifies readiness. The default images are immutable version tags from the same release. Keep environment-specific values in your own values file and pass them with `-f values.yaml`.

## Connect the CLI without SSH

The default chart exposes the control and HTTP gateway ports. Keep this port-forward open in a separate terminal:

```bash
kubectl --namespace axern-system port-forward svc/gatewayd \
  25100:25000 25101:25080
```

Back in the original shell (which owns `enrollment_token_dir`), import the deployment-owned administrator mTLS identity as a local CLI context. The empty SSH endpoint is intentional: SSH is disabled by the chart defaults and is not required for Environment, Run, or SDK workflows.

```bash
axern context import-kubernetes local \
  --namespace axern-system \
  --secret axern-pki \
  --endpoint 127.0.0.1:25100 \
  --ssh-endpoint "" \
  --current

for credential_path in "${enrollment_token_dir}"/node-*; do
  axern admin node admit "$(basename "${credential_path}")" \
    --enrollment-token-file "${credential_path}" \
    --operator-reason "initial Kubernetes node admission"
done

kubectl -n axern-system rollout status daemonset -l app.kubernetes.io/component=node --timeout=15m
axern doctor --namespace default
axern environment list
```

The imported context carries the control endpoint and TLS material. Node reports are fail-closed until admission succeeds. SSH fields remain empty unless you explicitly enable SSH and provide a client identity. Every later control-plane workflow uses the same context model as the local Compose install.

## Enable SSH for interactive agent workflows

SSH is an optional gateway feature. Enable the listener in a separate values file; client keys are registered through AccessAdmin, not Helm values:

```yaml title="ssh-values.yaml"
gatewayd:
  ssh:
    enabled: true
```

Apply the values file to the same release, then forward the SSH port:

```bash
helm upgrade axern oci://ghcr.io/cofy-x/charts/axern \
  --version <version> \
  --namespace axern-system \
  -f ssh-values.yaml \
  --reuse-values \
  --wait \
  --timeout 15m

kubectl --namespace axern-system port-forward svc/gatewayd 25122:25022
```

Register the public key as a Principal Credential whose namespace role permits execution, then import or update the context:

```bash
axern admin credential add <principal-id> --ssh-public-key ~/.ssh/id_ed25519.pub \
  --expires-at 2027-01-01T00:00:00Z --label workstation
axern context import-kubernetes local \
  --namespace axern-system \
  --secret axern-pki \
  --endpoint 127.0.0.1:25100 \
  --ssh-endpoint 127.0.0.1:25122 \
  --ssh-identity-file ~/.ssh/id_ed25519 \
  --current
```

Choose a credential expiry appropriate to the deployment and grant its Principal the required namespace role. Keep SSH private keys restricted and verify the persistent gateway host key before using this in a shared cluster.

## Node identity operations

Persist the explicit `nodeName` / `nodeID` bindings in your deployment values; do not regenerate them on ordinary upgrades. Bootstrap tokens are read-only files, not node configuration or environment contents. After certificate publication, the token Secret may be removed; restart and automatic renewal no longer read it. Certificates renew with 7–8 hours remaining using bounded jittered retries.

Use `axern admin node revoke <node-id> --operator-reason <reason>` to withdraw authority immediately, even while busy. This does not assert resource cleanup: the Allocation lifecycle still owns termination and release, and existing in-flight authority is bounded by its normal TTL. A compromised host also requires external isolation. `node retire` remains blocked until cleanup and access obligations converge.

Host replacement, lost state, or expired identity requires a new Node ID and fresh node state, even when Kubernetes reuses the hostname. Fence and clean up the old host first; never erase live state or reuse a token to bypass recovery.

## Before a durable deployment

The bundled PostgreSQL and single-node defaults are intended for evaluation. Review these chart areas before running shared or production workloads:

- **Release artifacts:** pin the chart, image, and CLI versions together, and verify the CLI checksum before installing it.
- **Cluster prerequisites:** confirm the required Kubernetes/Helm versions, `runsc` runtime availability, node privileges for the runtime and image services, an eBPF-capable Linux kernel for the default NAT dataplane (`node.network.natBackend=iptables` is the explicit rollback), and image-registry reachability from every scheduled node.
- **Gateway exposure:** replace the local port-forward with an explicitly managed Service or Ingress, configure TLS server names and network policy, and keep SSH disabled unless an interactive workflow needs it.

- **Secrets:** supply `secrets.existingSecret` with the master key, `postgres.existingSecret` for database credentials, and `node.enrollment.existingSecret` with an independent one-time enrollment token for every admitted Node ID. Before scheduling the DaemonSet onto a new Kubernetes Node, add its `nodeID` key and admit that identity through the admin API.
- **Durable storage:** set `postgres.persistence.enabled=true` with a topology-aware `ReadWriteOnce` StorageClass; do not run a durable environment on the `emptyDir` fallback.
- **Scheduling:** give `scheduling.platform`, `scheduling.observability`, and `scheduling.runtime` dedicated node-pool labels and matching `NoSchedule` taints.
- **Observability:** the bundled Prometheus, Tempo, Loki, and Grafana stack is durable but single-replica; size retention and storage under `observability`.

:::caution[Pre-1.0 security boundary]

Axern does not claim a default install is safe for untrusted multi-tenant workloads. Operators own TLS, ingress, image trust, network policy, secret storage, quotas, and artifact retention.

:::

The [Helm chart README](https://github.com/cofy-x/axern/tree/main/deploy/helm/axern) is the authoritative reference for values, node networking, and stateful dependencies.
