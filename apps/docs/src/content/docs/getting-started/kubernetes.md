---
title: Kubernetes Install
description: Install Axern on Kubernetes with the published Helm chart and connect the CLI.
---

Axern publishes its cloud-neutral chart to GHCR as an OCI artifact. The chart
supports separate platform, observability, and runtime scheduling profiles so
production clusters can isolate control-plane services from sandbox capacity.

This page describes the evaluation path using a local port-forward. It needs
`kubectl`, Helm 3, and an `axern` CLI archive from the same release as the
chart. Download the archive and its checksum from the
[Axern releases](https://github.com/cofy-x/axern/releases) page before
continuing. The `<version>` value below is the release version, without a
leading `v`.

## Install the chart

```bash
helm install axern oci://ghcr.io/cofy-x/charts/axern \
  --version <version> \
  --namespace axern-system \
  --create-namespace \
  --wait \
  --timeout 15m
```

The default images are immutable version tags from the same release. Keep
environment-specific values in your own values file and pass them with
`-f values.yaml`.

## Connect the CLI without SSH

The default chart exposes the control and HTTP gateway ports. Keep this
port-forward open in one terminal:

```bash
kubectl --namespace axern-system port-forward svc/gatewayd \
  25100:25000 25101:25080
```

In a second terminal, import the chart-generated mTLS identity as a local CLI
context. The empty SSH endpoint is intentional: SSH is disabled by the chart
defaults and is not required for catalog, Run, Service, Function, or SDK
workflows.

```bash
axern context import-kubernetes local \
  --namespace axern-system \
  --endpoint 127.0.0.1:25100 \
  --service-url http://127.0.0.1:25101 \
  --ssh-endpoint "" \
  --current

axern doctor --namespace default
axern catalog list
```

The imported context carries the endpoint, HTTP service URL, and TLS material.
SSH fields remain empty unless you explicitly enable SSH and provide a client
identity. Every later control-plane workflow uses the same context model as
the local Compose install.

## Enable SSH for interactive agent workflows

SSH is an optional gateway feature for the interactive agent and SSH guides.
Enable it with an authorized public key in a separate values file:

```yaml title="ssh-values.yaml"
gatewayd:
  ssh:
    enabled: true
    authorizedKeys: |
      ssh-ed25519 AAAA... workstation
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

Import or update a context with an SSH endpoint and a private key that matches
the authorized public key:

```bash
axern context import-kubernetes local \
  --namespace axern-system \
  --endpoint 127.0.0.1:25100 \
  --service-url http://127.0.0.1:25101 \
  --ssh-endpoint 127.0.0.1:25122 \
  --ssh-identity-file ~/.ssh/id_ed25519 \
  --current
```

Do not enable SSH with the chart's default empty `authorizedKeys`; the
gateway will have no client key that can authenticate. Keep the SSH identity
file permissions restricted and review host-key handling before using this in
a shared cluster.

## Before a durable deployment

The bundled PostgreSQL and single-node defaults are intended for evaluation.
Review these chart areas before running shared or production workloads:

- **Release artifacts:** pin the chart, image, and CLI versions together, and
  verify the CLI checksum before installing it.
- **Cluster prerequisites:** confirm the required Kubernetes/Helm versions,
  `runsc`/`runc` runtime availability, node privileges for the runtime and
  volume services, an eBPF-capable Linux kernel for the default NAT dataplane
  (`node.network.natBackend=iptables` is the explicit rollback), and
  image-registry reachability from every scheduled node.
- **Gateway exposure:** replace the local port-forward with an explicitly
  managed Service or Ingress, configure TLS server names and network policy,
  and keep SSH disabled unless an interactive workflow needs it.

- **Secrets:** supply `secrets.existingSecret` with the master key, rollout
  worker token, artifact ticket key, and gateway token, and
  `postgres.existingSecret` for database credentials.
- **Durable storage:** set `postgres.persistence.enabled=true` with a
  topology-aware `ReadWriteOnce` StorageClass; do not run a durable
  environment on the `emptyDir` fallback.
- **Scheduling:** give `scheduling.platform`, `scheduling.observability`, and
  `scheduling.runtime` dedicated node-pool labels and matching `NoSchedule`
  taints.
- **Rollout workers:** set `rolloutWorker.registryAuth.existingSecret` to a
  Docker config secret that can pull TaskSet repositories; the chart wires
  separate control and execution mTLS contexts for the worker.
- **Observability:** the bundled Prometheus, Tempo, Loki, and Grafana stack is
  durable but single-replica; size retention and storage under
  `observability`.

:::caution[Pre-1.0 security boundary]
Axern does not claim a default install is safe for untrusted multi-tenant
workloads. Operators own TLS, ingress, image trust, network policy, secret
storage, quotas, and persistent storage.
:::

The [Helm chart README](https://github.com/cofy-x/axern/tree/main/deploy/helm/axern)
is the authoritative reference for values, node networking, and stateful
dependencies.
