# Axern Helm Chart

This chart is the deployment entrypoint for running Axern on Kubernetes. It
supports separate platform, observability, and runtime scheduling profiles so
production clusters can isolate control-plane services from sandbox capacity.

## Render And Install

Released charts are published to GHCR as OCI artifacts:

```bash
helm install axern oci://ghcr.io/cofy-x/charts/axern \
  --version 0.2.0 \
  --namespace axern-system \
  --create-namespace
```

The default Axern images are immutable version tags from the same release.
Source development uses `values-local-development.yaml` after
`make local-images-build`; it does not change the public chart defaults.

Keep environment-specific values outside this repository and pass them through
`AXERN_HELM_VALUES`:

```bash
make helm-lint
make helm-template AXERN_HELM_VALUES=/path/to/values.yaml
make helm-install \
  AXERN_KUBECONFIG=/path/to/kubeconfig \
  AXERN_HELM_VALUES=/path/to/values.yaml
```

`make helm-health` verifies both service health and the current node-report
contract: every reported node must be fresh and include aggregate
`runtime_slots`. Releases that change a required node-summary contract must use
one image set for controld and node-all-in-one and rebuild them together. The
chart does not support a mixed-version fallback to cgroup/interface capacity
inference.

Use `helm-registry-secret` when a generic private registry pull secret needs to
be created. Cloud resource provisioning, provider credentials, and
environment profiles are intentionally outside the chart.

```bash
make helm-registry-secret \
  AXERN_KUBECONFIG=/path/to/kubeconfig \
  AXERN_REGISTRY_SERVER=registry.example.com \
  AXERN_REGISTRY_USERNAME=robot \
  AXERN_REGISTRY_PASSWORD='...'
```

Set the chart's `global.imagePullSecrets` value to the corresponding
`AXERN_REGISTRY_PULL_SECRET` name when private images require it.

When `secrets.existingSecret` is configured, it must contain
`AXERN_SECRETS_MASTER_KEY`, `CONTROLD_ROLLOUT_WORKER_TOKEN`,
`CONTROLD_ARTIFACT_TICKET_KEY`, and `GATEWAYD_DEV_TOKEN`. When
`postgres.existingSecret` is configured, it must contain the keys selected by
`postgres.passwordKey` and `postgres.dsnKey`.

Durable rollout workers resolve TaskSet descriptors from inside the running
container, so kubelet image-pull credentials alone are insufficient. Set
`rolloutWorker.registryAuth.existingSecret` to a Docker config secret that can
pull TaskSet repositories; the chart mounts only its configured key as the
worker's read-only Docker config. Runtime-node image credentials remain under
`node.registryAuth` because the two components may use different repositories.

The chart gives each rollout worker two explicit mTLS contexts. Its control
context connects directly to controld's private worker API, while its execution
context connects to gatewayd for allocation and sandbox APIs. Do not collapse
these endpoints or expose `RolloutWorkerControl` through gatewayd: their
separate authority and routing responsibilities are intentional.

The bundled MinIO endpoint is cluster-internal and is intended for development
and in-cluster verification. Clients never connect to MinIO/S3 directly:
gatewayd streams artifacts after resolving a short-lived ticket through
controld's private mTLS API. Only rollout workers and gatewayd's resolved
internal presigned request need object-store network reachability; gatewayd
does not receive static S3 credentials. Gatewayd uses its dedicated
`gatewayd.crt` identity for controld calls; generic `client.crt` holders are not
authorized to resolve artifact tickets.
When `pki.existingSecret` is used, its `gatewayd.crt` must have both
`serverAuth` and `clientAuth` extended key usages and the verified subject
identity `gatewayd`; the chart-generated certificate already has that shape.

`secrets.artifactTicketKey` is independent from provider, registry,
object-store, and worker bootstrap credentials. The chart preserves a generated
key across upgrades with `lookup`, or consumes the configured existing Secret.
Configure gateway artifact concurrency, chunk size, upstream timeout, and
maximum artifact size under `gatewayd.artifact`.

## Node Resources

The chart defaults `node.resourceSource` to `kubernetes`, so `node-all-in-one`
reports Kubernetes `Node.status.capacity` and `Node.status.allocatable` to the
control plane. This keeps Grafana capacity panels aligned with the cloud node
shape while making Axern admission use Kubernetes allocatable capacity. The
chart creates a dedicated node service account and a minimal ClusterRole with
`get` access to `nodes`.

Set `node.resourceSource=host` for non-Kubernetes-style deployments where
axnoded should derive both capacity and allocatable from the host itself.

## Workload Scheduling

Use `scheduling.platform`, `scheduling.observability`, and
`scheduling.runtime` to configure the node selectors, taints, and topology
spread policy for each workload class. Platform services include the Axern
control plane and chart-managed backing services. Observability contains the
OpenTelemetry Collector, Prometheus, Tempo, Loki, and Grafana. Each stateful
backend has an independent PVC and lifecycle; the node-all-in-one DaemonSet
uses the runtime profile.

Production deployments should give each class a dedicated node-pool label and
matching `NoSchedule` taint. Keep topology spreading enabled for replicated
platform and observability workloads. The runtime DaemonSet does not need a
topology-spread constraint because its desired placement is one pod on every
runtime node.

## Stateful Dependencies

The chart-managed PostgreSQL deployment is suitable for a development or
production-validation cluster when `postgres.persistence.enabled=true` and a
topology-aware `ReadWriteOnce` StorageClass is selected. Do not run a durable
environment with the PostgreSQL `emptyDir` fallback.

MinIO is not an Axern control-plane dependency. `minio.enabled` only deploys an
in-cluster S3-compatible service, while `objectStore.enabled` controls whether
node runtime images receive a default S3 backend. OCI and Nydus registry
rootfs paths require neither setting. Enable both only for an intentional S3
rootfs test, and give MinIO its own PVC.

## Observability

The production-validation stack deliberately avoids the all-in-one LGTM image.
The Collector routes OTLP metrics to a Prometheus scrape endpoint, traces to
Tempo, and logs to Loki. Prometheus, Tempo, Loki, and Grafana persist data on
separate `ReadWriteOnce` volumes, so restarting or upgrading one component does
not erase the other signals. Configure retention and storage sizes under
`observability`; use a topology-aware StorageClass on a multi-zone cluster.

The bundled stack is single-replica and durable, not highly available. It is
the baseline environment for repeatable performance attribution. A production
service requiring observability control-plane HA should use managed or
distributed backends without changing Axern's OTLP export contract.

For an externally reachable Grafana, set
`observability.grafana.admin.existingSecret`. The Secret must contain the keys
configured by `userKey` and `passwordKey`. Grafana consumes these values only
when its database is first initialized; rotate an existing administrator with
Grafana's supported admin workflow, then restart the Deployment. Changing the
environment variables alone does not rewrite an initialized account.

## Node Networking

The chart defaults `node.network.natBackend` to `ebpf`. bpfnet is the default
production NAT dataplane for supported Axern Linux nodes after the production
replacement gates in
[`network/bpfnet/docs/production-replacement-baseline.md`](../../../network/bpfnet/docs/production-replacement-baseline.md)
pass. Use `node.network.natBackend=iptables` only as an explicit rollback
backend.

Keep `node.network.ebpf.localOutCompat=true` and
`node.network.ebpf.iptablesFallback=true`. A mode that includes
`localhost-tcp-iptables-compat` is acceptable when the kernel cannot expose the
localhost TCP cgroup path, as long as TC ingress and egress remain attached.
`iptables-full-fallback` is not a successful bpfnet replacement state.

`node.network.ebpf.mapSize` controls low-churn service and localhost maps.
`node.network.ebpf.snatMapSize` controls the egress SNAT forward/reverse maps
and should be sized for short-connection flow churn. The translated source
port allocator uses a fixed dataplane range of `10000-65535` with `256`
hash/stride fallback probes after same-port conflicts. axnoded runs a background
SNAT GC loop when bpfnet is active; tune `snatGcInterval`,
`snatTcpIdleTimeout`, `snatTcpClosingTimeout`, and `snatDatagramIdleTimeout`
when validating high-churn TCP short connections or UDP workloads. The default
datagram idle timeout is tuned for short-message churn; increase it for
long-idle UDP or QUIC-like traffic. Use `bpfnetctl status --json` to inspect map
occupancy; `bpfnetctl check --json` is only a readiness check.

Before promoting a new bpfnet change, use the reusable regression runbook in
[`network/bpfnet/docs/production-regression-runbook.md`](../../../network/bpfnet/docs/production-regression-runbook.md).
