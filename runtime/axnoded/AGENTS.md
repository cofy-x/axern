# Node Runtime Agent Contract

## Purpose

`runtime/axnoded` owns node-local Allocation execution and resource enforcement. Use the [Node Runtime README](README.md) for commands and other document lookup; read the contracts selected by the task below.

## Task Routes

| Task | Required local context |
| --- | --- |
| Sandboxd process, wait/reap, streams, file operations, or PTY | [Sandbox Daemon](docs/sandbox-daemon.md) |
| Runtime creation, persistence, recovery, cleanup, or package boundaries | [Node Architecture](docs/architecture.md) |
| Resource pools, cgroup ownership, or network backend | [Resource Management](docs/resource.md) |
| Writable rootfs, image ownership, or storage cleanup | [Rootfs Storage](docs/rootfs-storage.md) |
| Capability observation, admission, or enforcement loss | [Observed Capability Providers](../../docs/architecture/observed-capability-providers.md) |
| Node identity, registration, renewal, or revocation | [Node Identity Ownership And Recovery](../../control/controld/README.md#node-identity-ownership-and-recovery), [Node Configuration](docs/configuration.md) |
| Node API authorization or operator/debug access | [Authorization](../../docs/architecture/authorization.md), [Operator Authority Boundary](../../docs/decisions/node-operator-authority-boundary.md) |

## Ownership Boundaries

- Keep entrypoints thin, `internal/app` limited to composition, `internal/api` to adapters, and `internal/service` to orchestration. Service packages must not import app or API packages. Package maps belong in Node Architecture.
- Use the single runsc executor and typed sandboxd clients. Do not add an Allocation-selected runtime registry or duplicate shared capability definitions from `lib/go/nodecapability`.
- The globally unique Allocation ID is the only execution identity. The lifecycle API, runtime container, cgroup lease, egress policy, tunnel routing, and capability conditions use it directly; do not add target maps, ID prefixes, attempts, generations, labels, or zero-value tests as identity aliases.
- Validate typed execution inputs before digest computation or side effects; never pack secrets, policy, or execution behavior into JSON or labels.
- `AllocationState` is the sole durable node admission and recovery record. Node-local sessions and conformance probes have no such record, are discarded on restart, and must not be reported as admitted Allocations. Preserve terminal reporting and cleanup obligations across failures.
- Egressd owns its policy record; sandboxd readiness is queried live. Neither may acquire a duplicate authoritative copy in container metadata.
- Preserve Allocation-scoped `Exec`, `ExecStream`, and `Wait` as node-local diagnostic capabilities. They must reuse the normal process/runtime implementation, require the exact Allocation identity, and must not create a second lifecycle authority. Destructive node-local operations are explicit privileged break-glass actions; they may not silently bypass terminal reporting, cleanup ownership, or durable recovery obligations.
- Routable `NodeLifecycle` accepts only controld and `NodeSandbox` only gatewayd. Operator, machine lookup, and conformance sockets remain separate and root-controlled, never world-writable. Production disables conformance; conformance cannot operate admitted Allocations.
- Keep only the narrow terminal lifecycle outbox required to bridge runtime cleanup and control-plane acknowledgement. Inventory, locality, sandboxd diagnostics, and other runtime/kernel observations are rebuildable projections and must not gain another durable cache.
- Keep test adapters in explicit test-support packages and keep production packages free of bridge aliases, catch-all helpers, and convenience `pkg` layers.
- Connected nodes require an explicit Node ID. Existing identity must never read bootstrap material or fall back to enrollment. Bootstrap material is a separate read-only file, not configuration or environment contents; identity maintenance must not block recovery or ExecutionLease enforcement.

## Validation

Use [Runtime Verification](docs/verification.md) for focused commands and the Linux truth matrix. Run targeted package tests and vet for code changes, architecture checks for layering changes, and the affected privileged Linux path for runtime, cgroup, network, rootfs, SSH, or Tunnel behavior. Host-only tests do not prove Linux kernel behavior. Repository-wide gate selection follows the root contract.
