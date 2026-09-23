# Configuration

This document explains the operator-facing meaning of [`sample_conf.toml`](sample_conf.toml). The sample is a production-shaped operator template, not a universal runnable default: required qualification values such as `memory_system_reserve_bytes` must be supplied before startup. Use this page for context, profiles, and troubleshooting hints.

`axnoded` starts from `config.DefaultConfig()`, then overlays the TOML file passed by `-config`. If `-config` is omitted, it reads `<root>/config.toml`, where `-root` defaults to `/var/lib/axnoded`.

Daemon flags such as `-socket`, `-grpc-address`, `-http-address`, `-log-level`, and `-log-file` are not TOML fields. They control process endpoints and logging for the current daemon invocation.

## File Roles

| File                                                                                         | Role                                                                                          |
| -------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| [`sample_conf.toml`](sample_conf.toml)                                                       | production-shaped reference template with deliberately unset environment qualification values |
| [`../config/config.go`](../config/config.go)                                                 | TOML schema, normalization helpers, and default overlay behavior                              |
| [`../config/defaults.go`](../config/defaults.go)                                             | default values used before TOML overlay                                                       |
| [Resource Handling](resource.md)                                                             | cgroup/interface pool behavior behind `[plugin.resource]` and `[plugin.network]`              |
| [Observed Capability Providers](../../../docs/architecture/observed-capability-providers.md) | platform observation and structured extension capability contract                             |
| [Runtime Logs](../../../docs/operations/runtime-logs.md)                                     | cross-component log locations and meanings                                                    |
| [Local Troubleshooting](../../../deploy/local/troubleshooting.md)                            | compose/kind troubleshooting commands                                                         |

## Top-Level Paths

| Key        | Meaning                                                  | Change When                                     |
| ---------- | -------------------------------------------------------- | ----------------------------------------------- |
| `rootDir`  | Sandbox runtime state, OCI bundles, and container roots. | Moving axnoded state to another disk or mount.  |
| `storeDir` | Persistent metadata store used for restart recovery.     | Separating metadata from heavier runtime state. |

If either path is wrong or not writable, startup, restart recovery, and container delete cleanup can fail early. In compose/kind, confirm the path is inside the mounted dev volume before treating it as a runtime bug.

## Control Plane

`[plugin]` contains the optional outbound relationship to `controld`.

| Key | Meaning | Notes |
| --- | --- | --- |
| `control_plane_target` | `controld` node-control endpoint. | Empty disables the reporter. |
| `control_plane_node_id` | Stable node identity reported to `controld`. | Required for a connected node; validated against its URI SAN identity. |
| `control_plane_node_target` | Internal address that `gatewayd` and `controld` can use to reach this node. | Needed when gateway forwarding crosses host boundaries. |
| `control_plane_enrollment_target` | Dedicated controld enrollment listener. | Required for node registration and renewal. |
| `workload_cluster` | Exact URI trust domain. | Must match controld and deployment PKI. |
| `control_plane_heartbeat_interval` | Node report interval. | Empty or non-positive falls back to `5s`. |
| `control_plane_node_resource_source` | Source for reported node capacity, allocatable resources, and placement labels. | Use `kubernetes` in Kubernetes deployments so axnoded reports Node API `status.capacity`, `status.allocatable`, and `metadata.labels`; use `host` for local or bare-metal nodes. |
| `control_plane_kubernetes_node_name` | Kubernetes Node object name used when `control_plane_node_resource_source = "kubernetes"`. | In Helm deployments this is populated from `spec.nodeName`; otherwise empty falls back to `control_plane_node_id`. |
| `control_plane_tls_ca_cert` | CA certificate path for control-plane TLS. | Use with secured `controld` endpoints. |
| `control_plane_node_state` | Advertised scheduling state. | Valid values are `ready`, `draining`, and `disabled`; invalid values normalize to `ready`. |
| `[[node_extension_capabilities]]` | Exact-match extension facts using `name` and optional `value`. | Names must use `<dns-domain>/<name>`; Axern-owned domains are rejected. Platform capabilities cannot be configured. |
| `[plugin.control_plane_node_labels]` | Explicit placement labels. | Empty keys are ignored and values are trimmed. Explicit labels override labels collected from the Kubernetes Node object. |

Check the deployment Prometheus/LGTM metrics exported through OTEL, such as `axern_axnoded_control_plane_rpc_total` and `axern_axnoded_control_plane_report_total`, plus axnoded logs when node-report or heartbeat behavior looks wrong. For allocation lifecycle delivery, inspect `/control-planez` and `axern_axnoded_allocation_lifecycle_oldest_pending_age_seconds`, `axern_axnoded_allocation_lifecycle_consecutive_failures`, and `axern_axnoded_allocation_lifecycle_retry_delay_seconds`.

Extension capability declarations are config-static facts. Platform facts are owned by probes and derived policy, and therefore have no configuration list or operator override.

Failure of the configured node resource source is fail-closed. Axnoded retains the last successful values only in its local inventory for diagnosis; it stops publishing heartbeats and invalidates node-local memory admission until a new authoritative resource sample succeeds. Cached Kubernetes capacity is never re-timestamped as current capacity.

## Network

`[plugin.network]` configures sandbox bridge/veth networking.

| Key | Meaning | Notes |
| --- | --- | --- |
| `ip_range` | IPv4 or IPv6 CIDR used for `sandbox0`, sandbox IPs, and host veth allocation. | Must provide at least `max_instance_num` addresses and must not collide with host, pod, service, or VPC ranges. |
| `nat_backend` | NAT implementation. | Valid values are `iptables` and `ebpf`. |

`iptables` provides bridge egress NAT for IPv4 and IPv6. `ebpf` is an IPv4-only egress backend that keeps the same bridge/veth/netns shape and requires both TC directions and all current pinned objects to be ready. An IPv6 `ip_range` with `nat_backend = "ebpf"` is rejected; axnoded never changes the selected backend implicitly or mixes iptables rules into an active eBPF dataplane. Inbound access is provided through Allocation-scoped Tunnel or SSH sessions.

`[plugin.network.ebpf]` is only active when `nat_backend = "ebpf"`.

| Key | Meaning | Notes |
| --- | --- | --- |
| `pin_path` | bpffs pin root for bpfnet maps and programs. | Default is `/sys/fs/bpf/axern/bpfnet`. |
| `snat_map_size` | egress SNAT forward/reverse map capacity. | Size for short-connection flow churn; default is `262144`. |
| `snat_gc_interval` | Background interval for axnoded to remove stale bpfnet SNAT mappings. | Empty or non-positive falls back to `1s`. |
| `snat_tcp_idle_timeout` | Idle timeout for active TCP SNAT mappings. | Keep long enough for pooled/keep-alive connections; default is `5m`. |
| `snat_tcp_closing_timeout` | Idle timeout after a TCP mapping observes FIN/RST. | Lower values release translated ports faster for short-connection churn; default is `2s`. |
| `snat_datagram_idle_timeout` | Idle timeout for UDP and ICMP SNAT mappings. | Default is `10s`; increase for long-idle UDP or QUIC-like traffic. |
| `uplink_devices` | Optional uplink device allowlist. | Leave empty for auto/default behavior. |
| `native_routing_cidrs` | CIDRs that should use native routing behavior. | Deployment-specific; keep empty unless the dataplane requires it. |

When networking fails, inspect the selected backend, `sandbox0`, host veths, egress SNAT state, and bpfnet attach logs before changing resource sizing.

## Resource Pool

`[plugin.resource]` controls node-local cgroup and interface warm pools. The implementation details are covered in [Resource Handling](resource.md).

| Key | Meaning | Notes |
| --- | --- | --- |
| `cgroup_cache_size` | Never-assigned warm cgroup target. | `0` disables prewarming only; required enforcement still creates a fresh one-use cgroup per allocation. Must not exceed `max_instance_num`. |
| `interface_cache_size` | Idle interface target. | `0` disables the pool and is valid only when no loaded runtime requires interfaces; must not exceed `max_instance_num`. |
| `cgroup_root_name` | Single child name created under axnoded's delegated cgroup-v2 root. | Defaults to `sandbox`. |
| `max_instance_num` | Positive hard cap for using plus idle resources. | Must not exceed the container hard limit; exhaustion can also come from IP capacity. |
| `memory_system_reserve_bytes` | Memory reserved for axnoded, lifecycle monitors, node-local daemons, and the isolated runtime-certification cgroup outside sandbox capacity. | It must cover the 512 MiB aggregate runtime-conformance ceiling plus measured daemon headroom when `cgroup_enforcement = "required"`; production qualification owns the exact value. `disabled_dev` must use zero because it has no enforceable internal cgroup accounting. There is intentionally no production default. |
| `resource_pool_reconcile_interval` | Reconcile interval for filling warm pools. | Empty or non-positive falls back to `1s`. |

Useful symptoms:

| Symptom                      | Start With                                                                                        |
| ---------------------------- | ------------------------------------------------------------------------------------------------- |
| `ErrResourceExhausted`       | `max_instance_num`, `ip_range`, current resource pool metrics.                                    |
| Slow starts after idle drain | `axern_axnoded_resource_pool_allocate_total{axern_result="miss_sync_create"}` and refill metrics. |
| Cgroup cleanup retries       | `axern_axnoded_gc_queue_current` and cgroup process state.                                        |

## Runtime

`[plugin.runtime]` controls rootfs resolution, the runsc executor, DNS materialization and warm idle environment retention.

| Key | Meaning | Notes |
| --- | --- | --- |
| `image_manager_enabled` | Enables `imagemgr` for image-backed rootfs and inventory. | Defaults to true. Set false for local-rootfs-only setups. |
| `image_lib_dir` | Local rootfs/image library directory. | Used by image/rootfs flows under axnoded. |
| `image_manager_socket` | Unix socket for `imagemgr`. | Ignored when `image_manager_enabled = false`; default is `/var/run/imagemgr.sock`. |
| `rootfs_snapshot_repository` | Platform-owned OCI repository for requested successful Run rootfs results. | Empty disables the observed rootfs-snapshot capability. The repository uses imagemgr's registry credentials; quota, retention, and unreferenced-blob GC are operator policy. |
| `egress_manager_socket` | Trusted node-local `egressd` Unix socket used for fail-closed sandbox policy lifecycle. | Defaults to `/run/egressd/egressd.sock`; absence keeps policy capabilities unavailable without affecting unrestricted sandboxes. |
| `idle_environment_retention_ttl` | How long idle prepared Environment/rootfs state remains warm. | Empty falls back to `5m`. |
| `idle_environment_retention_max` | Max retained prepared Environments per node. | Defaults to `8`; `<= 0` disables idle retention. Retention keeps rootfs leases and reusable OCI bundle projections, never an allocation-less OCI container. |
| `cgroup_enforcement` | `required` or explicit local-only `disabled_dev`. | Defaults to `required`. `disabled_dev` rejects any workload declaring a memory hard limit. |
| `filestore_mode` | `existing` or `loopback_dev`. | Production uses an existing data-disk mount. Loopback mode is development-only and never reformats an existing image. |
| `filestore_dir` | Runtime writable-storage mount. | Must be a writable independent XFS or ext4 mount; startup performs a real OverlayFS scratch probe. |
| `filestore_loopback_image` | Persistent image used by `loopback_dev`. | Created only when absent and retained after shutdown. |
| `filestore_loopback_size_bytes` | Initial size for a newly created loopback image. | Must be positive in `loopback_dev`. |
| `filestore_system_reserve_bytes` | Capacity unavailable to sandbox Allocation charges. | Admission checks both committed Allocation charges and the live available-space floor. |
| `ephemeral_storage_default_limit_bytes` | Internal default backing limit for the public `limits.ephemeral_storage_bytes` contract. | Writable runsc roots use this in `root:dir=...,size=...`. |

Environment retention is keyed by the resolved execution specification, so namespace, Environment, Run, and Allocation identity do not duplicate the same prepared rootfs entry. It retains only reusable immutable rootfs and OCI bundle-template inputs. OCI create is always Allocation-owned because the container ID, cgroup, network, storage charge, evidence, and cleanup record cannot be safely rebound to a future Allocation.

### Runtime DNS

`[plugin.runtime.dns]` controls resolver files materialized into OCI bundles. When `nameservers` is empty, axnoded derives usable resolvers from the node. If the node exposes only loopback resolvers or no usable resolver, axnoded owns an inert `/etc/resolv.conf` for the sandbox instead of blocking OCI creation or inventing a public fallback. Resolver-independent workloads continue to run; DNS diagnostics and domain-policy forwarding remain unavailable until the node has a verified upstream resolver.

| Key              | Meaning                                      |
| ---------------- | -------------------------------------------- |
| `nameservers`    | Explicit resolver IPs.                       |
| `search_domains` | Search domains written into resolver config. |
| `options`        | Resolver options such as `ndots:5`.          |

Use explicit DNS values on production nodes that require VPC, cluster, or corporate resolvers. For local development, deriving from the node is usually less brittle.

### Runsc Executor

The default, sample, packaged, and devbox configurations use gVisor (`runsc`) as the single execution implementation. Runtime selection is not part of the product or node-local protocol contract, and axnoded does not maintain a backend registry.

`[plugin.runtime.runsc]` declares the runsc binary and startup options. Axern's OCI package owns the workload base policy; there is no external base-spec file or runtime sample generation. Configuration decoding rejects unknown keys and never ignores misspelled settings. Configuration changes require a node restart: conformance identifies the policy loaded by the handler, never a subsequently edited configuration file. Axnoded must load runsc before persistent container inventory is reconciled or the node can become ready. A transient runtime-state or filestore conflict is retried until startup is canceled; axnoded never starts with a partial execution stack.

| Key                            | Meaning                                              |
| ------------------------------ | ---------------------------------------------------- |
| `binary`                       | Runtime binary path, such as `/usr/local/bin/runsc`. |
| `[plugin.runtime.runsc.options]` | runsc-specific options.                              |

There is no per-runtime cgroup fallback. In `required` mode cgroup controller writes, limit readback, and runtime host-PID attribution are fail-closed. For `runsc`, `options.allow_suid = true` maps to `runsc --allow-suid` so setuid tools inside Axern-maintained images, such as `sudo`, can elevate privileges within the sandbox.

See [rootfs-storage.md](rootfs-storage.md) for the system-file, projection, EROFS lower, ephemeral-storage backing, quota, and cleanup contract.

## Common Profiles

| Profile | Key Choices |
| --- | --- |
| Local compose/kind with imagemgr | Keep `image_manager_enabled = true`; point `image_manager_socket` at the dev socket mounted into axnoded; keep `nat_backend = "iptables"` unless testing bpfnet. |
| Local rootfs only | Set `image_manager_enabled = false`; make sure requests use local rootfs paths; keep `image_lib_dir` harmless. |
| eBPF dataplane verification | Set `nat_backend = "ebpf"`; confirm bpffs, privileged host access, both TC directions, and current pinned maps/programs. Every required path is fail-closed. |
| Control-plane connected node | Set `control_plane_target`, stable `control_plane_node_id`, reachable `control_plane_node_target`, enrollment endpoint, trust domain, CA path, labels, and optional extension capabilities. Supply a separate one-time enrollment token file for first registration; ordinary operation uses the durable Node identity. Platform capabilities come from observed providers. |
| Kubernetes production node | Set `control_plane_node_resource_source = "kubernetes"` and pass the Kubernetes Node name; the Helm chart does this by default and grants read-only `nodes/get` RBAC. |
| Production node | Move `rootDir`, `storeDir`, and `image_lib_dir` to durable host paths for runtime recovery; set explicit DNS if node resolvers are not suitable for sandboxes; provide the qualified `memory_system_reserve_bytes` receipt value. |

## Troubleshooting By Config Area

| Problem Shape                     | Likely Config Area                                       | Useful Checks                                                                  |
| --------------------------------- | -------------------------------------------------------- | ------------------------------------------------------------------------------ |
| axnoded will not start            | top-level paths, runtime binaries, filestore | axnoded startup logs, path permissions, `runsc --version`.                     |
| node never appears in `controld`  | control plane                                            | `control_plane_target`, node auth/TLS, heartbeat metrics, controld logs.       |
| image-backed rootfs fails         | runtime image manager                                    | `image_manager_enabled`, `image_manager_socket`, imagemgr logs, imagefsd logs. |
| sandbox has no egress             | network                                                  | `nat_backend`, `ip_range`, `sandbox0`, iptables/bpfnet logs.                   |
| start is slow after burst         | resource pool                                            | idle gauges, `miss_sync_create`, reconcile interval, cache sizes.              |
| delete leaves resources behind    | resource/runtime paths                                   | typed resource ledgers, storeDir, cleanup logs, GC queue metric.                |

For local compose/kind command examples, use [Local Troubleshooting](../../../deploy/local/troubleshooting.md).

Bootstrap input is not node configuration. Pass `-enrollment-token-file <read-only-file>` (container entrypoint: `AXNODED_ENROLLMENT_TOKEN_FILE`). It is opened only before the node certificate is published; renewal and restart do not depend on it. A malformed, expired, or mismatched existing identity fails closed and never falls back to enrollment. Registration retries run independently from Allocation recovery and lease enforcement. Renewal is scheduled with 7–8 hours of certificate validity remaining, with bounded jittered exponential retry (up to one minute).
