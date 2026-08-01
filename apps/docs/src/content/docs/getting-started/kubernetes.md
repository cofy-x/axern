---
title: Kubernetes Install
description: Install Axern on Kubernetes with the published Helm chart and connect the CLI.
---

Axern publishes its cloud-neutral chart to GHCR as an OCI artifact. The chart
supports separate platform, observability, and runtime scheduling profiles so
production clusters can isolate control-plane services from sandbox capacity.

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

## Connect the CLI

Keep a gateway port-forward open and import the chart-generated mTLS identity
as a local CLI context:

```bash
kubectl --namespace axern-system port-forward svc/gatewayd \
  25100:25000 25101:25080 25122:25022

axern context import-kubernetes local \
  --namespace axern-system \
  --current

axern doctor --namespace default
axern catalog list
```

The imported context carries the endpoint, TLS material, and SSH identity the
CLI and SDKs need; every later workflow uses the same context model as the
local Compose install.

## Before a durable deployment

The bundled PostgreSQL and single-node defaults are intended for evaluation.
Review these chart areas before running shared or production workloads:

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
