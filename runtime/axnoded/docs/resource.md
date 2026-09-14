# Resource Management

This document defines the node-local cgroup and network-resource contract. The public request, limit, quota, and placement model is defined by [Resource Model](../../../docs/architecture/resource-model.md); operator settings are defined by [Configuration](configuration.md); executable coverage is defined by [Verification](verification.md).

## Fact Ownership

Every resource binding belongs to one globally unique Allocation ID. Runtime metadata and OCI labels, annotations, paths, prefixes, and field presence are projections and never authorize lookup, recovery, or cleanup.

| Resource | Durable authority | Rebuildable projection |
| --- | --- | --- |
| cgroup | `CgroupLease(allocation_id, cgroup_id, lifecycle, commitment)` | OCI `linux.cgroupsPath`, kernel counters, workload leaf |
| network | `NetworkLease(allocation_id, host_veth, IP, netns)` | link state, neighbor entries, runtime namespace |
| writable storage | filestore Allocation charge ledger and projection manifest | runsc writable root and host mount projection |

`AllocationState` owns the admitted execution specification. Resource ledgers own node-local bindings and cleanup debt. Neither duplicates the other's facts.

## Allocation And Release

Assignment is durably recorded before allocation succeeds. Release is durably recorded before an IP, interface slot, or other reusable capacity returns to a pool. A failed write leaves the resource owned and unavailable for retry.

Warm resources are optimization only. A cache miss may create a resource synchronously, but capacity charging remains atomic and bounded by `max_instance_num`. Only never-assigned cgroups may be reused; an Allocation-owned cgroup moves from `assigned` to `retiring` and is destroyed after cleanup.

Delete resolves bindings by Allocation ID, crosses the runtime and monitor exit barrier, cleans activation-specific network state, releases runtime storage and image ownership, then retires the cgroup. Every step is idempotent. Failure preserves the remaining authoritative records and returns an error to the durable control-plane retry path.

## Recovery

Startup loads the typed ledgers before advertising capacity and reconciles them against one complete runsc and kernel inventory.

- Duplicate or conflicting ownership fails closed.
- A missing assigned host interface remains owned and its IP remains unavailable until complete runtime recovery proves the Allocation absent.
- An unreadable or inconsistent assigned interface fails startup; it is never silently rebuilt as idle capacity.
- Retiring cgroups retain their memory commitment until the exact fenced cgroup is empty and removal succeeds.
- An assigned cgroup never becomes idle during recovery.
- OCI metadata cannot create, transfer, or release resource ownership.

The complete crash-state ordering is defined by [Axnoded Architecture](architecture.md).

## Accounting

Node inventory reports `runtime_slots` as the admission capacity consumed by controld. Enabled resource managers can reduce effective capacity when their pool is smaller or has unavailable resources. Pool-specific counts are diagnostics, not independent control-plane facts.

`active_allocation_ids` contains admitted local Allocations whose lifecycle or terminal acknowledgement is still active. `running_allocation_ids` is a runtime projection. Node-local sessions and conformance probes appear in neither set.

Axnoded reads the immutable `ResourceSpec` from `AllocationState` and applies only runtime-enforced values:

| Field | Node behavior |
| --- | --- |
| `requests.cpu_milli` | relative CPU weight |
| `limits.cpu_milli` | CFS quota and period |
| `requests.memory_bytes` | scheduling commitment for the entire sandbox cgroup |
| `limits.memory_bytes` | parent `memory.max`, zero swap, and group OOM |
| `requests.ephemeral_storage_bytes` | filestore Allocation charge |
| `limits.ephemeral_storage_bytes` | runsc writable-root hard limit |

CPU and memory commitment use requests, falling back to the corresponding limit only when the request is absent. A running Allocation with neither value increments the matching unbounded diagnostic counter.

## Cgroup Boundary

The Allocation parent is the memory safety boundary; the runtime-created workload leaf is the OCI attribution boundary. Axnoded verifies controls, identity, and PID membership but does not install a second authoritative limit at the leaf.

The memory request covers runtime processes, guest accounting, anonymous and shared memory, kernel memory, lower and writable page cache, dirty pages, and writeback. Node daemons and conformance workloads are charged to the separately bounded system reserve under the same delegated cgroup-v2 root.

Cgroup GC never kills an unexplained process. It retains cleanup debt and retries removal after the runtime barrier. Optional `memory.peak` and PSI data are diagnostic only and never weaken enforcement.

Capability evidence and loss policy are defined by [Observed Capability Providers](../../../docs/architecture/observed-capability-providers.md).

## Network Boundary

The interface manager owns the bridge, host veth pool, sandbox IP pool, and netns binding. Cached interfaces are validated before use. Recycle clears the bridge neighbor entry before returning capacity, and allocation clears it again before reuse; neighbor cleanup failure is observable but does not transfer ownership.

`nat_backend` selects either the iptables implementation or the bpfnet eBPF implementation. Axnoded never falls back between them implicitly. A required backend that cannot attach or reconcile keeps the node unavailable. Backend behavior is defined by [bpfnet Architecture](../../../network/bpfnet/docs/architecture.md).

## Diagnostics

Use `axctl node resources`, `/inventoryz`, and Prometheus metrics to inspect capacity, ownership, unavailable resources, refill outcomes, and cgroup cleanup debt. Node identity always comes from the explicit control-plane `node_id`, never a hostname or Pod name.
