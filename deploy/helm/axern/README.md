# Axern Helm Chart

This chart is the deployment entrypoint for running Axern on Kubernetes. It supports separate platform, observability, and runtime scheduling profiles so production clusters can isolate control-plane services from sandbox capacity.

## Render And Install

Provision identity material first with `axern admin pki bootstrap` and create `pki.secretName` and the separate control-only `pki.signerSecretName`. The [Kubernetes install guide](../../../apps/docs/src/content/docs/getting-started/kubernetes.md) gives the release-only commands. Source deployments can use `make helm-pki-bootstrap` before `make helm-install`. The chart never generates or shares Node private keys.

Released charts are published to GHCR as OCI artifacts:

```bash
helm install axern oci://ghcr.io/cofy-x/charts/axern \
  --version 0.6.2 \
  --namespace axern-system \
  --create-namespace \
  --set-string node.enrollment.existingSecret=axern-enrollment-tokens \
  --set-json 'node.enrollment.nodes=[{"nodeName":"worker-a","nodeID":"node-a"},{"nodeName":"worker-b","nodeID":"node-b"}]' \
  --set-string node.memorySystemReserveBytes=<qualified-bytes>
```

The default Axern images are immutable version tags from the same release. Source development uses `values-local-development.yaml` after `make local-images-build`; it does not change the public chart defaults.

Keep environment-specific values outside this repository and pass them through `AXERN_HELM_VALUES`:

```bash
make helm-lint
make helm-template AXERN_HELM_VALUES=/path/to/values.yaml
make helm-install \
  AXERN_KUBECONFIG=/path/to/kubeconfig \
  AXERN_HELM_VALUES=/path/to/values.yaml
```

`make helm-health` verifies both service health and the current node-report contract: every reported node must be fresh and include aggregate `runtime_slots`. Releases that change a required node-summary contract must use one image set for controld and node-all-in-one and rebuild them together. The chart does not support a mixed-version fallback to cgroup/interface capacity inference.

Use `helm-registry-secret` when a generic private registry pull secret needs to be created. Cloud resource provisioning, provider credentials, and environment profiles are intentionally outside the chart.

```bash
make helm-registry-secret \
  AXERN_KUBECONFIG=/path/to/kubeconfig \
  AXERN_REGISTRY_SERVER=registry.example.com \
  AXERN_REGISTRY_USERNAME=robot \
  AXERN_REGISTRY_PASSWORD='...'
```

Set the chart's `global.imagePullSecrets` value to the corresponding `AXERN_REGISTRY_PULL_SECRET` name when private images require it.

When `secrets.existingSecret` is configured, it must contain `AXERN_SECRETS_MASTER_KEY` and `GATEWAYD_DEV_TOKEN`. When `postgres.existingSecret` is configured, it must contain the keys selected by `postgres.passwordKey` and `postgres.dsnKey`.

Gatewayd uses its dedicated URI identity from `gatewayd.pem`. Workloads mount only their role bundle and public trust; only controld mounts the separate signing Secret. All services share `pki.trustDomain`. Initial administrator metadata is configured under `auth.bootstrap`; later administrator credential changes use AccessAdmin, not service renewal.

Every Node must be admitted before it reports observations. Set `node.enrollment.nodes` to explicit `{nodeName, nodeID}` bindings. Node identity is independent of the Kubernetes hostname; preserve the bindings in deployment values. The chart renders one pinned DaemonSet per name and projects only that Node's token key; no runtime pod receives the full token Secret. Deployment-label hashes only disambiguate Kubernetes object names and never authorize Node identity. `node.enrollment.existingSecret` contains one random token per Node ID, with matching data keys. Admit IDs using `axern admin node admit --enrollment-token-file`. Within one hour of admission the node registers one locally generated key; exact CSR retries recover the committed reply and a different CSR is rejected. Certificates renew automatically without changing Node ID. Retirement is irreversible; expired or lost Node identity requires operator recovery, never silent token reuse.

## Node Resources

The chart defaults `node.resourceSource` to `kubernetes`, so `node-all-in-one` reports Kubernetes `Node.status.capacity` and `Node.status.allocatable` to the control plane. This keeps Grafana capacity panels aligned with the cloud node shape while making Axern admission use Kubernetes allocatable capacity. The chart creates a dedicated node service account and a minimal ClusterRole with `get` access to `nodes`.

Set `node.resourceSource=host` for non-Kubernetes-style deployments where axnoded should derive both capacity and allocatable from the host itself.

## Workload Scheduling

Use `scheduling.platform`, `scheduling.observability`, and `scheduling.runtime` to configure the node selectors, taints, and topology spread policy for each workload class. Platform services include the Axern control plane and chart-managed backing services. Observability contains the OpenTelemetry Collector, Prometheus, Tempo, Loki, and Grafana. Each stateful backend has an independent PVC and lifecycle; the node-all-in-one DaemonSet uses the runtime profile.

Production deployments should give each class a dedicated node-pool label and matching `NoSchedule` taint. Keep topology spreading enabled for replicated platform and observability workloads. Each pinned runtime DaemonSet places one pod on its explicitly enrolled host; adding runtime capacity requires admitting and adding a new binding.

## Stateful Dependencies

The chart-managed PostgreSQL deployment is suitable for a development or production-validation cluster when `postgres.persistence.enabled=true` and a topology-aware `ReadWriteOnce` StorageClass is selected. Do not run a durable environment with the PostgreSQL `emptyDir` fallback.

Chart-managed PostgreSQL uses a zero-surge deployment strategy. Its single-writer volume must never be mounted by overlapping old and new Pods during an upgrade.

## Observability

The production-validation stack deliberately avoids the all-in-one LGTM image. The Collector routes OTLP metrics to a Prometheus scrape endpoint, traces to Tempo, and logs to Loki. Prometheus, Tempo, Loki, and Grafana persist data on separate `ReadWriteOnce` volumes, so restarting or upgrading one component does not erase the other signals. Configure retention and storage sizes under `observability`; use a topology-aware StorageClass on a multi-zone cluster.

The bundled stack is single-replica and durable, not highly available. It is the baseline environment for repeatable performance attribution. A production service requiring observability control-plane HA should use managed or distributed backends without changing Axern's OTLP export contract.

For an externally reachable Grafana, set `observability.grafana.admin.existingSecret`. The Secret must contain the keys configured by `userKey` and `passwordKey`. Grafana consumes these values only when its database is first initialized; rotate an existing administrator with Grafana's supported admin workflow, then restart the Deployment. Changing the environment variables alone does not rewrite an initialized account.

## Node Networking

The chart defaults `node.network.natBackend` to `ebpf`. bpfnet is the default production NAT dataplane for supported Axern Linux nodes after the production replacement gates in [`network/bpfnet/docs/production-replacement-baseline.md`](../../../network/bpfnet/docs/production-replacement-baseline.md) pass. Use `node.network.natBackend=iptables` only as an explicit rollback backend.

The eBPF backend requires TC ingress/egress and localhost cgroup links to be ready; attach or reconciliation failure is fail-closed. Select `node.network.natBackend=iptables` explicitly when the complete iptables backend is required. The two dataplanes are never mixed on IPv4.

`node.network.ebpf.snatMapSize` controls the egress SNAT forward/reverse maps and should be sized for short-connection flow churn. The translated source port allocator uses a fixed dataplane range of `10000-65535` with `256` hash/stride fallback probes after same-port conflicts. axnoded runs a background SNAT GC loop when bpfnet is active; tune `snatGcInterval`, `snatTcpIdleTimeout`, `snatTcpClosingTimeout`, and `snatDatagramIdleTimeout` when validating high-churn TCP short connections or UDP workloads. The default datagram idle timeout is tuned for short-message churn; increase it for long-idle UDP or QUIC-like traffic. Use `bpfnetctl status --json` to inspect map occupancy; `bpfnetctl check --json` is only a readiness check.

Before promoting a new bpfnet change, use the reusable regression runbook in [`network/bpfnet/docs/production-regression-runbook.md`](../../../network/bpfnet/docs/production-regression-runbook.md).

## Identity Rotation, Replacement, And Removal

Bootstrap Secrets are optional mounts and can be removed after all identities have been published. A new node without its token remains unready; an enrolled node restarts and renews without it. Token contents are never copied into node configuration or environment variables.

Renewing a certificate preserves Node ID and local Allocation state. A hostname change does not require a new identity if the same private state is deliberately retained. Disk loss, expired identity, or host replacement requires a new admitted Node ID, even when Kubernetes reuses the hostname. Do not mount the old Axern state directory into the new identity, or erase live state to force enrollment. Fence the old host, complete its Allocation cleanup, retire its identity, then provision fresh state and update the binding. The chart deliberately does not infer replacement from hostnames or automate cloud fencing.

Use `axern admin node revoke <node-id> --operator-reason <reason>` for emergency withdrawal of authority; this does not claim that resources were cleaned. Retire only after cleanup obligations are resolved. These are separate transitions in one Node lifecycle, not separate certificate lifecycle entities.
