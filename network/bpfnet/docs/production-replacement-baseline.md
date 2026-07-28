# bpfnet Production Replacement Baseline

This document is the reusable production replacement baseline for the bpfnet
dataplane. It intentionally avoids environment-specific cluster names, regions,
image tags, registry names, kubeconfigs, and rollout revisions.

Use [Production Regression Runbook](production-regression-runbook.md) for the
repeatable Kubernetes command matrix and [Production Alerting](production-alerting.md)
for the runtime alert policy.

## Decision

bpfnet is production-usable as the default Axern NAT dataplane on real Linux
Kubernetes nodes. It can replace the `iptables` backend for Axern service and
sandbox traffic when the acceptance gates in this document pass.

Use these Helm values:

- `node.network.natBackend = ebpf`
- `node.network.ebpf.localOutCompat = true`
- `node.network.ebpf.iptablesFallback = true`
- `node.network.ebpf.snatGcInterval = 1s`
- `node.network.ebpf.snatTcpClosingTimeout = 2s`
- `node.network.ebpf.snatDatagramIdleTimeout = 10s`

The equivalent axnoded config shape is:

- `nat_backend = "ebpf"`
- `local_out_compat = true`
- `iptables_fallback = true`
- `snat_gc_interval = "1s"`
- `snat_tcp_closing_timeout = "2s"`
- `snat_datagram_idle_timeout = "10s"`

`iptables` remains the explicit rollback backend. A mode that includes
`localhost-tcp-iptables-compat` is acceptable on kernels where the localhost
TCP cgroup path is unavailable. `iptables-full-fallback` is not acceptable for
production replacement because it means the eBPF dataplane did not take over the
main TC ingress/egress path.

## Validation Envelope

The baseline applies to:

- real Linux Kubernetes nodes running the `axnoded` `node-all-in-one`
  DaemonSet;
- `runsc` workloads launched by the axnoded Kubernetes benchmark harness;
- bpfnet service ingress, external ingress, sandbox TCP/UDP/ICMP egress, SNAT
  close-path cleanup, and DaemonSet rollout recovery;
- benchmark Jobs that use the `axnoded-verify` image, not the production
  `node-all-in-one` image.

## Acceptance Gates

Every production replacement validation must satisfy these gates:

| Gate | Required result |
| --- | --- |
| eBPF benchmark failures | `0` |
| `snatAllocExhausted` | `0` |
| `snatTcpReverseMissSynAcks` | `0` |
| `snatTcpNonSynMissFwdHostMismatches` | `0` |
| Post-GC TCP SNAT maps | forward/reverse maps drain to `0` |
| UDP short-message post-GC state | UDP reverse entries and translated ports drain to `0` after datagram idle timeout plus GC grace |
| Node readiness | every `node-all-in-one` pod returns `bpfnetctl check --json` with `.ok=true` |
| Dataplane mode | TC ingress/egress mode, not `iptables-full-fallback` |

Late TCP non-SYN misses can be healthy close-path tail traffic when failures are
zero and the marker-gated risk counters above stay zero. Treat them as a bug
only when they correlate with failures, host mismatches, reverse SYN-ACK misses,
allocator exhaustion, or persistent post-GC map retention.

## Performance Baseline

These numbers are the reference replacement baseline for future regressions.
New validation runs do not need to beat every number, but they must stay in the
same performance class and must satisfy the acceptance gates.

### TCP Short-Connection Churn

This is the most important replacement proof because it stresses SNAT
allocation, reverse lookups, close-path cleanup, and client ephemeral-port
pressure.

| Path | Backend | Requests | Concurrency | Client namespaces | RPS | P95 | Failures |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `egress_tcp_short_multi_client` | `iptables` | 90000 | 64 | 2 | 2336.246 | 46.768 ms | 2 |
| `egress_tcp_short_multi_client` | `ebpf` | 90000 | 64 | 2 | 3096.424 | 48.717 ms | 0 |

Reference delta for eBPF:

- throughput: `+32.538%`
- P95 latency: `+4.169%`
- failure count: lower than iptables in the same shape

Required eBPF risk counters:

| Metric | Required result |
| --- | ---: |
| `snatAllocExhausted` | 0 |
| `snatTcpReverseMissSynAcks` | 0 |
| `snatTcpNonSynMissFwdHostMismatches` | 0 |
| `snatMapPostGc.fwdEntries` | 0 |
| `snatMapPostGc.revEntries` | 0 |

### TCP Steady-State Reuse And Pool

Steady-state TCP reuse and pool paths represent SDK and HTTP-client style
production traffic. bpfnet is expected to be comparable to iptables here; it
does not need to be faster than iptables for replacement.

| Path | iptables RPS | eBPF RPS | RPS Delta | iptables P95 | eBPF P95 | P95 Delta | Failures |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `egress_tcp_reuse` | 21085.744 | 20594.147 | -2.331% | 12.460 ms | 13.948 ms | +11.943% | 0 / 0 |
| `egress_tcp_pool` | 19693.196 | 19495.942 | -1.002% | 13.609 ms | 14.768 ms | +8.516% | 0 / 0 |

Both paths must keep `snatAllocExhausted=0`,
`snatTcpReverseMissSynAcks=0`, and post-GC forward/reverse maps at `0`.

### TCP eBPF Soak

Repeated eBPF-only runs prove the replacement path is stable without relying on
a single successful comparison run.

| Path | Runs | Requests | Failures | Avg RPS | Avg P95 | Max P95 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `egress_tcp_short_multi_client` | 6 | 540000 | 0 | 3005.731 | 46.606 ms | 47.130 ms |
| `egress_tcp_reuse` | 6 | 540000 | 0 | 23043.692 | 5.445 ms | 5.494 ms |
| `egress_tcp_pool` | 6 | 540000 | 0 | 21665.267 | 5.932 ms | 6.102 ms |

Risk counters must stay quiet across soak:

| Path | `snatAllocExhausted` | `snatTcpNonSynMissFwdHostMismatches` | `snatTcpReverseMissSynAcks` |
| --- | ---: | ---: | ---: |
| `egress_tcp_short_multi_client` | 0 | 0 | 0 |
| `egress_tcp_reuse` | 0 | 0 | 0 |
| `egress_tcp_pool` | 0 | 0 | 0 |

### Ingress And UDP Coverage

These paths prove the replacement is not only a TCP-short optimization. eBPF
should remain broadly comparable to iptables and must keep zero failures.

| Path | iptables RPS | eBPF RPS | RPS Delta | iptables P95 | eBPF P95 | P95 Delta | Failures |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `external_tcp_ingress` | 3132.024 | 3071.695 | -1.926% | 33.641 ms | 32.735 ms | -2.693% | 0 / 0 |
| `external_udp_ingress` | 21982.432 | 20897.137 | -4.937% | 5.011 ms | 5.736 ms | +14.478% | 0 / 0 |
| `egress_udp` | 9270.642 | 9093.947 | -1.906% | 14.283 ms | 14.391 ms | +0.761% | 0 / 0 |
| `egress_udp_connected` | 16005.389 | 16318.341 | +1.955% | 6.888 ms | 6.671 ms | -3.160% | 0 / 0 |

## UDP Retention Baseline

The default UDP/ICMP retention policy is optimized for short-message burst
traffic:

- `snat_datagram_idle_timeout = 10s`
- `snat_gc_interval = 1s`
- benchmark post-GC grace: `BENCHMARK_SNAT_POST_GC_WAIT = 12s`
- required observability:
  - `snatMap*.fwdUdpEntries`
  - `snatMap*.revUdpEntries`
  - `snatMap*.udpTranslatedPortsUsed`

Reference unconnected UDP burst:

| Path | Requests | Concurrency | RPS | P95 | Failures | `snatMappingsProgrammed` | `snatAllocExhausted` |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `egress_udp` | 10000 | 64 | 8531.318 | 16.565 ms | 0 | 9103 | 0 |

Required map behavior:

| Snapshot | fwd entries | rev entries | fwd UDP | rev UDP | UDP translated ports |
| --- | ---: | ---: | ---: | ---: | ---: |
| `snatMapAfter` | bounded by the idle window | bounded by the idle window | no unexpected growth | may retain until idle timeout | may retain until idle timeout |
| `snatMapPostGc` | 0 | 0 | 0 | 0 | 0 |

Use a larger explicit `snat_datagram_idle_timeout` only for workloads that need
long-idle UDP or QUIC-like behavior. Do not change the short-message default to
mask workload-specific long-idle requirements.

## Rollout And Recovery Baseline

Production replacement requires the operational path to be predictable:

- rolling restart of `node-all-in-one` returns every DaemonSet pod to ready;
- `bpfnetctl check --json` returns `.ok=true` on every node after rollout;
- `bpfnetctl status --json` shows TC ingress/egress attached on every node;
- pinned maps and pinned programs are ready on every node;
- SNAT forward/reverse maps are `0` after post-GC smoke;
- eBPF-only post-rollout smoke across `egress_tcp_reuse`, `egress_tcp_pool`,
  and `egress_udp` has zero failures.

The reference post-rollout smoke result:

| Path | Requests | Failures | RPS | P95 | Post-GC fwd | Post-GC rev |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| `egress_tcp_reuse` | 10000 | 0 | 13452.519 | 8.213 ms | 0 | 0 |
| `egress_tcp_pool` | 10000 | 0 | 14221.048 | 7.897 ms | 0 | 0 |
| `egress_udp` | 10000 | 0 | 9446.183 | 13.961 ms | 0 | 0 |

## Future Hardening

These items strengthen production confidence but do not block replacing
iptables for the validated Axern traffic shape:

- multi-hour eBPF soak under mixed service and sandbox traffic;
- node hard reboot and node replacement validation;
- long-idle UDP and QUIC-like workloads with explicit datagram idle timeout;
- additional Kubernetes node kernel versions;
- larger cluster scale and higher cross-node concurrency.

Future validation should update this document only when the acceptance gates or
replacement baseline changes. Keep environment-specific command transcripts,
kubeconfigs, image tags, and registry names outside this document.
