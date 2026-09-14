# Node Runtime Agent Contract

## Purpose

`runtime/axnoded` owns node-local Allocation execution and resource enforcement. Read the [Node Runtime README](README.md) for subsystem and document routing.

## Ownership Boundaries

- Keep `cmd/axnoded` thin, `internal/app` limited to construction and lifecycle, `internal/api` limited to protocol adapters, and `internal/service` responsible for Allocation orchestration.
- `internal/service` is an implementation-layer name, not a product Service. Every workload path is keyed by a globally unique Allocation ID, and service subpackages must not import `internal/app` or `internal/api`.
- Keep the single runsc executor, OCI execution, sandboxd integration, and writable rootfs views under their focused `internal/runtime` subpackages. Axnoded has no runtime registry or per-Allocation runtime selector; missing runsc requirements fail closed.
- Keep image/rootfs coordination in `internal/environmentcache`, reusable resource pools in `internal/resources`, network integration in `internal/network`, and process-owned durable node records in `internal/nodestate`.
- Keep observed node facts in `internal/nodecapability` and shared capability definitions in `lib/go/nodecapability`. Runtime handler declarations and sandboxd operations are separate capability domains.
- Allocation state is one durable record per control-plane-bound Allocation. Node-local sessions and conformance probes are `DISCARD_ON_RESTART` and keep no durable Allocation record. Lifecycle workers enqueue bounded, coalesced observations without waiting for control-plane RPCs and preserve terminal evidence across retry and restart.
- The globally unique Allocation ID is the only execution identity. The lifecycle API, runtime container, cgroup lease, egress policy, tunnel routing, and capability conditions use it directly; do not add target maps, ID prefixes, attempts, generations, labels, or zero-value tests as identity aliases.
- Keep the node execution request typed through the lifecycle and service boundaries. Secrets, registry credentials, ports, network mode, and egress policy must not be packed into JSON or behavior-bearing labels; validate them before computing the Allocation request digest or creating node-local side effects.
- `AllocationState` is the sole node-local proof that controld admitted an Allocation to this exact node and the sole durable owner of its request digest, execution contract, and recovery obligations. Container metadata/status are runtime checkpoints; OCI labels, prefixes, field presence, or a second binding record may not authorize reporting or recovery.
- A runtime container is durable only when the same globally unique ID has an admitted `AllocationState`. Node-local sessions and conformance probes keep no such record, are discarded on restart, and may never be reported as Allocations.
- Egressd alone persists the exact Allocation policy/IP record. Axnoded persists no digest/revision proof copy and reconciles egressd against recovered durable Allocation executions.
- Sandboxd readiness and operation support come from the live per-Allocation Unix socket. Never persist them as container labels or infer them from a runtime annotation.
- Keep only the narrow terminal lifecycle outbox required to bridge runtime cleanup and control-plane acknowledgement. Inventory, locality, sandboxd diagnostics, and other runtime/kernel observations are rebuildable projections and must not gain another durable cache.
- Keep test adapters in explicit test-support packages and keep production packages free of bridge aliases, catch-all helpers, and convenience `pkg` layers.
- Treat proto, config, runsc execution, capability, image-manager, and network changes as cross-owner contracts; update their authoritative code and documents together. A future isolation backend must ship as a separately qualified node implementation/pool, not as a second handler selected from Allocation metadata.

## Validation

- Run `make fmt`, `make vet`, and targeted Go tests for ordinary changes; run `make check-architecture` for package or layering changes.
- On non-Linux hosts, use `make test-host` plus affected Linux-target compile checks. Validate runtime, cgroup, network, DNAT, and rootfs behavior through the relevant privileged Linux target selected by `make verify-changed`.
- Use [Verification](docs/verification.md) for the runtime truth matrix.
