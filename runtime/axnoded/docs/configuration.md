# Configuration

This document explains the operator-facing meaning of
[`sample_conf.toml`](sample_conf.toml). Keep the sample file runnable and use
this page for context, profiles, and troubleshooting hints.

`axnoded` starts from `config.DefaultConfig()`, then overlays the TOML file
passed by `-config`. If `-config` is omitted, it reads
`<root>/config.toml`, where `-root` defaults to `/var/lib/axnoded`.

Daemon flags such as `-socket`, `-grpc-address`, `-http-address`,
`-log-level`, and `-log-file` are not TOML fields. They control process
endpoints and logging for the current daemon invocation.

## File Roles

| File | Role |
| --- | --- |
| [`sample_conf.toml`](sample_conf.toml) | runnable reference config for local and generic Linux nodes |
| [`../config/config.go`](../config/config.go) | TOML schema, normalization helpers, and default overlay behavior |
| [`../config/defaults.go`](../config/defaults.go) | default values used before TOML overlay |
| [Resource Handling](resource.md) | cgroup/interface pool behavior behind `[plugin.resource]` and `[plugin.network]` |
| [Observed Capability Providers](../../../docs/architecture/observed-capability-providers.md) | platform observation and structured extension capability contract |
| [Runtime Logs](../../../docs/operations/runtime-logs.md) | cross-component log locations and meanings |
| [Local Troubleshooting](../../../deploy/local/troubleshooting.md) | compose/kind troubleshooting commands |

## Top-Level Paths

| Key | Meaning | Change When |
| --- | --- | --- |
| `rootDir` | Sandbox runtime state, OCI bundles, and container roots. | Moving axnoded state to another disk or mount. |
| `storeDir` | Persistent metadata store used for restart recovery. | Separating metadata from heavier runtime state. |

If either path is wrong or not writable, startup, restart recovery, and
container delete cleanup can fail early. In compose/kind, confirm the path is
inside the mounted dev volume before treating it as a runtime bug.

## Control Plane

`[plugin]` contains the optional outbound relationship to `controld`.

| Key | Meaning | Notes |
| --- | --- | --- |
| `control_plane_target` | `controld` node-control endpoint. | Empty disables the reporter. |
| `control_plane_node_id` | Stable node identity reported to `controld`. | Empty falls back to hostname. |
| `control_plane_node_target` | Internal address that `gatewayd` and `controld` can use to reach this node. | Needed when gateway forwarding crosses host boundaries. |
| `control_plane_node_auth_token` | Node auth token for control-plane registration. | Required by secured control-plane deployments. |
| `control_plane_heartbeat_interval` | Node report interval. | Empty or non-positive falls back to `5s`. |
| `control_plane_node_resource_source` | Source for reported node capacity, allocatable resources, and placement labels. | Use `kubernetes` in Kubernetes deployments so axnoded reports Node API `status.capacity`, `status.allocatable`, and `metadata.labels`; use `host` for local or bare-metal nodes. |
| `control_plane_kubernetes_node_name` | Kubernetes Node object name used when `control_plane_node_resource_source = "kubernetes"`. | In Helm deployments this is populated from `spec.nodeName`; otherwise empty falls back to `control_plane_node_id`. |
| `control_plane_tls_ca_cert` | CA certificate path for control-plane TLS. | Use with secured `controld` endpoints. |
| `control_plane_tls_cert` | Client certificate path. | Pair with `control_plane_tls_key`. |
| `control_plane_tls_key` | Client private key path. | Pair with `control_plane_tls_cert`. |
| `control_plane_node_state` | Advertised scheduling state. | Valid values are `ready`, `draining`, and `disabled`; invalid values normalize to `ready`. |
| `[[node_extension_capabilities]]` | Exact-match extension facts using `name` and optional `value`. | Names must use `<dns-domain>/<name>`; Axern-owned domains are rejected. Platform capabilities cannot be configured. |
| `[plugin.control_plane_node_labels]` | Explicit placement labels. | Empty keys are ignored and values are trimmed. Explicit labels override labels collected from the Kubernetes Node object. |

Check the deployment Prometheus/LGTM metrics exported through OTEL, such as
`axern_axnoded_control_plane_rpc_total` and
`axern_axnoded_control_plane_report_total`, plus axnoded logs when registration
or heartbeat behavior looks wrong. For allocation status delivery, inspect
`/control-planez` and
`axern_axnoded_allocation_status_oldest_pending_age_seconds`,
`axern_axnoded_allocation_status_consecutive_failures`, and
`axern_axnoded_allocation_status_retry_delay_seconds`.

Extension capability declarations are config-static facts. Platform facts are
owned by probes and derived policy, and therefore have no configuration list or
operator override.

## Network

`[plugin.network]` configures sandbox bridge/veth networking.

| Key | Meaning | Notes |
| --- | --- | --- |
| `ip_range` | CIDR used for `sandbox0`, sandbox IPs, and host veth allocation. | Must not collide with host, pod, service, or VPC ranges. |
| `nat_backend` | NAT implementation. | Valid values are `iptables` and `ebpf`. |

`iptables` is the legacy full bridge SNAT/DNAT path. `ebpf` keeps the same
bridge/veth/netns shape, but uses `bpfnet` for supported tc/cgroup dataplane
paths and delegates unsupported compatibility work to the iptables backend.

`[plugin.network.ebpf]` is only active when `nat_backend = "ebpf"`.

| Key | Meaning | Notes |
| --- | --- | --- |
| `pin_path` | bpffs pin root for bpfnet maps and programs. | Default is `/sys/fs/bpf/axern/bpfnet`. |
| `map_size` | bpfnet map capacity. | Increase only when service/rule cardinality requires it. |
| `snat_map_size` | egress SNAT forward/reverse map capacity. | Size for short-connection flow churn; default is `262144`. |
| `snat_gc_interval` | Background interval for axnoded to remove stale bpfnet SNAT mappings. | Empty or non-positive falls back to `1s`. |
| `snat_tcp_idle_timeout` | Idle timeout for active TCP SNAT mappings. | Keep long enough for pooled/keep-alive connections; default is `5m`. |
| `snat_tcp_closing_timeout` | Idle timeout after a TCP mapping observes FIN/RST. | Lower values release translated ports faster for short-connection churn; default is `2s`. |
| `snat_datagram_idle_timeout` | Idle timeout for UDP and ICMP SNAT mappings. | Default is `10s`; increase for long-idle UDP or QUIC-like traffic. |
| `uplink_devices` | Optional uplink device allowlist. | Leave empty for auto/default behavior. |
| `native_routing_cidrs` | CIDRs that should use native routing behavior. | Deployment-specific; keep empty unless the dataplane requires it. |
| `local_out_compat` | Enables host-local TCP hostPort compatibility through cgroup sock_addr eBPF. | Keep enabled for localhost hostPort checks. |
| `iptables_fallback` | Allows fallback to the full iptables path when attach or feature probing fails. | Useful for heterogeneous dev and CI hosts. |

When networking fails, inspect the selected backend, `sandbox0`, host veths,
DNAT/SNAT rules, and bpfnet attach logs before changing resource sizing.

## Resource Pool

`[plugin.resource]` controls node-local cgroup and interface warm pools. The
implementation details are covered in [Resource Handling](resource.md).

| Key | Meaning | Notes |
| --- | --- | --- |
| `cgroup_cache_size` | Idle cgroup target. | `0` disables the pool and is valid only when every loaded runtime ignores cgroups; must not exceed `max_instance_num`. |
| `interface_cache_size` | Idle interface target. | `0` disables the pool and is valid only when no loaded runtime requires interfaces; must not exceed `max_instance_num`. |
| `cgroup_root_name` | Cgroup root path managed by axnoded. | Defaults to `/sandbox`. |
| `max_instance_num` | Positive hard cap for using plus idle resources. | Must not exceed the container hard limit; exhaustion can also come from IP capacity. |
| `resource_pool_reconcile_interval` | Reconcile interval for filling warm pools. | Empty or non-positive falls back to `1s`. |
| `recycle_policy` | Cgroup recycle mode. | `reuse` returns deleted cgroups to idle; `destroy` sends them to GC. |

Useful symptoms:

| Symptom | Start With |
| --- | --- |
| `ErrResourceExhausted` | `max_instance_num`, `ip_range`, current resource pool metrics. |
| Slow starts after idle drain | `axern_axnoded_resource_pool_allocate_total{axern_result="miss_sync_create"}` and refill metrics. |
| Cgroup cleanup retries | `axern_axnoded_gc_queue_current` and cgroup process state. |

## Runtime

`[plugin.runtime]` controls rootfs resolution, OCI runtime handlers, DNS
materialization, volumed integration, and warm idle runtime retention.

| Key | Meaning | Notes |
| --- | --- | --- |
| `image_manager_enabled` | Enables `imagemgr` for image-backed rootfs and inventory. | Defaults to true. Set false for local-rootfs-only setups. |
| `image_lib_dir` | Local rootfs/image library directory. | Used by image/rootfs flows under axnoded. |
| `image_manager_socket` | Unix socket for `imagemgr`. | Ignored when `image_manager_enabled = false`; default is `/var/run/imagemgr.sock`. |
| `runtime_runner_binary` | Host lifecycle helper used to monitor OCI init/runtime processes and durably persist their exact wait status. | Defaults to `/usr/local/libexec/axnoded/axnoded-runtime-runner`; packaged node images install it there. |
| `volume_manager_socket` | Local `volumed` Unix socket used to publish resolved node volumes. | Defaults to `/run/volumed/volumed.sock`. |
| `idle_runtime_retention_ttl` | How long idle runtime templates/rootfs state remain warm. | Empty falls back to `5m`. |
| `idle_runtime_retention_max` | Max retained static runtime templates per node. | Defaults to `8`; `<= 0` disables idle retention. Retention keeps rootfs leases and bundle templates, never an allocation-less OCI container. |
| `cgroup_enforcement` | `required` or explicit local-only `disabled_dev`. | Defaults to `required`. `disabled_dev` rejects any workload declaring a memory hard limit. |
| `filestore_mode` | `existing` or `loopback_dev`. | Production uses an existing data-disk mount. Loopback mode is development-only and never reformats an existing image. |
| `filestore_dir` | Runtime writable-storage mount. | Must be a writable independent XFS or ext4 mount; startup performs a real OverlayFS scratch probe. |
| `filestore_loopback_image` | Persistent image used by `loopback_dev`. | Created only when absent and retained after shutdown. |
| `filestore_loopback_size_bytes` | Initial size for a newly created loopback image. | Must be positive in `loopback_dev`. |
| `filestore_system_reserve_bytes` | Capacity unavailable to sandbox reservations. | Admission checks both committed reservations and the live available-space floor. |
| `ephemeral_storage_default_limit_bytes` | Internal default backing limit for the public `limits.ephemeral_storage_bytes` contract. | Writable runsc roots use this in `root:dir=...,size=...`; writable runc roots require XFS project quota. |

Runtime retention is keyed by the static execution template, so namespace,
service, environment, and allocation-specific volume identity do not duplicate
the same rootfs/template cache entry. It retains only reusable immutable rootfs
and bundle-template inputs. OCI create is always allocation-owned because the
container ID, cgroup, network, storage reservation, evidence, and cleanup
record cannot be safely rebound to a future allocation.

### Runtime DNS

`[plugin.runtime.dns]` controls resolver files materialized into OCI bundles.
When `nameservers` is empty, axnoded derives usable resolvers from the node.

| Key | Meaning |
| --- | --- |
| `nameservers` | Explicit resolver IPs. |
| `search_domains` | Search domains written into resolver config. |
| `options` | Resolver options such as `ndots:5`. |

Use explicit DNS values on production nodes that require VPC, cluster, or
corporate resolvers. For local development, deriving from the node is usually
less brittle.

### Runtime Handlers

`[plugin.runtime.runtimes.<name>]` declares each OCI runtime handler.

| Key | Meaning |
| --- | --- |
| `binary` | Runtime binary path, such as `/usr/local/bin/runsc` or `/usr/bin/runc`. |
| `base_spec` | Base OCI spec used by axnoded when building bundles. |
| `[plugin.runtime.runtimes.<name>.options]` | Runtime-specific options. |

There is no per-runtime cgroup fallback. In `required` mode cgroup controller
writes, limit readback, and runtime host-PID attribution are fail-closed. For
`runsc`, `options.allow_suid = true` maps to `runsc --allow-suid` so
setuid tools inside Axern-maintained images, such as `sudo`, behave the same
way they do under `runc`.

See [rootfs-storage.md](rootfs-storage.md) for the system-file, projection,
EROFS lower, ephemeral-storage backing, quota, and cleanup contract.

## Common Profiles

| Profile | Key Choices |
| --- | --- |
| Local compose/kind with imagemgr | Keep `image_manager_enabled = true`; point `image_manager_socket` at the dev socket mounted into axnoded; keep `nat_backend = "iptables"` unless testing bpfnet. |
| Local rootfs only | Set `image_manager_enabled = false`; make sure requests use local rootfs paths; keep `image_lib_dir` harmless. |
| eBPF dataplane verification | Set `nat_backend = "ebpf"`; keep `local_out_compat = true` and `iptables_fallback = true`; confirm bpffs and privileged host access. |
| Control-plane connected node | Set `control_plane_target`, stable `control_plane_node_id`, reachable `control_plane_node_target`, auth token, TLS paths, labels, and optional extension capabilities. Platform capabilities come from observed providers. |
| Kubernetes production node | Set `control_plane_node_resource_source = "kubernetes"` and pass the Kubernetes Node name; the Helm chart does this by default and grants read-only `nodes/get` RBAC. |
| Production node | Move `rootDir`, `storeDir`, and `image_lib_dir` to durable host paths; run `volumed` with durable state and local volume roots; set explicit DNS if node resolvers are not suitable for sandboxes. |

## Troubleshooting By Config Area

| Problem Shape | Likely Config Area | Useful Checks |
| --- | --- | --- |
| axnoded will not start | top-level paths, runtime binaries, base specs, filestore | axnoded startup logs, path permissions, `runsc --version`, `runc --version`. |
| node never appears in `controld` | control plane | `control_plane_target`, node auth/TLS, heartbeat metrics, controld logs. |
| image-backed rootfs fails | runtime image manager | `image_manager_enabled`, `image_manager_socket`, imagemgr logs, imagefsd logs. |
| sandbox has no egress or hostPort | network | `nat_backend`, `ip_range`, `sandbox0`, iptables/bpfnet logs. |
| start is slow after burst | resource pool | idle gauges, `miss_sync_create`, reconcile interval, cache sizes. |
| delete leaves resources behind | resource/runtime paths | OCI annotations, storeDir, resource cleanup logs, GC queue metric. |

For local compose/kind command examples, use
[Local Troubleshooting](../../../deploy/local/troubleshooting.md).
