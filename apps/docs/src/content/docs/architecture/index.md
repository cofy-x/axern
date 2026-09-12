---
title: Architecture
description: A concise view of Axern's durable control plane and node-local data plane.
---

Axern separates durable product intent from node-local execution. Public clients address `gatewayd`, the unified external gateway for control and Allocation-scoped data-plane traffic. `controld` remains the authority for placement, lifecycle, leases, health, and resource state. Runtime services own the host operations needed to turn that intent into an isolated workload; internal lifecycle and status traffic does not route through `gatewayd`.

```mermaid
flowchart LR
    Clients["CLI · SDKs · Axrun"] --> Gateway["gatewayd\npublic control + data edge"]
    Gateway -->|control APIs + target resolution| Control["controld\ndurable intent + placement"]
    Control --> Postgres[(PostgreSQL)]
    Control -->|lifecycle| Node["axnoded\nsandbox lifecycle"]
    Node -->|status + capabilities| Control
    Gateway -->|Allocation operations| Node
    Gateway -->|client peer| Tunnel["tunneld\nreverse TCP relay"]
    Node -->|node peer| Tunnel
    Node --> Image["imagemgr + imagefsd\nOCI + Nydus"]
    Node --> Runtime["runsc"]
```

## Stable ownership

- **Gateway:** authenticates public clients and forwards control, process, file, archive, terminal, SSH, and Tunnel traffic without owning durable state.
- **Control plane:** persists resources and coordinates placement, leases, health, attempt-fenced status, and cleanup.
- **Node runtime:** owns sandbox processes, filesystems, images, networking (an eBPF NAT dataplane with an explicit iptables rollback; see [Node Networking](/architecture/networking/)), probes, and node-local reconciliation.
- **SDKs:** expose Sandbox ergonomics over the durable `Environment -> Run -> Allocation` chain without creating another workload model.
- **Axrun and higher layers:** own agent tasks, verification, trajectories, rewards, evaluation, and data-synthesis workflows above the execution platform.

This page intentionally stays conceptual. The repository's [runtime architecture](https://github.com/cofy-x/axern/blob/main/docs/architecture/runtime-architecture.md), [resource model](https://github.com/cofy-x/axern/blob/main/docs/architecture/resource-model.md), and [workload lifecycle](https://github.com/cofy-x/axern/blob/main/docs/architecture/workload-lifecycle-sequence.md) are the engineering sources of truth.
