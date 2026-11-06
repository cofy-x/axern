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
- Assigned interfaces are not raised, rebuilt, or checked against idle-pool IP and route configuration while loading the ledger. After complete runtime classification, an unreadable or inconsistent live binding fails startup; it is never silently rebuilt as idle capacity.
- An unassigned interface with invalid preparation also fails startup without repair: absent ownership may indicate a lost lease for a live netstack, rather than idle capacity.
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

With required cgroup enforcement, requested CPU quota, period and shares are never discarded when the CPU controller is missing. Axnoded writes the workload leaf, reads back its exact `cpu.max` quota/period and the `cpu.weight` produced by the cgroup driver's shares conversion, then repeats that readback and verifies the runsc Sentry/gofer process attribution after runsc create and before returning a prepared container. Missing, unreadable or mismatched quota, period, weight or process attribution fails creation through the existing cleanup path before user code starts. CPU-only limits receive the same checks without requiring a memory limit. Explicit `disabled_dev` retains its development behavior and skips these host cgroup checks; native serving uses `required`. CPU-set controls are outside this readback contract.

These CPU checks fence creation; they do not add a continuous CPU capability audit or revalidate already-running Allocations during recovery. Recovery retains the admitted resource specification and existing kernel cgroups, and rolls back a runtime still in `created` state through the normal failed-start path.

## Task Ceilings

The optional paired node policy `guest_task_limit` / `host_task_limit` applies to every new Allocation, including internal runtime-conformance Allocations. It is not a public resource request or a namespace quota. Both values are zero in existing deployment profiles; the native serving profile enables guest `256` and host `1024`. Enabling the pair requires cgroup-v2 enforcement and positive values from `64` through `65536`.

The host boundary counts Linux tasks, including Sentry and gofer threads. Axnoded sets `pids.max` on the Allocation parent and its workload leaf, reads back both controls before and after runsc create, and verifies Sentry/gofer attribution. The parent ceiling covers the entire Allocation subtree. An unavailable controller, wrong limit or missing attribution prevents workload activation.

The guest boundary counts gVisor tasks, including processes and threads regardless of UID. Runsc uses its in-sandbox cgroup-v2 hierarchy with a private cgroup namespace and a read-only guest cgroup mount. After OCI start, Axnoded uses runsc's host-side control channel to close descendant delegation with `cgroup.max.depth=0` and `cgroup.max.descendants=0`, verifies no descendant cgroups and a bounded task count, then sets and reads back `pids.max`. Sandboxd PID 1 independently waits for the exact protected guest controls before opening any API listener or starting caller code. Guest cgroup subdelegation is intentionally unavailable in this profile. Runtime command or readback failures retain the existing ordered failed-start cleanup path.

The effective pair is recorded in the immutable `AllocationEnforcementManifest` before OCI start. Node restart verifies running Allocations against those persisted values and the exact runtime manifest, host controls, Sentry/gofer attribution and guest controls before restoring image leases or advertising capacity. Recovery never rewrites a live limit to match changed node configuration. A previously protected Allocation remains verified against its original limits even if the node policy changes or is disabled. Enabling the policy refuses existing live Allocations whose manifests have no task limits; drain and recreate those Allocations first. Runtimes still in `created` state follow existing interrupted-start cleanup, while an unreadable or inconsistent running task boundary prevents node startup for operator inspection. These fences do not introduce a continuous task-limit audit or a CPU recovery audit.

## Cgroup Boundary

The Allocation parent is the memory safety boundary; the runtime-created workload leaf is the OCI attribution boundary. Axnoded verifies controls, identity, and PID membership but does not install a second authoritative limit at the leaf.

The memory request covers runtime processes, guest accounting, anonymous and shared memory, kernel memory, lower and writable page cache, dirty pages, and writeback. Node daemons and conformance workloads are charged to the separately bounded system reserve under the same delegated cgroup-v2 root.

Cgroup GC never kills an unexplained process. It retains cleanup debt and retries removal after the runtime barrier. Optional `memory.peak` and PSI data are diagnostic only and never weaken enforcement.

Capability evidence and loss policy are defined by [Observed Capability Providers](../../../docs/architecture/observed-capability-providers.md).

## Network Boundary

The interface manager owns the bridge, host veth pool, sandbox IP pool, and netns binding. Cached interfaces are validated before use. Recycle clears the bridge neighbor entry before returning capacity, and allocation clears it again before reuse; neighbor cleanup failure is observable but does not transfer ownership.

Runsc transfers the prepared namespace's IP addresses into its private netstack during OCI start. A running or paused sandbox therefore does not need to retain those addresses or the corresponding routes on Linux `eth0`. Live recovery audits the durable Allocation-to-lease binding, host veth kind and bridge membership, the peer indices and peer namespace identity, and equality of the named namespace with the runsc Sentry process's network namespace. The namespace stays open throughout the inspection. Recovery does not rewrite addresses, routes, link state, or lease ownership. An interrupted `created` runtime and a terminal runtime retain their existing ordered cleanup paths; a missing assigned interface stays unavailable until complete inventory reconciliation authorizes release.

This audit verifies connected bindings already present in the authoritative network ledger. A live OCI projection referencing a named network namespace without an exact Allocation lease is rejected; that projection cannot recreate or transfer ownership. Isolated Allocations deliberately have no network lease, and the absence of a lease does not independently prove that an Allocation requested isolation.

`nat_backend` selects either the iptables implementation or the bpfnet eBPF implementation. Axnoded never falls back between them implicitly. A required backend that cannot attach or reconcile keeps the node unavailable. Backend behavior is defined by [bpfnet Architecture](../../../network/bpfnet/docs/architecture.md).

## Diagnostics

Use `axctl node resources`, `/inventoryz`, and Prometheus metrics to inspect capacity, ownership, unavailable resources, refill outcomes, and cgroup cleanup debt. Node identity always comes from the explicit control-plane `node_id`, never a hostname or Pod name.
