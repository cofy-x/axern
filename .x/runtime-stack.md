# Runtime Stack

Use this routing index when a change crosses subsystem boundaries. For work contained inside one subsystem, follow its `AGENTS.md` task routes instead. Product object meaning remains authoritative in the [Stable Domain Model](../docs/product/domain-model.md); this page does not define a parallel architecture contract.

Component ownership and traffic topology are defined in [Runtime Architecture](../docs/architecture/runtime-architecture.md). Use the [Module Guide](module-guide.md) to locate code owners; this page only routes cross-component changes.

## Authoritative Contracts

Read only the contracts affected by the change:

- Product identity and lifecycle: [Stable Domain Model](../docs/product/domain-model.md).
- Control and node execution sequences: [Workload Lifecycle](../docs/architecture/workload-lifecycle-sequence.md).
- Trust, operation access, and operator separation: [Authorization](../docs/architecture/authorization.md).
- Capability admission and enforcement: [Observed Capability Providers](../docs/architecture/observed-capability-providers.md).
- Placement quantities and enforcement limits: [Resource Model](../docs/architecture/resource-model.md).
- Egress and DNS enforcement: [Sandbox Network Policy](../docs/architecture/sandbox-network-policy.md).
- Writable data, image mounts, and cleanup: [Storage Architecture](../docs/architecture/storage-architecture.md).

## Change Routing

| Change | Read / update |
| --- | --- |
| Public API, SDK shape, or protobuf contract | `sdk/proto`, generated SDKs, owning module, CLI and public docs |
| Environment, Run, Allocation, placement, lease, or node lifecycle | `control/controld`, `runtime/axnoded`, affected SDK surfaces |
| Capability observation, admission evidence, or enforcement loss | `lib/go/nodecapability`, `runtime/axnoded`, `control/controld`, SDK/proto diagnostics |
| Sandbox DNS, egress policy, NAT, or host networking | `runtime/egressd`, `runtime/axnoded`, `network/bpfnet`, deployment and verification surfaces |
| Writable filesystem reservation, recovery, or cleanup | `runtime/axnoded`, `control/controld`, storage architecture |
| Public gateway, terminal, SSH, file/archive, or Tunnel client path | `gateway/gatewayd`, `control/controld`, `runtime/tunneld`, `runtime/axnoded` |
| Sandboxd process, file, PTY, or proxy behavior | `runtime/axnoded`, sandboxd documentation, SDK/proto when public |
| OCI, Nydus, rootfs, or image mounts | `runtime/axnoded`, `runtime/imagemgr`, `runtime/imagefsd`, SDK/proto when public |
| Devbox sockets or `.dev/` stack layout | Root Make files, devbox scripts, root docs, affected service docs |

A socket, mount target, or `.dev/` layout change updates every owning module that names it. Follow the root [Agent Contract](../AGENTS.md) for synchronized contract changes and validation; this index does not add another verification tier.
