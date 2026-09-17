# Node Runtime Agent Contract

## Purpose

`runtime/axnoded` owns node-local Allocation execution, recovery, and resource enforcement. Use the [Node Runtime README](README.md) for commands.

## Task Routes

| Task | Required local context |
| --- | --- |
| Sandboxd process, wait/reap, streams, file operations, or PTY | [Sandbox Daemon](docs/sandbox-daemon.md) |
| Runtime creation, recovery, cleanup, or package boundaries | [Node Architecture](docs/architecture.md) |
| Resource pools, cgroup ownership, or network backend | [Resource Management](docs/resource.md) |
| Writable rootfs, image ownership, or storage cleanup | [Rootfs Storage](docs/rootfs-storage.md) |
| Capability observation, admission, or enforcement loss | [Observed Capability Providers](../../docs/architecture/observed-capability-providers.md) |
| Node identity, registration, renewal, or revocation | [Node Identity Ownership And Recovery](../../control/controld/README.md#node-identity-ownership-and-recovery), [Node Configuration](docs/configuration.md) |
| Node API authorization or operator/debug access | [Authorization](../../docs/architecture/authorization.md), [Operator Authority Boundary](../../docs/decisions/node-operator-authority-boundary.md) |

## Ownership Boundaries

- Allocation ID is the only execution identity across runtime, cgroup, egress, tunnels, and capability enforcement; never infer identity from labels, names, prefixes, attempts, generations, or zero values.
- `AllocationState` is the sole durable node admission and recovery record. Runtime and kernel state are projections; retain only the terminal delivery intent needed to bridge cleanup and control-plane acknowledgement.
- Use the single runsc executor and typed sandboxd clients. Missing identity, authority, runtime match, or enforcement evidence fails closed.
- Lifecycle, sandbox, operator, machine, and conformance authorities remain separate and least-privilege. Diagnostic Exec/Wait reuses Allocation-scoped processes; destructive operator actions cannot bypass reporting or cleanup ownership.
- Validate typed input before side effects, keep secrets and behavior out of labels or JSON side channels, and do not duplicate egressd, sandboxd, or shared capability truth.
- Node identity is explicit and never falls back from an established identity to bootstrap material. Identity maintenance cannot block recovery or ExecutionLease enforcement.

## Validation

Use [Runtime Verification](docs/verification.md) and `make verify-changed`; host-only tests do not prove runtime, cgroup, network, rootfs, SSH, or Tunnel behavior on Linux.
