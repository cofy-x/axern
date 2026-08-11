# Resource Management

This document is the resource-management contract for `axnoded`. Use it before
changing cgroup allocation, bridge/veth allocation, warm-pool sizing, delete
cleanup, resource accounting, or network backend behavior.

For operator-facing config fields, use [Configuration](configuration.md). For
cross-runtime network or bpfnet changes, also read
[`network/bpfnet/AGENTS.md`](../../../network/bpfnet/AGENTS.md).

## Contract

This document covers allocation-owned cgroup domains, the reusable interface
pool, and their OCI claims. Only never-assigned warm cgroups are reusable;
every cgroup assigned to an allocation is destroyed after cleanup. Ephemeral-storage reservations and hard limits are a separate
filestore lifecycle described in [Rootfs And Writable Storage](rootfs-storage.md).

The reusable pool manager owns these two claim types for each sandbox:

| Resource | Owner | Durable claim |
| --- | --- | --- |
| cgroup | `internal/resources.CgroupManager` plus `internal/cgroup` drivers | `io.axnoded.resource/cgroup` and OCI `linux.cgroupsPath` |
| interface | `internal/resources.InterfaceManager` plus `internal/network` backends | `io.axnoded.resource/interface` |

Resource claims are written into OCI annotations with this prefix:

```text
io.axnoded.resource/
```

The stored OCI spec is authoritative for cgroup and interface cleanup claims.
Delete, housekeeping, and retry paths collect those claims from the spec, not
from an in-memory-only allocation record. It is not the entire allocation
recovery contract: nodestate owns the allocation dependency and runtime/image
record, while the filestore ledger and projection manifest own writable-storage
reservation, project ID, and mount cleanup state.

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

- `CgroupManager` loads the durable `idle / assigned / retiring` lease ledger.
  Assigned ownership must agree with recovered OCI claims. Retiring leases keep
  their memory commitment and resume reclaim/removal after restart.
- `InterfaceManager` loads persisted using IDs from the `network_interfaces` store
  bucket, scans host veths, returns non-using veths to the idle queue, and
  rebuilds the idle IP queue from `ip_range`.
- Managers periodically persist using IDs when `storeMark` is set.
- Before serving, the container manager reconciles every persisted assigned
  lease against recovered OCI resource claims. Ownership ambiguity fails node
  startup instead of advertising inconsistent capacity. An assigned cgroup is
  never converted back to idle during recovery.
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

Delete first collects resource claims, crosses the runtime/monitor exit-state
barrier, releases rootfs, volume, storage and image leases, and only then moves
the allocation cgroup to retiring.

Delete rules:

- Collect claims from OCI `config.json` annotations before runtime delete.
- If the cgroup annotation is missing, fall back to `linux.cgroupsPath`.
- Clean activation-specific network rules before final container cleanup.
- Every cleanup operation is idempotent so a partially successful
  multi-resource cleanup can be retried safely. Interface claims may return to
  the warm pool; allocation-owned cgroups may not.
- Interface recycle treats an already-absent host veth as successful cleanup.
  Runtime netns deletion can remove the peer before the explicit node cleanup
  runs; restoring ownership in that case would create a permanent ghost claim.
- A late exit event after an explicit delete treats an already-removed
  container bundle as cleanup complete when the in-memory container is also
  absent.
- A successful runtime delete does not immediately release memory capacity.
  The retiring cgroup remains committed at `max(original request,
  memory.current)` until it contains no process, dirty/writeback converges,
  `memory.reclaim` drains charged cache, and removal succeeds.
- Successful final cleanup schedules a coalesced inventory refresh and node
  report. Create needs no matching refresh because its durable reservation
  accounts for the slot before node startup begins.
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
| `requests.memory_bytes` | total sandbox cgroup scheduling reservation and node-local commitment |
| `limits.cpu_milli` | cgroup CFS quota/period hard CPU ceiling |
| `limits.memory_bytes` | total sandbox host-cgroup hard limit; swap is disabled and group OOM is enabled |
| `requests.ephemeral_storage_bytes` | control-plane and node-local filestore reservation |
| `limits.ephemeral_storage_bytes` | runsc `size=` or runc XFS project-quota hard limit |

`ephemeral_storage_bytes` is the public sandbox-lifetime resource. Axnoded
currently charges only the runc writable upper or runsc file-backed root
overlay, including metadata, copy-up, and whiteouts. Persistent volumes,
immutable lower/image cache storage, artifacts, projection placeholders,
tmpfs, and logs are outside this accounting scope. The runtime implementation
may call the charged backing writable storage internally; that implementation
term does not broaden the public resource contract.

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

`CgroupManager` owns cgroup creation, one-allocation ownership, destruction,
persistence, reclaim, and cleanup debt.

Key behavior:

- Root defaults to `/sandbox`.
- IDs are generated under the configured root.
- Existing cgroups are scanned at startup.
- The durable lease ledger records `idle`, `assigned`, or `retiring`, allocation
  ownership, memory request, cgroup identity, current charge, and cleanup error.
- Warm creation may place a never-assigned empty cgroup in `idle`. Assignment is
  a one-way ownership boundary: delete moves it to `retiring`, never back to
  `idle`.
- When the process receives a different delegated root after replacement,
  startup discards only never-assigned leases and leases owned by the internal
  runtime-conformance harness. It recreates the warm pool under the current
  root, while the deterministic conformance preflight removes interrupted
  probe artifacts. Any workload-owned old-root `assigned` or `retiring` lease
  remains a hard startup error until allocations are drained and cleanup debt
  is reconciled; its commitment is never silently released. Ownership is a
  durable typed field and is never inferred from an allocation ID prefix.
- `cgroup_cache_size = 0` disables only warm creation. In required mode the
  manager remains active and creates one-use allocation cgroups on demand.
- GC never kills an unexplained remaining process. It retains the retiring
  lease, retries after the runtime/monitor barrier, waits for dirty/writeback,
  invokes `memory.reclaim`, and removes the empty cgroup.
- `cgroup_enforcement = "required"` is the production default. A declared
  memory limit writes the same `memory.max` to allocation parent and workload
  leaf, writes `memory.swap.max=0`, sets parent and leaf
  `memory.oom.group=1`, reads all
  controls back, and verifies runtime host PID membership. Failure force-deletes
  the sandbox.
- Node startup creates a private probe cgroup under `cgroup_root_name`, writes
  and reads back a memory limit, and removes the probe before publishing the
  typed cgroup-controller fact. Runtime-specific runc/runsc memory-hard-limit
  capability is derived only after a dedicated readonly-root conformance
  sandbox also verifies the runtime-specific host process boundary. Runc
  reconciles its state init PID with the immutable pid-file PID and checks
  membership. Runsc checks Sentry and gofer roles, executable identity, and
  membership; guest workload memory is accounted through Sentry, not through a
  separate guest host PID.
  The conformance workload allocates and touches anonymous memory until a real
  group OOM, then verifies memory-event deltas, monitor exit-state persistence,
  swap prohibition, and cleanup. Storage conformance runs in a separate writable-root sandbox; disabling cgroup
  enforcement for development cannot manufacture memory evidence or suppress
  storage evidence. Every real allocation is verified again after create.
- `requests.memory_bytes` is the complete sandbox memcg reservation. Runc init
  and descendants, runsc Sentry/gofer and guest accounting, anon, shmem, kernel
  memory, EROFS lower page cache, writable-overlay page cache, dirty pages, and
  writeback all consume it. There is no runsc overhead reservation.
- `memory_system_reserve_bytes` covers axnoded, lifecycle monitors, imagemgr,
  imagefsd, volumed, Nydus daemons/cache, and other node-local processes outside
  sandbox cgroups. Production requires a qualification-derived positive value;
  exceeding it blocks new create without terminating existing sandboxes.
- Every process charged to that reserve must run below the same delegated
  cgroup-v2 root and inherit its `internal` child. The packaged all-in-one node
  satisfies this by moving the supervisor and its existing children before
  enabling the sandbox subtree; separate service units must be deployed under
  an equivalent shared delegation. A daemon outside that domain is a deployment
  error because `internal/memory.current` cannot account for it.
- The node memory budget reports physical capacity separately from the resource
  source's allocatable value. The sandbox scheduling base is
  `min(source_allocatable_bytes, delegated_root_limit_bytes)` when the delegated
  limit is finite, minus the system reserve. Physical capacity is a diagnostic
  consistency fact, not extra allocatable memory.
- Per-allocation observations always report `memory.current`. When the host
  kernel exposes `memory.peak`, `peak_available=true` identifies the kernel
  high-water mark. Older cgroup-v2 kernels report the current sample as
  `peak_bytes` with `peak_available=false`; this is an observability limitation,
  not a hard-limit fallback. Enforcement continues to depend on control
  readback, OOM events, cgroup identity, and runtime PID attribution.
- Capacity reporting and hard-limit enforcement are separate contracts.
  Production publishes `CGROUP_V2` with a positive reserve and cgroup-backed
  capacity identity. Explicit `disabled_dev` publishes `DISABLED_DEV` capacity
  with zero reserve, but memory limits remain ineligible because the runtime
  hard-limit capability is unavailable. Because this mode has no
  allocation-owned memcg, allocation CPU/memory usage observations are omitted
  rather than sampling the shared delegated root and fabricating attribution;
  request commitments remain authoritative for local admission.

The catalog, evidence validity, placement dependency, and enforcement-loss
policy for these probes are defined by
[Observed Capability Providers](../../../docs/architecture/observed-capability-providers.md),
not by this pool implementation document.

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
idle, using, target, allocation result, refill result, and GC queue state. Both
JSON surfaces expose `node.node_id`/`node_id` as the exact control-plane node
identity used by placement and allocation reports; tooling must not reconstruct
it from a hostname or Kubernetes Pod name. For compose/kind command examples,
use
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
