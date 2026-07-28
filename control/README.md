# Control Plane

This directory contains active Axern control-plane services and coordination components.

Active services and design surfaces:

- [Control Plane](./controld/README.md): Postgres-backed daemon for node
  registration, heartbeat ingest, lifecycle control, gateway route resolution,
  execution leases, and the read-only runtime catalog.
- [Storage Control Plane](./storaged/README.md): Storage V1 module for volume
  classes, claims, bindings, backend coordination, resolved node volume specs,
  publish/release observations, binding health, and failed-binding retry.

The control plane owns durable product metadata, placement, admission,
lifecycle APIs, storage metadata, gateway route resolution, node image
inventory summaries, and revocable execution leases.
Realtime exec, terminal, tunnel, and service HTTP traffic stay outside the
control plane and flow through `gatewayd`.

Node capacity admission consumes the aggregate `runtime_slots` contract
reported by axnoded. The control plane does not infer capacity from individual
cgroup or interface pools, and it rejects node reports that omit the aggregate.
Node identity is durable: operators retire permanently removed nodes through
the audited admin API after allocations, reservations, leases, tunnels, volume
bindings, reclaims, and lifecycle retries have converged. Retired identities
cannot re-register and are excluded from placement and fleet health.

For implementation details, runtime contracts, and local commands, use the
owning subsystem README. Cross-service storage ownership is tracked in
[Storage Architecture](../docs/architecture/storage-architecture.md).
