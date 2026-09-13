# Node Runtime Agent Contract

## Purpose

`runtime/axnoded` owns node-local Allocation execution and resource enforcement. Read the [Node Runtime README](README.md) for subsystem and document routing.

## Ownership Boundaries

- Keep `cmd/axnoded` thin, `internal/app` limited to construction and lifecycle, `internal/api` limited to protocol adapters, and `internal/service` responsible for Allocation orchestration.
- `internal/service` is an implementation-layer name, not a product Service. Every workload path is keyed by a globally unique Allocation ID, and service subpackages must not import `internal/app` or `internal/api`.
- Keep runtime handlers, OCI execution, sandboxd integration, and writable rootfs views under their focused `internal/runtime` subpackages. Runsc is the production runtime; missing requirements fail closed.
- Keep image/rootfs coordination in `internal/langruntime`, reusable resource pools in `internal/resources`, network integration in `internal/network`, and process-owned durable node records in `internal/nodestate`.
- Keep observed node facts in `internal/nodecapability` and shared capability definitions in `lib/go/nodecapability`. Runtime handler declarations and sandboxd operations are separate capability domains.
- Allocation state is one durable record per Allocation. Lifecycle workers enqueue bounded, coalesced observations without waiting for control-plane RPCs and preserve terminal evidence across retry and restart.
- The globally unique Allocation ID is the only execution identity. The lifecycle API, runtime container, cgroup lease, egress policy, tunnel routing, and capability conditions use it directly; do not add target maps, ID prefixes, attempts, generations, labels, or zero-value tests as identity aliases.
- `ControlPlaneAllocationBinding` is the sole node-local proof that controld admitted an Allocation to this node. `AllocationState` owns the execution contract and recovery obligations, while container metadata/status are runtime checkpoints; none of those records, OCI labels, prefixes, or field presence may substitute for the binding or reconstruct another missing authority.
- Every runtime container checkpoint carries an explicit recovery mode. A durable container requires both `AllocationState` and an exact control-plane binding; node-local sessions and conformance probes are discard-on-restart and may never be reported as Allocations.
- Egressd alone persists the exact Allocation policy/IP record. Axnoded persists no digest/revision proof copy and reconciles egressd against recovered durable Allocation executions.
- Sandboxd readiness and operation support come from the live per-Allocation Unix socket. Never persist them as container labels or infer them from a runtime annotation.
- Keep only the narrow terminal lifecycle outbox required to bridge runtime cleanup and control-plane acknowledgement. Inventory, locality, sandboxd diagnostics, and other runtime/kernel observations are rebuildable projections and must not gain another durable cache.
- Keep test adapters in explicit test-support packages and keep production packages free of bridge aliases, catch-all helpers, and convenience `pkg` layers.
- Treat proto, config, runtime registration, capability, image-manager, and network changes as cross-owner contracts; update their authoritative code and documents together.

## Validation

- Run `make fmt`, `make vet`, and targeted Go tests for ordinary changes; run `make check-architecture` for package or layering changes.
- On non-Linux hosts, use `make test-host` plus affected Linux-target compile checks. Validate runtime, cgroup, network, DNAT, and rootfs behavior through the relevant privileged Linux target selected by `make verify-changed`.
- Use [Verification](docs/verification.md) for the runtime truth matrix.
