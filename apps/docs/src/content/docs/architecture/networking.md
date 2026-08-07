---
title: Node Networking
description: How Axern connects sandboxes to the network — the eBPF NAT dataplane, its packet paths, and the explicit rollback.
---

Axern's node networking is handled by `bpfnet`, an in-repo Linux eBPF NAT
dataplane library. It is embedded by `axnoded` rather than running as a
separate daemon, and it is the default production NAT dataplane on supported
Linux nodes. Its scope is IPv4; Linux localhost UDP and IPv6 are outside the
supported design.

## Packet paths

| Path | Program |
| --- | --- |
| External TCP/UDP hostPort DNAT to the sandbox target | TC ingress |
| Service reply source restoration to the node hostPort | TC egress |
| Sandbox TCP/UDP/ICMP egress SNAT | TC egress |
| Sandbox egress reply restoration | TC ingress |
| Host-local TCP hostPort compatibility | cgroup `connect4`, `getpeername4`, `sock_release` |
| Native-routing CIDR skip | TC egress |

## Ownership

`axnoded` owns the sandbox lifecycle, bridge/veth/netns resources, service
hostPort intent, backend selection, rollback policy, and SNAT GC scheduling.
`bpfnet` owns dataplane attach and reconciliation, service-map programming,
pinned maps and programs, and status collection. The `bpfnetctl` binary is
read-only diagnostics; it never writes service intent.

## Fallback semantics

Fallbacks are explicit states, not silent degradation. When the localhost TCP
cgroup path is unavailable, axnoded may use iptables for localhost TCP
compatibility while TC ingress and egress remain on eBPF. A full iptables
fallback is a rollback state, not a successful eBPF replacement. The Helm
chart defaults `node.network.natBackend` to `ebpf`; set it to `iptables` only
as an explicit rollback backend.

The engineering contracts — attach lifecycle, production replacement gates,
regression runbook, and alerting — live with the module in the repository's
[`network/bpfnet/docs/`](https://github.com/cofy-x/axern/tree/main/network/bpfnet/docs)
and the [Helm chart README](https://github.com/cofy-x/axern/tree/main/deploy/helm/axern).
