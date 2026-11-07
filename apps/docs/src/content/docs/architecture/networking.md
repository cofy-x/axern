---
title: Node Networking
description: Explicit NAT backend selection and ownership for sandbox networking.
---

`axnoded` owns sandbox network namespaces, interfaces and NAT backend selection. The [native Linux installation](https://github.com/cofy-x/axern/blob/main/deploy/standalone/INSTALL.md) configures the iptables backend. The Helm chart defaults to `ebpf`, using the repository's IPv4 `bpfnet` library embedded in `axnoded`. IPv6 requires explicitly selecting iptables.

## eBPF packet paths

| Path | Program |
| --- | --- |
| Sandbox TCP/UDP/ICMP egress SNAT | TC egress |
| Sandbox egress reply restoration | TC ingress |
| Native-routing CIDR skip | TC egress |

## Ownership

`axnoded` owns Allocation lifecycle, bridge/veth/netns resources, backend selection, rollback policy, and SNAT GC scheduling. `bpfnet` owns egress dataplane attach and reconciliation, pinned maps/programs, and status collection. Inbound access uses Allocation-scoped Tunnel or SSH sessions; node-global endpoint publication is not part of the model.

## Backend configuration

The eBPF backend requires both TC directions and all current pinned objects. Set `node.network.natBackend` in Helm values to select `ebpf` or `iptables` for that installation's network profile; use iptables for IPv6. Backend health and any configured rollback are explicit operational states.

The engineering contracts — attach lifecycle, production replacement gates, regression runbook, and alerting — live with the module in the repository's [`network/bpfnet/docs/`](https://github.com/cofy-x/axern/tree/main/network/bpfnet/docs) and the [Helm chart README](https://github.com/cofy-x/axern/tree/main/deploy/helm/axern).
