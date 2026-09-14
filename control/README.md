# Control Plane

This directory contains active Axern control-plane services and coordination components.

Active services and design surfaces:

- [Control Plane](./controld/README.md): Postgres-backed daemon for node registration, heartbeat ingest, lifecycle control, gateway route resolution, execution liveness, and the read-only environment template set. The control plane owns durable product metadata, placement, admission, lifecycle APIs, node execution authority, gateway access grants, and node image inventory summaries. Rootfs locality describes local directories or registry images (OCI/Nydus). Realtime exec, file/archive transfer, terminal, SSH, and tunnel traffic stay outside the control plane and flow through `gatewayd`.

Node capacity admission consumes the aggregate `runtime_slots` contract reported by axnoded. The control plane does not infer capacity from individual cgroup or interface pools, and it rejects node reports that omit the aggregate. Node capability admission consumes the atomic typed observation snapshot in the same report. Runsc memory and ephemeral-storage limits require matching runtime conformance evidence. Platform requirements are derived from workload semantics and rechecked while candidate rows are locked; users may declare only structured extension requirements. See [Observed Capability Providers](../docs/architecture/observed-capability-providers.md). Node identity is durable: operators retire permanently removed nodes through the audited admin API after Allocations, access grants, tunnels, and lifecycle delivery have converged. Retired identities cannot re-register and are excluded from placement and fleet health.

For implementation details, runtime contracts, and local commands, use the owning subsystem README. Generic persistent-volume APIs and coordination are not part of the execution core.
