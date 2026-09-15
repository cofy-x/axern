---
title: Node Networking
description: How Axern connects sandboxes to the network — the eBPF NAT dataplane, its packet paths, and the explicit rollback.
---

Axern's node networking uses `bpfnet`, an in-repo IPv4 eBPF egress-NAT library embedded by `axnoded`. It is the default production NAT dataplane on supported Linux nodes. IPv6 requires explicitly selecting the separate iptables backend.

## Packet paths

| Path | Program |
| --- | --- |
| Sandbox TCP/UDP/ICMP egress SNAT | TC egress |
| Sandbox egress reply restoration | TC ingress |
| Native-routing CIDR skip | TC egress |

## Ownership

`axnoded` owns Allocation lifecycle, bridge/veth/netns resources, backend selection, rollback policy, and SNAT GC scheduling. `bpfnet` owns egress dataplane attach and reconciliation, pinned maps/programs, and status collection. Inbound access uses Allocation-scoped Tunnel or SSH sessions; node-global endpoint publication is not part of the model.

## Fallback semantics

Fallbacks are explicit states, not silent degradation. The eBPF backend requires both TC directions and all current pinned objects. The Helm chart defaults `node.network.natBackend` to `ebpf`; set it to `iptables` only as an explicit rollback backend or for IPv6.

The engineering contracts — attach lifecycle, production replacement gates, regression runbook, and alerting — live with the module in the repository's [`network/bpfnet/docs/`](https://github.com/cofy-x/axern/tree/main/network/bpfnet/docs) and the [Helm chart README](https://github.com/cofy-x/axern/tree/main/deploy/helm/axern).
