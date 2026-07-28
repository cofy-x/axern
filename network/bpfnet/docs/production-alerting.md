# bpfnet Production Alerting

This document defines the durable alert policy for bpfnet as the default Axern
production NAT dataplane. It focuses on low-cardinality signals that distinguish
rollback states and correctness risks from healthy close-path churn.

## Metric Contract

`controld` exports bpfnet node state from axnoded node summaries:

```promql
axern_controld_node_bpfnet_current{axern_node_id, axern_state}
```

`axern_state` is one of:

| State | Meaning | Healthy value |
| --- | --- | ---: |
| `enabled` | node reports bpfnet component enabled | `1` |
| `ready` | TC dataplane is ready and not in full fallback | `1` |
| `snat_fallback` | node needs SNAT fallback | `0` |
| `full_dnat_fallback` | node needs full DNAT fallback or TC is not ready | `0` |
| `localhost_compat` | localhost TCP path uses iptables compatibility | allowed |

`localhost_compat=1` is not a page by itself. It is acceptable on kernels where
the localhost cgroup path is unavailable, as long as `ready=1` and
`full_dnat_fallback=0`.

The existing node count metric remains the cluster-level availability gate:

```promql
axern_controld_nodes_current{axern_state}
```

## Page Alerts

Use these as Alertmanager rules or equivalent monitors in the production
observability stack.

```yaml
groups:
  - name: axern-bpfnet-production
    rules:
      - alert: AxernBPFNetNodeNotReady
        expr: |
          axern_controld_node_bpfnet_current{axern_state="enabled"} == 1
          and on (axern_node_id)
          axern_controld_node_bpfnet_current{axern_state="ready"} == 0
        for: 2m
        labels:
          severity: page
        annotations:
          summary: "bpfnet dataplane is not ready on {{ $labels.axern_node_id }}"
          description: "The node reports bpfnet enabled but not ready. Inspect bpfnetctl check/status on the node pod."

      - alert: AxernBPFNetFullFallback
        expr: axern_controld_node_bpfnet_current{axern_state="full_dnat_fallback"} > 0
        for: 1m
        labels:
          severity: page
        annotations:
          summary: "bpfnet entered full fallback on {{ $labels.axern_node_id }}"
          description: "Full fallback means TC ingress/egress did not take over the main dataplane and is not a successful replacement state."

      - alert: AxernBPFNetSNATFallback
        expr: axern_controld_node_bpfnet_current{axern_state="snat_fallback"} > 0
        for: 1m
        labels:
          severity: page
        annotations:
          summary: "bpfnet SNAT fallback required on {{ $labels.axern_node_id }}"
          description: "Sandbox egress SNAT is not on the expected eBPF path. Inspect attach state and pinned maps/programs."

      - alert: AxernNodeReadinessLost
        expr: |
          sum(axern_controld_nodes_current{axern_state="not_ready"})
          + sum(axern_controld_nodes_current{axern_state="stale"}) > 0
        for: 2m
        labels:
          severity: page
        annotations:
          summary: "Axern node readiness is degraded"
          description: "At least one node is not ready or has stale reports. Check node-all-in-one rollout and control-plane heartbeat/summary freshness."
```

## Ticket Alerts

Use these for capacity/stability work queues rather than immediate pages:

```yaml
groups:
  - name: axern-bpfnet-production-tickets
    rules:
      - alert: AxernBPFNetLocalhostCompat
        expr: axern_controld_node_bpfnet_current{axern_state="localhost_compat"} > 0
        for: 30m
        labels:
          severity: ticket
        annotations:
          summary: "bpfnet localhost TCP compatibility fallback on {{ $labels.axern_node_id }}"
          description: "This is acceptable when TC ingress/egress are ready. Track kernel capability coverage separately from main dataplane replacement."
```

## Node-Local Checks

Prometheus metrics intentionally avoid service IDs, allocation IDs, image IDs,
paths, and single-flow details. Use `bpfnetctl` for node-local forensics:

```bash
bpfnetctl check --json
bpfnetctl status --json
bpfnetctl maps
bpfnetctl dump snat_fwd_map --limit 20
bpfnetctl dump snat_rev_map --limit 20
```

Page immediately when `check --json` returns `.ok=false`, when
`status.state.fullFallback=true`, or when TC ingress/egress, pinned maps, or
pinned programs are not ready.

## Benchmark-Gated Signals

Some correctness signals are currently benchmark/probe outputs rather than
cluster-wide Prometheus metrics:

- `snatAllocExhausted`
- `snatTcpReverseMissSynAcks`
- `snatTcpNonSynMissFwdHostMismatches`
- post-GC SNAT forward/reverse/alias map retention
- UDP translated-port retention after datagram idle timeout plus GC grace

Run the production regression runbook after bpfnet changes and on a scheduled
production-validation cadence. Treat any non-zero risk counter, eBPF benchmark
failure, or persistent post-GC retention as a blocking regression.

Do not page on `snatTcpNonSynMisses` or `snatFallbackHits` alone. Late TCP
non-SYN misses can be healthy close-path tail traffic when failures are zero,
post-GC maps drain to zero, and the risk counters above stay zero.
