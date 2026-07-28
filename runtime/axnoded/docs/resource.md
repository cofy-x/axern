# Resource Management

This document is the resource-management contract for `axnoded`. Use it before
changing cgroup allocation, bridge/veth allocation, warm-pool sizing, delete
cleanup, resource accounting, or network backend behavior.

For operator-facing config fields, use [Configuration](configuration.md). For
cross-runtime network or bpfnet changes, also read
[`network/bpfnet/AGENTS.md`](../../../network/bpfnet/AGENTS.md).

## Contract

`axnoded` manages two node-local resource classes for each sandbox:

| Resource | Owner | Durable claim |
| --- | --- | --- |
| cgroup | `internal/resources.CgroupManager` plus `internal/cgroup` drivers | `io.axnoded.resource/cgroup` and OCI `linux.cgroupsPath` |
| interface | `internal/resources.InterfaceManager` plus `internal/network` backends | `io.axnoded.resource/interface` |

Resource claims are written into OCI annotations with this prefix:

```text
io.axnoded.resource/
```

The stored OCI spec is the recovery and cleanup contract. Delete,
housekeeping, and retry paths must collect resource claims from the stored spec,
not from an in-memory-only allocation record.

## Ownership

Resource model changes usually move through `internal/resources`,
`internal/container`, and `internal/service/allocation`. Config-facing changes
also touch `config/`, [Configuration](configuration.md), and
[`sample_conf.toml`](sample_conf.toml).

## Startup And Recovery

At service startup, `internal/service` creates the resource manager from config.
The manager enables cgroup and interface managers as configured, then starts a
pool controller for each resizable pool.

Recovery rules:

- `CgroupManager` loads persisted using IDs from the `cgroups` store bucket,
  scans existing cgroups under `cgroup_root_name`, and deletes scanned cgroups
  that are not marked using.
- `InterfaceManager` loads persisted using IDs from the `network_interfaces` store
  bucket, scans host veths, returns non-using veths to the idle queue, and
  rebuilds the idle IP queue from `ip_range`.
- Managers periodically persist using IDs when `storeMark` is set.
- Before serving, the container manager reconciles every persisted pool using
  ID against recovered OCI resource claims. Unclaimed resources are recycled;
  a recycle failure fails node startup so recovery is retried instead of
  advertising inconsistent capacity.
- Startup recovery must be idempotent; a partially deleted sandbox should not
  permanently poison the pool.

## Allocation And Delete

Sandbox start allocates resources through `container.Manager.Occupy`.

Allocation rules:

- Idle pool hits record `result=hit`.
- Idle misses request an async refill with trigger `allocation_miss`.
- Idle misses also attempt one synchronous create so the sandbox can continue
  without waiting for the next reconcile tick.
- Interface creation atomically reserves a slot before slow veth/netns work,
  retains it after successful creation, and releases it only after destruction.
  Concurrent cache misses therefore cannot exceed `max_instance_num`.
- A foreground allocation that reaches capacity while a warm-pool interface is
  still being built waits for that in-flight build and retries from the idle
  pool. The allocation request context bounds this wait; transient refill work
  must not be reported as real node exhaustion.
- If interface creation succeeds but lookup and rollback both fail, the slot
  and IP are quarantined and reported as `PoolState.unavailable`; they are not
  returned to the allocator or hidden as free capacity.
- If no slot or IP remains, allocation returns resource exhausted.
- If allocation fails partway through, `container.Manager.Occupy` releases any
  resources already allocated for that start attempt.

Delete first collects resource claims, then asks the runtime to delete the
container, then releases node-local resources.

Delete rules:

- Collect claims from OCI `config.json` annotations before runtime delete.
- If the cgroup annotation is missing, fall back to `linux.cgroupsPath`.
- Clean activation-specific network rules before final container cleanup.
- Every resource manager's recycle operation is idempotent so a partially
  successful multi-resource cleanup can be retried safely.
- Interface recycle treats an already-absent host veth as successful cleanup.
  Runtime netns deletion can remove the peer before the explicit node cleanup
  runs; restoring ownership in that case would create a permanent ghost claim.
- A late exit event after an explicit delete treats an already-removed
  container bundle as cleanup complete when the in-memory container is also
  absent.
- Successful delete schedules a coalesced inventory refresh and node report so
  placement can reuse a confirmed-free runtime slot without waiting for the
  periodic heartbeat. Create needs no matching refresh because its durable
  reservation accounts for the slot before node startup begins.
- Clear resource annotations and `linux.cgroupsPath` only after every resource
  release succeeds.
- Preserve the OCI spec, claims, and in-memory container when release fails.
  The node delete RPC returns the error so the control plane's durable delete
  worker retries it; the bundle is removed only after cleanup succeeds.

## Resource Accounting

Node inventory reports `runtime_slots` as the admission contract consumed by
controld. Its base capacity is `max_instance_num`; enabled cgroup and interface
pools reduce its effective capacity when they report a smaller capacity or
unavailable slots. Disabled pools do not block inventory and do not constrain
the aggregate. Individual cgroup and interface summaries remain diagnostic
details rather than a control-plane inference contract.
Axnoded rejects startup when a loaded runtime requires a pool disabled by
configuration.

The aggregate `idle` count is the number of runtime slots whose enabled pool
resources are already materialized. It is the minimum idle count across enabled
pools, capped by effective available capacity. When no resource pool is enabled,
all available runtime slots are immediately reusable.

Workload resources arrive from the control plane as `ResourceSpec`, with
separate request and limit semantics. `axnoded` preserves that model in
container status and converts only the runtime-enforced parts into Linux cgroup
settings.

| Field | Local behavior |
| --- | --- |
| `requests.cpu_milli` | cgroup CPU shares for relative fairness |
| `requests.memory_bytes` | scheduling reservation only; not a hard cgroup limit |
| `limits.cpu_milli` | cgroup CFS quota/period hard CPU ceiling |
| `limits.memory_bytes` | cgroup memory hard limit |

Container status stores both scheduler-facing `ResourceSpec` and local
`LinuxResources`. Inventory commitment uses running containers only:

| Resource | Primary source | Fallback |
| --- | --- | --- |
| CPU | `ResourceSpec.requests.cpu_milli` | CFS quota/period, then CPU shares |
| Memory | `ResourceSpec.requests.memory_bytes` | `LinuxResources.memory_limit_in_bytes` |

A running container with neither request nor applicable runtime limit increments
the matching unbounded counter.

## Pool Reconciliation

Each resizable manager owns an idle target and a max capacity:

```text
target = cgroup_cache_size or interface_cache_size
max    = max_instance_num
```

`poolController` keeps idle resources near the target without exceeding max
capacity.

| Trigger | Source |
| --- | --- |
| `periodic` | reconcile ticker, default `1s` |
| `low_watermark` | allocation hit leaves idle count below target |
| `allocation_miss` | allocation finds no idle resource |

| Allocate result | Meaning |
| --- | --- |
| `hit` | resource came from the idle pool |
| `miss_sync_create` | idle pool was empty and synchronous create succeeded |
| `exhausted` | pool reached `max_instance_num` or no IPs remain |
| `error` | create, parse, validate, or rebuild failed |

## Cgroups

`CgroupManager` owns cgroup creation, reuse, destruction, persistence, and GC.

Key behavior:

- Root defaults to `/sandbox`.
- IDs are generated under the configured root.
- Existing cgroups are scanned at startup.
- Using IDs are persisted under the `cgroups` store bucket.
- `recycle_policy = "reuse"` returns deleted cgroups to the idle queue.
- `recycle_policy = "destroy"` removes them from the known set and queues GC.
- GC retries failed removals and tries to kill remaining cgroup processes before
  retrying.

## Interfaces And Network

`InterfaceManager` owns the sandbox bridge, host veth pool, sandbox IP pool,
and netns metadata.

Key behavior:

- The bridge name is `sandbox0`.
- Default CIDR is `172.17.0.1/16`.
- Host veth names are derived from the assigned IP.
- `NetResource` is serialized as JSON in the interface resource annotation.
- Cached interfaces are validated on allocation and rebuilt when stale.
- Recycled interfaces return to the idle queue only after the bridge neighbor
  entry for their IP is cleared. Allocation clears it again before reuse so a
  stale ARP state cannot delay the first host-to-sandbox connection.
- Neighbor cleanup is best effort so a transient netlink failure does not make
  the node unavailable. `axern.axnoded_network_neighbor_reset_total` records
  `cleared`, `absent`, and `error` results for startup load, allocation, and
  recycle boundaries.
- On manager shutdown, idle interfaces are destroyed slowly to reduce host
  impact.

The interface manager always uses the same bridge/veth/netns shape.
`nat_backend` selects rule programming:

| Backend | Behavior |
| --- | --- |
| `iptables` | bridge SNAT, hostPort DNAT, localhost compatibility, and cleanup through iptables |
| `ebpf` | `bpfnet` tc/cgroup dataplane for supported paths, with iptables fallback for unsupported or compatibility paths |

For `ebpf`, `axnoded` configures the bpfnet controller before resource manager
startup. If bpfnet reports that fallback is needed, the eBPF network manager
delegates compatibility work to the iptables backend.

## Inspection

Use `axctl node resources`, `/inventoryz`, and Prometheus metrics to inspect
idle, using, target, allocation result, refill result, and GC queue state. For
compose/kind command examples, use
[Local Troubleshooting](../../../deploy/local/troubleshooting.md).

## Sync Points

When this area changes, keep these in sync as applicable:

- `config/config.go`
- `config/config_test.go`
- `docs/sample_conf.toml`
- `docs/configuration.md`
- `README.md` only when endpoint summaries or document routing changes
- `internal/resources`
- `internal/container`
- `internal/service/allocation`
